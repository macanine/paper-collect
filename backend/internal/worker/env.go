package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/storage"
)

// remoteEnv is the Env of one job executing on a worker.
type remoteEnv struct {
	w     *Worker
	jobID string

	mu     sync.Mutex
	pinned []string
}

func (e *remoteEnv) Progress(ctx context.Context, message string, current, total int, metrics map[string]any) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_ = e.w.call(ctx, http.MethodPost, "/internal/v1/jobs/"+e.jobID+"/progress",
		map[string]any{"message": message, "current": current, "total": total, "metrics": metrics}, nil)
}

func (e *remoteEnv) SaveRefreshToken(ctx context.Context, driveID, previous, next string) error {
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	return e.w.call(ctx, http.MethodPost, "/internal/v1/drives/"+driveID+"/refresh-token",
		map[string]string{"previous": previous, "next": next}, nil)
}

func (e *remoteEnv) Adapter(cfg storage.Config) (storage.Adapter, error) { return e.w.cfg.Factory(cfg) }
func (e *remoteEnv) TempDir() string                                     { return filepath.Join(e.w.cfg.CacheDir, "tmp") }
func (e *remoteEnv) Logf(format string, args ...any) {
	e.w.cfg.Logf("%s", jobexec.Redact(fmt.Sprintf(format, args...)))
}

// OpenBlob serves the original from the local cache, downloading it from the
// master first when needed. Callers may keep using the path after closing the
// file, so it stays pinned against eviction until the job ends.
func (e *remoteEnv) OpenBlob(ctx context.Context, b jobexec.Blob) (io.ReadCloser, error) {
	if raw, err := hex.DecodeString(b.SHA256); err != nil || len(raw) != sha256.Size {
		return nil, errors.New("sha256 格式无效")
	}
	path := filepath.Join(e.w.cfg.CacheDir, "blobs", b.SHA256)
	e.pin(path)
	unlock, err := e.w.lockDownload(ctx, b.SHA256)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if f := e.w.openCached(path, b); f != nil {
		return f, nil
	}
	if !e.fetchFromReplicas(ctx, b, path) {
		if err := e.w.download(ctx, b, path); err != nil {
			return nil, err
		}
	}
	e.w.evict()
	return os.Open(path)
}

// fetchFromReplicas copies the original straight from a cloud replica, in read
// order, so it does not flow out through the master. Any failure, including a
// hash mismatch, falls through to the next source and finally to the master.
func (e *remoteEnv) fetchFromReplicas(ctx context.Context, b jobexec.Blob, dest string) bool {
	for _, source := range b.Sources {
		if ctx.Err() != nil {
			return false
		}
		err := e.copyFromReplica(ctx, b, source, dest)
		if err == nil {
			return true
		}
		e.Logf("read original from %s replica failed; trying next source: %v", source.Drive.Kind, err)
	}
	return false
}

func (e *remoteEnv) copyFromReplica(ctx context.Context, b jobexec.Blob, source jobexec.BlobSource, dest string) error {
	adapter, err := jobexec.NewAdapter(e, source.Drive)
	if err != nil {
		return err
	}
	if !adapter.Capabilities().Download {
		return errors.New("网盘不支持下载")
	}
	body, err := adapter.Open(ctx, source.Remote)
	if err != nil {
		return err
	}
	verified := jobexec.VerifyReader(body, b.SHA256, b.Size)
	defer verified.Close()
	tmp, err := os.CreateTemp(filepath.Join(e.w.cfg.CacheDir, "tmp"), "blob-*.part")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := io.Copy(tmp, verified); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

func (e *remoteEnv) pin(path string) {
	e.w.cacheMu.Lock()
	defer e.w.cacheMu.Unlock()
	e.w.pins[path]++
	e.mu.Lock()
	e.pinned = append(e.pinned, path)
	e.mu.Unlock()
}

// release frees everything this job pinned.
func (e *remoteEnv) release() {
	e.w.cacheMu.Lock()
	defer e.w.cacheMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, path := range e.pinned {
		if e.w.pins[path]--; e.w.pins[path] <= 0 {
			delete(e.w.pins, path)
		}
	}
	e.pinned = nil
}

// lockDownload lets jobs sharing an original, such as uploads of one file to
// several drives, wait for a single download instead of each fetching it.
func (w *Worker) lockDownload(ctx context.Context, sha string) (func(), error) {
	for {
		w.cacheMu.Lock()
		busy, ok := w.downloads[sha]
		if !ok {
			done := make(chan struct{})
			w.downloads[sha] = done
			w.cacheMu.Unlock()
			return func() {
				w.cacheMu.Lock()
				delete(w.downloads, sha)
				w.cacheMu.Unlock()
				close(done)
			}, nil
		}
		w.cacheMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-busy:
		}
	}
}

