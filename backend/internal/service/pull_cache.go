package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
)

const (
	defaultPullCacheTTL       = 15 * time.Minute
	pullCacheStageGracePeriod = 24 * time.Hour
)

var (
	errPullCacheLocal        = errors.New("pull cache local storage failed")
	errPullCacheSizeMismatch = errors.New("pull cache size mismatch")
	errPullCacheHashMismatch = errors.New("pull cache sha256 mismatch")
)

// PullCache describes a verified on-demand copy retained for a limited time.
type PullCache struct {
	FileName  string
	MediaType string
	SHA256    string
	SizeBytes int64
	ExpiresAt time.Time
	ExpiresIn int64
}

// PullCacheDownload is an open temporary copy.
type PullCacheDownload struct {
	PullCache
	Reader io.ReadCloser
}

func preparePullCache(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "tmp", "material-pulls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// PullMaterialCache copies one ready cloud replica into the temporary cache,
// verifying its declared size and SHA-256 before making it readable.
func (s *Service) PullMaterialCache(ctx context.Context, id string) (PullCache, error) {
	// A prior process may have stopped while writing a stage file. Clean up
	// expired cache and stage files on every pull attempt, including failed
	// attempts, while leaving recent files alone in shared multi-instance DATA_DIR.
	s.cleanupPullCache()
	var material model.Material
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&material).Error; err != nil {
		return PullCache{}, apperr.NotFound("资料不存在")
	}
	sha, err := pullCacheSHA(material.SHA256)
	if err != nil || material.SizeBytes < 1 {
		return PullCache{}, apperr.Internal(errors.New("invalid material content metadata"))
	}
	dest := filepath.Join(s.pullCacheDir, sha)
	info, ok, err := s.usablePullCache(dest, material.SizeBytes)
	if err != nil {
		return PullCache{}, apperr.Internal(err)
	}
	if ok {
		return s.pullCacheFromInfo(material, info), nil
	}

	sources, err := s.readyReplicaSources(ctx, material.BlobID)
	if err != nil {
		return PullCache{}, apperr.Internal(err)
	}
	if len(sources) == 0 {
		return PullCache{}, apperr.Unavailable("没有可用的云端副本可供拉取")
	}

	var lastErr error
	for _, source := range sources {
		err := s.cacheReplica(ctx, material, source, dest)
		if err == nil {
			info, ok, statErr := s.usablePullCache(dest, material.SizeBytes)
			if statErr != nil {
				return PullCache{}, apperr.Internal(statErr)
			}
			if !ok {
				return PullCache{}, apperr.Internal(errors.New("pull cache disappeared after commit"))
			}
			return s.pullCacheFromInfo(material, info), nil
		}
		if errors.Is(err, errPullCacheLocal) {
			s.logf("pull cache local storage failed: %v", err)
			return PullCache{}, apperr.Internal(err)
		}
		lastErr = err
		s.logf("pull cache replica unavailable: %s", storageErrorText(err))
		if ctx.Err() != nil {
			return PullCache{}, apperr.Unavailable("拉取已取消")
		}
	}
	if lastErr != nil {
		s.logf("pull cache exhausted replicas: %s", storageErrorText(lastErr))
	}
	return PullCache{}, apperr.Unavailable("云端副本校验失败，未保留临时文件")
}

// OpenMaterialPullCache serves an existing, unexpired temporary copy.
func (s *Service) OpenMaterialPullCache(ctx context.Context, id string) (PullCacheDownload, error) {
	var material model.Material
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&material).Error; err != nil {
		return PullCacheDownload{}, apperr.NotFound("资料不存在")
	}
	sha, err := pullCacheSHA(material.SHA256)
	if err != nil || material.SizeBytes < 1 {
		return PullCacheDownload{}, apperr.Internal(errors.New("invalid material content metadata"))
	}
	path := filepath.Join(s.pullCacheDir, sha)
	info, ok, err := s.usablePullCache(path, material.SizeBytes)
	if err != nil {
		return PullCacheDownload{}, apperr.Internal(err)
	}
	if !ok {
		return PullCacheDownload{}, apperr.Gone("临时下载不存在或已过期")
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return PullCacheDownload{}, apperr.Gone("临时下载不存在或已过期")
	}
	if err != nil {
		return PullCacheDownload{}, apperr.Internal(err)
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != material.SizeBytes ||
		!s.now().Before(st.ModTime().Add(s.pullCacheTTL)) {
		_ = f.Close()
		_ = os.Remove(path)
		return PullCacheDownload{}, apperr.Gone("临时下载不存在或已过期")
	}
	return PullCacheDownload{
		PullCache: s.pullCacheFromInfo(material, info),
		Reader:    f,
	}, nil
}

