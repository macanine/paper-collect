package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// localAdapter 把对象写入服务端固定的本地根目录。根目录只能由服务端配置，API 不允许任意路径。
type localAdapter struct {
	root string // 绝对且已清理
}

func newLocal(cfg Config) (Adapter, error) {
	root := strings.TrimSpace(cfg.Settings["root"])
	if root == "" {
		return nil, newCloudError(ClassConfig, "local_root_required", false)
	}
	if !filepath.IsAbs(root) {
		return nil, newCloudError(ClassConfig, "local_root_not_absolute", false)
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, newCloudError(ClassConfig, "local_root_unavailable", false)
	}
	return &localAdapter{root: root}, nil
}

func (a *localAdapter) destination(segments []string) string {
	parts := append([]string{a.root}, segments...)
	return filepath.Join(parts...)
}

// ensureWithin 确认 path（解析符号链接后）仍位于根目录之内。
func (a *localAdapter) ensureWithin(path string) *CloudError {
	realRoot, err := filepath.EvalSymlinks(a.root)
	if err != nil {
		return newCloudError(ClassConfig, "local_root_unavailable", false)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return newCloudError(ClassFileMissing, "local_object_missing", false)
		}
		return newCloudError(ClassUnavailable, "local_path_unresolvable", true)
	}
	if realPath != realRoot && !strings.HasPrefix(realPath, realRoot+string(filepath.Separator)) {
		return newCloudError(ClassInvalidInput, "local_path_escapes_root", false)
	}
	return nil
}

func (a *localAdapter) Put(ctx context.Context, obj Object) (Stored, error) {
	if err := ctx.Err(); err != nil {
		return Stored{}, transportError(ctx, err, "local_put")
	}
	segments, cerr := validateObjectKey(obj.Key)
	if cerr != nil {
		return Stored{}, cerr
	}
	if obj.Path == "" {
		return Stored{}, newCloudError(ClassInvalidInput, "local_source_path_required", false)
	}
	if obj.Size < 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "object_size_negative", false)
	}
	expectedSHA := ""
	if obj.SHA256 != "" {
		normalized, ok := normalizeSHA256(obj.SHA256)
		if !ok {
			return Stored{}, newCloudError(ClassInvalidInput, "sha256_invalid", false)
		}
		expectedSHA = normalized
	}

	src, err := os.Open(obj.Path)
	if err != nil {
		return Stored{}, newCloudError(ClassFileMissing, "local_source_missing", false)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_source_stat_failed", true)
	}
	if !info.Mode().IsRegular() {
		return Stored{}, newCloudError(ClassInvalidInput, "local_source_not_regular", false)
	}
	if info.Size() != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "local_source_size_mismatch", false)
	}

	dest := a.destination(segments)
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_mkdir_failed", true)
	}
	if cerr := a.ensureWithin(dir); cerr != nil {
		return Stored{}, cerr
	}

	tmp, err := os.CreateTemp(dir, ".put-*.tmp")
	if err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_temp_create_failed", true)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), &progressReader{r: ctxReader{ctx: ctx, r: src}, total: obj.Size, report: obj.Progress})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Stored{}, transportError(ctx, ctxErr, "local_put")
		}
		return Stored{}, newCloudError(ClassUnavailable, "local_write_failed", true)
	}
	if err := tmp.Sync(); err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_sync_failed", true)
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_close_failed", true)
	}
	if written != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "local_write_size_mismatch", false)
	}
	if expectedSHA != "" && hex.EncodeToString(hasher.Sum(nil)) != expectedSHA {
		return Stored{}, newCloudError(ClassHashMismatch, "local_write_hash_mismatch", false)
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_chmod_failed", true)
	}
	verification := "size"
	if expectedSHA != "" {
		verification = "sha256"
	}
	// A hard link commits without replacing whatever already sits at dest.
	if err := os.Link(tmpName, dest); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return Stored{}, newCloudError(ClassUnavailable, "local_commit_failed", true)
		}
		if cerr := a.adoptExisting(dest, written, hex.EncodeToString(hasher.Sum(nil))); cerr != nil {
			return Stored{}, cerr
		}
		verification = "sha256"
	}
	return Stored{RemoteID: obj.Key, Size: written, Verification: verification}, nil
}

