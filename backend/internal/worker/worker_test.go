package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/storage"
)

func blobOf(content []byte) jobexec.Blob {
	sum := sha256.Sum256(content)
	return jobexec.Blob{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}
}

func newWorker(t *testing.T, master http.Handler, mutate func(*Config)) *Worker {
	t.Helper()
	ts := httptest.NewServer(master)
	t.Cleanup(ts.Close)
	cfg := Config{
		MasterURL: ts.URL, Token: "nd-test", CacheDir: t.TempDir(), Slots: map[string]int{"replicate": 1},
		Heartbeat: 20 * time.Millisecond, ClaimWait: 10 * time.Millisecond, Logf: t.Logf,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestDownloadResumesAfterDroppedConnection(t *testing.T) {
	content := []byte(strings.Repeat("原件内容", 4096))
	var ranges []string
	var mu sync.Mutex
	w := newWorker(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		first := len(ranges) == 1
		mu.Unlock()
		if first {
			rw.Header().Set("Content-Length", fmt.Sprint(len(content)))
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write(content[:1000])
			rw.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		http.ServeContent(rw, r, "", time.Time{}, strings.NewReader(string(content)))
	}), nil)
	env := &remoteEnv{w: w, jobID: "job"}
	defer env.release()
	body, err := env.OpenBlob(context.Background(), blobOf(content))
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != string(content) {
		t.Fatalf("resumed content differs: %d bytes", len(got))
	}
	if len(ranges) != 2 || ranges[1] != "bytes=1000-" {
		t.Fatalf("requests = %q, want a resume from byte 1000", ranges)
	}
}

func TestDownloadRejectsWrongContentAndLeavesNoCache(t *testing.T) {
	want := []byte("the real original")
	w := newWorker(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte("the fake original"))
	}), nil)
	env := &remoteEnv{w: w, jobID: "job"}
	defer env.release()
	b := blobOf(want)
	if _, err := env.OpenBlob(context.Background(), b); !errors.Is(err, jobexec.ErrContentMismatch) {
		t.Fatalf("err = %v, want a content mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(w.cfg.CacheDir, "blobs", b.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("unverified content reached the cache: %v", err)
	}
}

func TestDamagedCacheEntryIsDownloadedAgain(t *testing.T) {
	content := []byte("verified original")
	var fetches atomic.Int32
	w := newWorker(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		_, _ = rw.Write(content)
	}), nil)
	b := blobOf(content)
	path := filepath.Join(w.cfg.CacheDir, "blobs", b.SHA256)
	// Same size, different bytes: only hashing can catch this.
	if err := os.WriteFile(path, []byte(strings.Repeat("x", len(content))), 0o600); err != nil {
		t.Fatal(err)
	}
	env := &remoteEnv{w: w, jobID: "job"}
	defer env.release()
	body, err := env.OpenBlob(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != string(content) || fetches.Load() != 1 {
		t.Fatalf("got %q after %d fetches", got, fetches.Load())
	}
	// A valid entry is served without another request.
	again, err := env.OpenBlob(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	if fetches.Load() != 1 {
		t.Fatalf("valid cache entry fetched again: %d", fetches.Load())
	}
}

func TestConcurrentJobsShareOneDownload(t *testing.T) {
	content := []byte("shared original")
	var fetches atomic.Int32
	w := newWorker(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = rw.Write(content)
	}), nil)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env := &remoteEnv{w: w, jobID: fmt.Sprint(i)}
			defer env.release()
			body, err := env.OpenBlob(context.Background(), blobOf(content))
			if err != nil {
				t.Error(err)
				return
			}
			body.Close()
		}(i)
	}
	wg.Wait()
	if fetches.Load() != 1 {
		t.Fatalf("fetches = %d, want 1", fetches.Load())
	}
}

// Replicate uses the cached path after closing the file, so another job's
// eviction must not remove it mid-upload.
func TestEvictionKeepsFilesPinnedByRunningJobs(t *testing.T) {
	contents := map[string][]byte{}
	w := newWorker(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write(contents[filepath.Base(r.URL.Path)])
	}), func(c *Config) { c.CacheMax = 10 })
	open := func(env *remoteEnv, content []byte) string {
		t.Helper()
		b := blobOf(content)
		contents[b.SHA256] = content
		body, err := env.OpenBlob(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		body.Close()
		return filepath.Join(w.cfg.CacheDir, "blobs", b.SHA256)
	}
	running := &remoteEnv{w: w, jobID: "running"}
	inUse := open(running, []byte("in use by a running job"))

	finished := &remoteEnv{w: w, jobID: "finished"}
	done := open(finished, []byte("job already finished"))
	finished.release()

	other := &remoteEnv{w: w, jobID: "other"}
	defer other.release()
	open(other, []byte("newest download"))
	if _, err := os.Stat(inUse); err != nil {
		t.Fatalf("eviction removed a pinned file: %v", err)
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Fatalf("unpinned file survived over the cache limit: %v", err)
	}
	running.release()
	if len(w.pins) != 1 {
		t.Fatalf("pins = %v, want only the job still running", w.pins)
	}
}