func (s *Service) cacheReplica(ctx context.Context, material model.Material, source replicaSource, dest string) error {
	body, err := s.openReplicaSource(ctx, source)
	if err != nil {
		return err
	}
	defer body.Close()

	tmp, err := os.CreateTemp(s.pullCacheDir, ".stage-*.part")
	if err != nil {
		return fmt.Errorf("%w: create: %v", errPullCacheLocal, err)
	}
	tmpName := tmp.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(
		io.MultiWriter(tmp, hasher),
		io.LimitReader(body, material.SizeBytes+1),
	)
	if err != nil {
		return err
	}
	if written != material.SizeBytes {
		return errPullCacheSizeMismatch
	}
	if hex.EncodeToString(hasher.Sum(nil)) != material.SHA256 {
		return errPullCacheHashMismatch
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("%w: sync: %v", errPullCacheLocal, err)
	}
	if err := tmp.Close(); err != nil {
		closed = true
		return fmt.Errorf("%w: close: %v", errPullCacheLocal, err)
	}
	closed = true
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("%w: chmod: %v", errPullCacheLocal, err)
	}
	now := s.now()
	if err := os.Chtimes(tmpName, now, now); err != nil {
		return fmt.Errorf("%w: touch: %v", errPullCacheLocal, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("%w: commit: %v", errPullCacheLocal, err)
	}
	committed = true
	return nil
}

func (s *Service) usablePullCache(path string, expectedSize int64) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	expired := !s.now().Before(info.ModTime().Add(s.pullCacheTTL))
	if !info.Mode().IsRegular() || info.Size() != expectedSize || expired {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
		return nil, false, nil
	}
	return info, true, nil
}

func (s *Service) pullCacheFromInfo(material model.Material, info os.FileInfo) PullCache {
	expiresAt := info.ModTime().Add(s.pullCacheTTL).UTC()
	expiresIn := int64(math.Ceil(expiresAt.Sub(s.now()).Seconds()))
	if expiresIn < 1 {
		expiresIn = 1
	}
	return PullCache{
		FileName:  material.FileName,
		MediaType: material.MediaType,
		SHA256:    material.SHA256,
		SizeBytes: material.SizeBytes,
		ExpiresAt: expiresAt,
		ExpiresIn: expiresIn,
	}
}

func (s *Service) cleanupPullCache() {
	entries, err := os.ReadDir(s.pullCacheDir)
	if err != nil {
		s.logf("pull cache cleanup failed: %v", err)
		return
	}
	now := s.now()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!isPullCacheName(name) && !isPullCacheStageName(name)) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		ttl := s.pullCacheTTL
		if isPullCacheStageName(name) {
			// Cloud reads can take several minutes; give abandoned stages a
			// longer grace window so another instance never unlinks an active
			// transfer just because the finished-cache TTL is shorter.
			ttl = pullCacheStageGracePeriod
		}
		if now.Before(info.ModTime().Add(ttl)) {
			continue
		}
		if err := os.Remove(filepath.Join(s.pullCacheDir, entry.Name())); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			s.logf("pull cache cleanup failed")
		}
	}
}

func pullCacheSHA(value string) (string, error) {
	if value != strings.ToLower(value) {
		return "", errors.New("invalid sha256")
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return "", errors.New("invalid sha256")
	}
	return value, nil
}

func isPullCacheName(name string) bool {
	if len(name) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func isPullCacheStageName(name string) bool {
	return strings.HasPrefix(name, ".stage-") && strings.HasSuffix(name, ".part")
}
