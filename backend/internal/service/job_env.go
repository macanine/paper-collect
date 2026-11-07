package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
)

// masterEnv executes on the master: originals come from the local CAS (or a
// cloud replica when the original is missing) and progress goes straight to
// the job row.
type masterEnv struct {
	s   *Service
	job *model.Job // nil outside a job
}

func (s *Service) masterEnv() jobexec.Env             { return masterEnv{s: s} }
func (s *Service) localEnv(job model.Job) jobexec.Env { return masterEnv{s: s, job: &job} }

func (e masterEnv) OpenBlob(ctx context.Context, b jobexec.Blob) (io.ReadCloser, error) {
	return e.s.openBlob(ctx, b.SHA256)
}

func (e masterEnv) Progress(ctx context.Context, message string, current, total int, metrics map[string]any) {
	if e.job != nil {
		e.s.jobProgressWithMetrics(ctx, *e.job, message, current, total, metrics)
	}
}

func (e masterEnv) SaveRefreshToken(ctx context.Context, driveID, previous, next string) error {
	return e.s.SaveRefreshToken(ctx, driveID, previous, next)
}

func (e masterEnv) Adapter(cfg storage.Config) (storage.Adapter, error) { return e.s.factory(cfg) }
func (e masterEnv) TempDir() string                                     { return filepath.Join(e.s.blobs.DataDir(), "tmp") }
func (e masterEnv) Logf(format string, args ...any)                     { e.s.logf(format, args...) }

// openBlob returns the original by hash. The local CAS file is returned as an
// *os.File so callers can read it in place; a cloud replica is streamed and
// verified against the recorded size and SHA-256.
func (s *Service) openBlob(ctx context.Context, sha string) (io.ReadCloser, error) {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if _, err := pullCacheSHA(sha); err != nil {
		return nil, apperr.Validation("sha256 格式无效")
	}
	var b model.Blob
	if err := s.db.WithContext(ctx).Where("sha256 = ?", sha).First(&b).Error; err != nil {
		return nil, apperr.NotFound("原件不存在")
	}
	if s.blobs.Exists(sha) {
		if f, err := os.Open(s.blobs.Path(sha)); err == nil {
			return f, nil
		}
	}
	sources, err := s.readyReplicaSources(ctx, b.ID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	var lastErr error
	for _, source := range sources {
		body, err := s.openReplicaSource(ctx, source)
		if err != nil {
			lastErr = err
			continue
		}
		return jobexec.VerifyReader(body, sha, b.SizeBytes), nil
	}
	if lastErr != nil {
		s.logf("open blob exhausted replicas: %s", storageErrorText(lastErr))
	}
	return nil, apperr.Unavailable("本机原件缺失，且没有可用的云端副本")
}

// OpenBlobForNode serves an original to a worker node. Local files support
// range requests through the returned *os.File.
func (s *Service) OpenBlobForNode(ctx context.Context, sha string) (io.ReadCloser, int64, error) {
	body, err := s.openBlob(ctx, sha)
	if err != nil {
		return nil, 0, err
	}
	var b model.Blob
	if err := s.db.WithContext(ctx).Select("size_bytes").Where("sha256 = ?", strings.ToLower(sha)).First(&b).Error; err != nil {
		body.Close()
		return nil, 0, apperr.NotFound("原件不存在")
	}
	return body, b.SizeBytes, nil
}