// adoptExisting accepts a file already at dest only when it holds the same bytes.
func (a *localAdapter) adoptExisting(dest string, size int64, sum string) *CloudError {
	if cerr := a.ensureWithin(dest); cerr != nil {
		return cerr
	}
	f, err := os.Open(dest)
	if err != nil {
		return newCloudError(ClassUnavailable, "local_existing_unreadable", true)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return newCloudError(ClassConflict, "local_path_taken", false)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return newCloudError(ClassUnavailable, "local_existing_unreadable", true)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != sum {
		return newCloudError(ClassConflict, "local_path_taken", false)
	}
	return nil
}

// Move renames an object inside the root without replacing another file.
func (a *localAdapter) Move(ctx context.Context, remoteID, key string) (Stored, error) {
	if err := ctx.Err(); err != nil {
		return Stored{}, transportError(ctx, err, "local_move")
	}
	from, cerr := validateObjectKey(remoteID)
	if cerr != nil {
		return Stored{}, cerr
	}
	to, cerr := validateObjectKey(key)
	if cerr != nil {
		return Stored{}, cerr
	}
	src, dest := a.destination(from), a.destination(to)
	if cerr := a.ensureWithin(src); cerr != nil {
		return Stored{}, cerr
	}
	info, err := os.Stat(src)
	if err != nil || !info.Mode().IsRegular() {
		return Stored{}, newCloudError(ClassFileMissing, "local_object_missing", false)
	}
	if src == dest {
		return Stored{RemoteID: key, Size: info.Size(), Verification: "size"}, nil
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Stored{}, newCloudError(ClassUnavailable, "local_mkdir_failed", true)
	}
	if cerr := a.ensureWithin(dir); cerr != nil {
		return Stored{}, cerr
	}
	if err := os.Link(src, dest); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return Stored{}, newCloudError(ClassUnavailable, "local_move_failed", true)
		}
		// An earlier attempt may have linked but not yet removed the source.
		existing, statErr := os.Stat(dest)
		if statErr != nil || !os.SameFile(info, existing) {
			return Stored{}, newCloudError(ClassConflict, "local_path_taken", false)
		}
	}
	if err := os.Remove(src); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Stored{}, newCloudError(ClassUnavailable, "local_move_cleanup_failed", true)
	}
	a.pruneEmptyDirs(filepath.Dir(src))
	return Stored{RemoteID: key, Size: info.Size(), Verification: "size"}, nil
}

// Delete removes an object and any directories it leaves empty.
func (a *localAdapter) Delete(ctx context.Context, remoteID string) error {
	if err := ctx.Err(); err != nil {
		return transportError(ctx, err, "local_delete")
	}
	segments, cerr := validateObjectKey(remoteID)
	if cerr != nil {
		return cerr
	}
	dest := a.destination(segments)
	if cerr := a.ensureWithin(dest); cerr != nil {
		if cerr.Class == ClassFileMissing {
			return nil
		}
		return cerr
	}
	if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return newCloudError(ClassUnavailable, "local_delete_failed", true)
	}
	a.pruneEmptyDirs(filepath.Dir(dest))
	return nil
}

// pruneEmptyDirs removes empty directories from dir up to, not including, the root.
func (a *localAdapter) pruneEmptyDirs(dir string) {
	for dir != a.root && strings.HasPrefix(dir, a.root+string(filepath.Separator)) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func (a *localAdapter) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, transportError(ctx, err, "local_open")
	}
	segments, cerr := validateObjectKey(key)
	if cerr != nil {
		return nil, cerr
	}
	dest := a.destination(segments)
	if cerr := a.ensureWithin(dest); cerr != nil {
		return nil, cerr
	}
	f, err := os.Open(dest)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, newCloudError(ClassFileMissing, "local_object_missing", false)
		}
		return nil, newCloudError(ClassUnavailable, "local_open_failed", true)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, newCloudError(ClassFileMissing, "local_object_not_regular", false)
	}
	return f, nil
}

// Share 本地存储没有公开链接能力，返回 unsupported 状态而不伪造 URL。
func (a *localAdapter) Share(ctx context.Context, remoteID string) (Share, error) {
	return Share{Status: "unsupported"}, nil
}

// Health 写入并删除探测文件，证明根目录可写。
func (a *localAdapter) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return transportError(ctx, err, "local_health")
	}
	info, err := os.Stat(a.root)
	if err != nil {
		return newCloudError(ClassConfig, "local_root_missing", false)
	}
	if !info.IsDir() {
		return newCloudError(ClassConfig, "local_root_not_directory", false)
	}
	probe, err := os.CreateTemp(a.root, ".health-*.tmp")
	if err != nil {
		return newCloudError(ClassUnavailable, "local_probe_create_failed", true)
	}
	name := probe.Name()
	_, writeErr := probe.Write([]byte("paper-collect storage health\n"))
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if writeErr != nil || closeErr != nil {
		return newCloudError(ClassUnavailable, "local_probe_write_failed", true)
	}
	if removeErr != nil {
		return newCloudError(ClassUnavailable, "local_probe_cleanup_failed", true)
	}
	return nil
}

func (a *localAdapter) Capabilities() Capabilities {
	return Capabilities{Upload: true, Download: true, Share: false, Folders: true, Move: true, Delete: true}
}