// openCached returns a verified cached original, discarding a damaged one.
func (w *Worker) openCached(path string, b jobexec.Blob) *os.File {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err == nil && n == b.Size && hex.EncodeToString(h.Sum(nil)) == b.SHA256 {
		if _, err = f.Seek(0, io.SeekStart); err == nil {
			now := time.Now()
			_ = os.Chtimes(path, now, now) // recency for eviction
			return f
		}
	}
	f.Close()
	w.cfg.Logf("cached original %s failed verification; downloading it again", b.SHA256)
	_ = os.Remove(path)
	return nil
}

const downloadAttempts = 5

// download fetches an original into dest, resuming with Range requests when the
// connection drops. The file is only published after its hash matches.
func (w *Worker) download(ctx context.Context, b jobexec.Blob, dest string) error {
	tmp, err := os.CreateTemp(filepath.Join(w.cfg.CacheDir, "tmp"), "blob-*.part")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	defer tmp.Close()
	for attempt := 0; ; attempt++ {
		retry, err := w.fetchInto(ctx, b, tmp)
		if err == nil {
			break
		}
		if !retry || attempt+1 >= downloadAttempts || ctx.Err() != nil {
			return err
		}
		w.cfg.Logf("download %s interrupted; retrying: %s", b.SHA256, jobexec.Redact(err.Error()))
		sleep(ctx, time.Duration(attempt+1)*time.Second)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, tmp); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != b.SHA256 {
		return fmt.Errorf("%w：%w", jobexec.ErrBlobUnavailable, jobexec.ErrContentMismatch)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

// fetchInto appends the rest of the original to tmp and reports whether a
// failure is worth retrying.
func (w *Worker) fetchInto(ctx context.Context, b jobexec.Blob, tmp *os.File) (bool, error) {
	offset, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return false, err
	}
	if offset > b.Size {
		if err := restart(tmp); err != nil {
			return false, err
		}
		offset = 0
	}
	if offset == b.Size && offset > 0 {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.url("/internal/v1/blobs/"+b.SHA256), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := w.cfg.HTTP.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)):
	case resp.StatusCode == http.StatusOK: // the master streams from a cloud replica and cannot seek
		if err := restart(tmp); err != nil {
			return false, err
		}
	default:
		err := fmt.Errorf("%w：主端返回 HTTP %d", jobexec.ErrBlobUnavailable, resp.StatusCode)
		return resp.StatusCode >= 500 || resp.StatusCode == http.StatusPartialContent, err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return true, err
	}
	end, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, err
	}
	if end != b.Size {
		return true, fmt.Errorf("%w：已收到 %d 字节，应为 %d", jobexec.ErrBlobUnavailable, end, b.Size)
	}
	return false, nil
}

func restart(tmp *os.File) error {
	if err := tmp.Truncate(0); err != nil {
		return err
	}
	_, err := tmp.Seek(0, io.SeekStart)
	return err
}

// evict removes least recently used originals until the cache fits, never
// removing a file a running job may still use.
func (w *Worker) evict() {
	cacheMax := w.cacheMaxValue()
	if cacheMax <= 0 {
		return
	}
	w.cacheMu.Lock()
	defer w.cacheMu.Unlock()
	entries, total := w.cacheEntries()
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
	for _, e := range entries {
		if total <= cacheMax {
			return
		}
		if w.pins[e.path] > 0 {
			continue
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}

func (w *Worker) cacheUsed() int64 {
	_, total := w.cacheEntries()
	return total
}

type cacheEntry struct {
	path string
	size int64
	mod  time.Time
}

func (w *Worker) cacheEntries() ([]cacheEntry, int64) {
	dir := filepath.Join(w.cfg.CacheDir, "blobs")
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	var out []cacheEntry
	var total int64
	for _, item := range items {
		info, err := item.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, cacheEntry{path: filepath.Join(dir, item.Name()), size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}
	return out, total
}