// fakeMaster answers the node protocol for one job.
type fakeMaster struct {
	mu         sync.Mutex
	claims     atomic.Int32
	job        *jobexec.Input
	heartbeat  int // status for heartbeats; 0 means 200
	complete   int // status for complete; 0 means 204
	fails      []map[string]any
	completes  int
	lost       []string
	interrupts int
}

func (m *fakeMaster) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeData := func(v any) {
		_ = json.NewEncoder(rw).Encode(map[string]any{"data": v})
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/heartbeat"):
		if m.heartbeat != 0 {
			rw.WriteHeader(m.heartbeat)
			return
		}
		writeData(map[string]any{"lost_job_ids": m.lost})
	case strings.HasSuffix(r.URL.Path, "/claim"):
		m.claims.Add(1)
		if m.job == nil {
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		writeData(m.job)
		m.job = nil
	case strings.HasSuffix(r.URL.Path, "/complete"):
		m.completes++
		if m.complete != 0 {
			rw.WriteHeader(m.complete)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/fail"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.fails = append(m.fails, body)
		rw.WriteHeader(http.StatusNoContent)
	default:
		rw.WriteHeader(http.StatusNoContent)
	}
}

func (m *fakeMaster) failures() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any{}, m.fails...)
}

// blockingAdapter runs until its context ends.
type blockingAdapter struct {
	storage.Adapter
	started chan struct{}
}

func (a blockingAdapter) Put(ctx context.Context, _ storage.Object) (storage.Stored, error) {
	close(a.started)
	<-ctx.Done()
	return storage.Stored{}, ctx.Err()
}

type instantAdapter struct{ storage.Adapter }

func (instantAdapter) Put(context.Context, storage.Object) (storage.Stored, error) {
	return storage.Stored{RemoteID: "r", Size: 1, Verification: "sha256"}, nil
}

func replicateJob(t *testing.T, w *Worker) *jobexec.Input {
	t.Helper()
	content := []byte("job original")
	b := blobOf(content)
	if err := os.WriteFile(filepath.Join(w.cfg.CacheDir, "blobs", b.SHA256), content, 0o600); err != nil {
		t.Fatal(err)
	}
	return &jobexec.Input{JobID: "job-1", Kind: jobexec.KindReplicate, Replicate: &jobexec.ReplicateInput{
		Drive: jobexec.DriveConfig{ID: "d", Kind: "webdav"}, Blob: b, DesiredPath: "a.pdf",
	}}
}

func runFor(w *Worker, d time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	w.Run(ctx)
}

func TestEmptyClaimRepliesDoNotSpin(t *testing.T) {
	master := &fakeMaster{}
	w := newWorker(t, master, nil)
	w.cfg.ClaimWait = time.Millisecond
	runFor(w, 250*time.Millisecond)
	if n := master.claims.Load(); n > 2 {
		t.Fatalf("%d claims in 250ms, want at most one per second", n)
	}
}

func TestDisabledNodeStopsRunningJobs(t *testing.T) {
	master := &fakeMaster{heartbeat: http.StatusUnauthorized}
	started := make(chan struct{})
	w := newWorker(t, master, func(c *Config) {
		c.Factory = func(storage.Config) (storage.Adapter, error) { return blockingAdapter{started: started}, nil }
	})
	master.job = replicateJob(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		w.execute(ctx, *master.job)
		close(finished)
	}()
	<-started
	w.heartbeat(ctx)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("job kept running with its credentials after the node was disabled")
	}
	if got := master.failures(); len(got) != 0 {
		t.Fatalf("a stopped job must not report: %v", got)
	}
}

func TestLostJobStopsWithoutReporting(t *testing.T) {
	master := &fakeMaster{lost: []string{"job-1"}}
	started := make(chan struct{})
	w := newWorker(t, master, func(c *Config) {
		c.Factory = func(storage.Config) (storage.Adapter, error) { return blockingAdapter{started: started}, nil }
	})
	in := replicateJob(t, w)
	finished := make(chan struct{})
	go func() {
		w.execute(context.Background(), *in)
		close(finished)
	}()
	<-started
	w.heartbeat(context.Background())
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("lost job kept running")
	}
	if got := master.failures(); len(got) != 0 || master.completes != 0 {
		t.Fatalf("lost job reported: fails=%v completes=%d", got, master.completes)
	}
}

// The same node can win a job back after being cut off past the lease. Fencing
// by node id cannot tell the two executions apart, so the worker must.
func TestReclaimedJobReplacesStaleExecution(t *testing.T) {
	master := &fakeMaster{}
	started := make(chan struct{})
	var calls atomic.Int32
	w := newWorker(t, master, func(c *Config) {
		c.Factory = func(storage.Config) (storage.Adapter, error) {
			if calls.Add(1) == 1 {
				return blockingAdapter{started: started}, nil
			}
			return instantAdapter{}, nil
		}
	})
	in := replicateJob(t, w)
	stale := make(chan struct{})
	go func() {
		w.execute(context.Background(), *in)
		close(stale)
	}()
	<-started
	w.execute(context.Background(), *in)
	select {
	case <-stale:
	case <-time.After(2 * time.Second):
		t.Fatal("stale execution kept running")
	}
	master.mu.Lock()
	defer master.mu.Unlock()
	if master.completes != 1 || len(master.fails) != 0 {
		t.Fatalf("completes=%d fails=%v, want exactly one report", master.completes, master.fails)
	}
	if len(w.running) != 0 {
		t.Fatalf("running = %v, want empty", w.running)
	}
}

func TestRefusedResultIsReportedAsFailure(t *testing.T) {
	master := &fakeMaster{complete: http.StatusRequestEntityTooLarge}
	w := newWorker(t, master, func(c *Config) {
		c.Factory = func(storage.Config) (storage.Adapter, error) { return instantAdapter{}, nil }
	})
	w.execute(context.Background(), *replicateJob(t, w))
	got := master.failures()
	if len(got) != 1 || !strings.Contains(fmt.Sprint(got[0]["error"]), "拒绝") {
		t.Fatalf("fails = %v, want the refusal reported so the job stops being requeued", got)
	}
}

// A result refused because the lease moved on belongs to another execution.
func TestConflictOnCompleteDoesNotFailTheJob(t *testing.T) {
	master := &fakeMaster{complete: http.StatusConflict}
	w := newWorker(t, master, func(c *Config) {
		c.Factory = func(storage.Config) (storage.Adapter, error) { return instantAdapter{}, nil }
	})
	w.execute(context.Background(), *replicateJob(t, w))
	if got := master.failures(); len(got) != 0 {
		t.Fatalf("fails = %v, want none", got)
	}
}

func TestOverrideRapidOCROnlyTouchesSelfHostedOCR(t *testing.T) {
	var seen string
	master := &fakeMaster{}
	w := newWorker(t, master, func(c *Config) { c.RapidOCRURL = "http://10.0.0.5:9003" })
	in := jobexec.Input{Kind: jobexec.KindAnalyze, Analyze: &jobexec.AnalyzeInput{OCR: &jobexec.OCRConfig{Kind: "rapidocr", BaseURL: "http://rapidocr:9003"}}}
	in.OverrideRapidOCR(w.cfg.RapidOCRURL)
	seen = in.Analyze.OCR.BaseURL
	if seen != "http://10.0.0.5:9003" {
		t.Fatalf("rapidocr base = %q", seen)
	}
	hosted := jobexec.Input{Kind: jobexec.KindIndex, Index: &jobexec.IndexInput{OCR: &jobexec.OCRConfig{Kind: "mineru", BaseURL: "https://mineru.net"}}}
	hosted.OverrideRapidOCR(w.cfg.RapidOCRURL)
	if hosted.Index.OCR.BaseURL != "https://mineru.net" {
		t.Fatalf("hosted OCR must keep its address: %q", hosted.Index.OCR.BaseURL)
	}
}

func TestStalePartialFilesAreRemovedOnStart(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "tmp", "blob-1.part")
	if err := os.WriteFile(stale, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{MasterURL: "http://127.0.0.1:1", Token: "nd-x", CacheDir: dir, Slots: map[string]int{"replicate": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale partial survived: %v", err)
	}
}
