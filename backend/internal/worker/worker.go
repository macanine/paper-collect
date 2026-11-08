// Package worker runs a database-free node that claims jobs from the master,
// executes them with jobexec, and reports results. Originals are cached on
// local disk; credentials arrive with each job and are kept only in memory.
package worker

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/storage"
	"github.com/nic/paper-collect/backend/internal/sysstat"
)

// Config describes one worker node.
type Config struct {
	MasterURL string
	Token     string
	Version   string
	CacheDir  string
	// CacheMax, Slots, Heartbeat and ClaimWait are optional programmatic
	// fallbacks. The production process receives them from the master.
	CacheMax  int64
	Slots     map[string]int // job kind → concurrent executions
	Heartbeat time.Duration
	ClaimWait time.Duration
	HTTP      *http.Client
	Factory   func(storage.Config) (storage.Adapter, error)
	Logf      func(format string, args ...any)
}

// Worker is a running node.
type Worker struct {
	cfg     Config
	base    *url.URL
	started time.Time
	sampler *sysstat.Sampler

	mu      sync.Mutex
	running map[string]*execution

	runtimeMu      sync.RWMutex
	slots          map[string]int
	cacheMax       int64
	ocrProviderIDs []string
	heartbeatEvery time.Duration
	claimWait      time.Duration
	revision       int64
	configured     bool

	loopMu     sync.Mutex
	loopCancel context.CancelFunc
	loopWG     sync.WaitGroup
	parent     context.Context

	cacheMu   sync.Mutex
	pins      map[string]int           // cached files in use by a job
	downloads map[string]chan struct{} // originals being fetched, by hash
}

// execution is one running job; a re-claimed job replaces its stale execution.
type execution struct{ cancel context.CancelCauseFunc }

var errLost = errors.New("job lease lost")

const (
	callTimeout    = 15 * time.Second
	reportTimeout  = 30 * time.Second
	claimGrace     = 30 * time.Second // on top of the long-poll wait
	minClaimPeriod = time.Second      // spacing when the master answers empty at once
)

// New validates the configuration.
func New(cfg Config) (*Worker, error) {
	base, err := url.Parse(strings.TrimRight(cfg.MasterURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("MASTER_URL 必须是 http(s) 地址")
	}
	if cfg.Token == "" {
		return nil, errors.New("缺少节点令牌")
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 10 * time.Second
	}
	if cfg.ClaimWait <= 0 {
		cfg.ClaimWait = 25 * time.Second
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	if cfg.Factory == nil {
		cfg.Factory = storage.New
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	for _, dir := range []string{filepath.Join(cfg.CacheDir, "blobs"), filepath.Join(cfg.CacheDir, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// Partial files from a previous process. Only one worker may use a cache dir.
	if stale, err := filepath.Glob(filepath.Join(cfg.CacheDir, "tmp", "*.part")); err == nil {
		for _, name := range stale {
			_ = os.Remove(name)
		}
	}
	initialSlots := cleanSlots(cfg.Slots)
	w := &Worker{
		cfg: cfg, base: base, started: time.Now().UTC(), sampler: sysstat.New(),
		running: map[string]*execution{}, pins: map[string]int{}, downloads: map[string]chan struct{}{},
		slots: initialSlots, cacheMax: cfg.CacheMax, heartbeatEvery: cfg.Heartbeat, claimWait: cfg.ClaimWait,
		ocrProviderIDs: []string{}, configured: cfg.Slots != nil,
	}
	if cfg.ClaimWait <= 0 {
		w.claimWait = 25 * time.Second
	}
	return w, nil
}

type remoteConfig struct {
	Slots          map[string]int `json:"slots"`
	CacheMaxBytes  int64          `json:"cache_max_bytes"`
	OCRProviderIDs []string       `json:"ocr_provider_ids"`
	HeartbeatMS    int64          `json:"heartbeat_ms"`
	ClaimWaitMS    int64          `json:"claim_wait_ms"`
	Revision       int64          `json:"revision"`
}

type heartbeatReply struct {
	LostJobIDs []string      `json:"lost_job_ids"`
	Config     *remoteConfig `json:"config"`
}

// Run blocks until ctx ends. The first heartbeat is synchronous so a production
// worker receives its slots from the master before claiming any work.
func (w *Worker) Run(ctx context.Context) {
	w.heartbeat(ctx)
	w.loopMu.Lock()
	w.parent = ctx
	w.loopMu.Unlock()
	w.reconcileClaimLoops()

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		w.heartbeatLoop(ctx)
	}()
	<-ctx.Done()
	w.stopClaimLoops()
	<-heartbeatDone
	w.stopClaimLoops()
	w.stopAll()
	w.loopWG.Wait()
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	for {
		wait := w.heartbeatInterval()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if !w.heartbeat(ctx) && ctx.Err() != nil {
				return
			}
		}
	}
}

func (w *Worker) heartbeat(ctx context.Context) bool {
	slots, _, cacheMax, _ := w.runtimeSnapshot()
	w.mu.Lock()
	ids := make([]string, 0, len(w.running))
	for id := range w.running {
		ids = append(ids, id)
	}
	w.mu.Unlock()
	revision := w.configRevision()
	body := map[string]any{
		"version": w.cfg.Version, "protocol": jobexec.Protocol, "started_at": w.started,
		"kinds": slotKinds(slots), "slots": slots, "running_job_ids": ids,
		"sample": w.sampler.Sample(ctx), "cache_used": w.cacheUsed(), "cache_max": cacheMax,
		"config_revision": revision,
	}
	var reply heartbeatReply
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	status, err := w.callStatus(callCtx, http.MethodPost, "/internal/v1/heartbeat", body, &reply)
	if err != nil {
		if ctx.Err() == nil {
			w.cfg.Logf("heartbeat failed: %s", jobexec.Redact(err.Error()))
		}
		if status == http.StatusUnauthorized {
			// Disabled by an administrator: stop using the credentials this node holds.
			w.stopClaimLoops()
			w.stopAll()
		}
		return false
	}
	if reply.Config != nil {
		w.applyRemoteConfig(*reply.Config)
	}
	for _, id := range reply.LostJobIDs {
		w.mu.Lock()
		run := w.running[id]
		w.mu.Unlock()
		if run != nil {
			w.cfg.Logf("job %s lease lost; stopping it", id)
			run.cancel(errLost)
		}
	}
	return true
}

func (w *Worker) applyRemoteConfig(config remoteConfig) {
	slots := cleanSlots(config.Slots)
	heartbeat := time.Duration(config.HeartbeatMS) * time.Millisecond
	if heartbeat <= 0 {
		heartbeat = w.heartbeatInterval()
	}
	claimWait := time.Duration(config.ClaimWaitMS) * time.Millisecond
	if claimWait <= 0 {
		claimWait = w.claimInterval()
	}

	w.runtimeMu.Lock()
	changed := !w.configured || w.revision != config.Revision ||
		!sameSlots(w.slots, slots) || w.cacheMax != config.CacheMaxBytes ||
		!slices.Equal(w.ocrProviderIDs, config.OCRProviderIDs)
	w.slots, w.cacheMax = slots, config.CacheMaxBytes
	w.ocrProviderIDs = append([]string{}, config.OCRProviderIDs...)
	w.heartbeatEvery, w.claimWait = heartbeat, claimWait
	w.revision, w.configured = config.Revision, true
	w.runtimeMu.Unlock()
	if changed {
		w.reconcileClaimLoops()
	}
}

func (w *Worker) reconcileClaimLoops() {
	w.loopMu.Lock()
	defer w.loopMu.Unlock()
	if w.parent == nil {
		return
	}
	if w.loopCancel != nil {
		w.loopCancel()
	}
	if !w.isConfigured() {
		return
	}
	loopCtx, cancel := context.WithCancel(w.parent)
	w.loopCancel = cancel
	slots := w.slotsSnapshot()
	for kind, count := range slots {
		for range count {
			w.loopWG.Add(1)
			go func(kind string) {
				defer w.loopWG.Done()
				w.claimLoop(loopCtx, w.parent, kind)
			}(kind)
		}
	}
}

func (w *Worker) stopClaimLoops() {
	w.loopMu.Lock()
	if w.loopCancel != nil {
		w.loopCancel()
		w.loopCancel = nil
	}
	w.loopMu.Unlock()
}

func (w *Worker) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, run := range w.running {
		run.cancel(errLost)
	}
}

func cleanSlots(in map[string]int) map[string]int {
	out := map[string]int{}
	for kind, count := range in {
		if jobexec.IsRemoteKind(kind) && count > 0 {
			if count > 64 {
				count = 64
			}
			out[kind] = count
		}
	}
	return out
}

func slotKinds(slots map[string]int) []string {
	out := make([]string, 0, len(slots))
	for kind, count := range slots {
		if count > 0 {
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

func sameSlots(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for kind, count := range a {
		if b[kind] != count {
			return false
		}
	}
	return true
}

func (w *Worker) runtimeSnapshot() (map[string]int, []string, int64, int64) {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return cleanSlots(w.slots), append([]string{}, w.ocrProviderIDs...), w.cacheMax, w.revision
}

func (w *Worker) slotsSnapshot() map[string]int {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return cleanSlots(w.slots)
}

func (w *Worker) ocrProviderIDsSnapshot() []string {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return append([]string{}, w.ocrProviderIDs...)
}

func (w *Worker) cacheMaxValue() int64 {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return w.cacheMax
}

func (w *Worker) heartbeatInterval() time.Duration {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	if w.heartbeatEvery <= 0 {
		return 10 * time.Second
	}
	return w.heartbeatEvery
}

func (w *Worker) claimInterval() time.Duration {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	if w.claimWait <= 0 {
		return 25 * time.Second
	}
	return w.claimWait
}

func (w *Worker) configRevision() int64 {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return w.revision
}

func (w *Worker) isConfigured() bool {
	w.runtimeMu.RLock()
	defer w.runtimeMu.RUnlock()
	return w.configured
}

func (w *Worker) claimLoop(loopCtx, jobCtx context.Context, kind string) {
	backoff := time.Second
	for loopCtx.Err() == nil {
		started := time.Now()
		var in jobexec.Input
		claimWait := w.claimInterval()
		callCtx, cancel := context.WithTimeout(loopCtx, claimWait+claimGrace)
		status, err := w.callStatus(callCtx, http.MethodPost, "/internal/v1/jobs/claim", map[string]any{
			"kinds": []string{kind}, "wait_ms": claimWait.Milliseconds(),
			"protocol": jobexec.Protocol, "ocr_provider_ids": w.ocrProviderIDsSnapshot(),
		}, &in)
		cancel()
		if err != nil {
			if loopCtx.Err() != nil {
				return
			}
			w.cfg.Logf("claim %s failed: %s", kind, jobexec.Redact(err.Error()))
			sleep(loopCtx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if status == http.StatusNoContent {
			if elapsed := time.Since(started); elapsed < minClaimPeriod {
				sleep(loopCtx, minClaimPeriod-elapsed)
			}
			continue
		}
		// A configuration revision may cancel the claim loop after the master
		// has leased this job. Finish that job with the root worker context.
		w.execute(jobCtx, in)
	}
}

func (w *Worker) execute(parent context.Context, in jobexec.Input) {
	ctx, cancel := context.WithCancelCause(parent)
	run := &execution{cancel: cancel}
	w.mu.Lock()
	if stale := w.running[in.JobID]; stale != nil {
		// The lease expired while this node was cut off and it won the job back;
		// the lease owner is the same node, so only this process can fence it.
		stale.cancel(errLost)
	}
	w.running[in.JobID] = run
	w.mu.Unlock()
	env := &remoteEnv{w: w, jobID: in.JobID}
	defer func() {
		w.mu.Lock()
		if w.running[in.JobID] == run {
			delete(w.running, in.JobID)
		}
		w.mu.Unlock()
		cancel(nil)
		env.release()
	}()
	out, err := jobexec.Run(ctx, env, in)
	// Reports outlive a shutdown so the master can requeue at once.
	report, done := context.WithTimeout(context.WithoutCancel(parent), reportTimeout)
	defer done()
	switch {
	case errors.Is(context.Cause(ctx), errLost):
		return
	case err != nil && ctx.Err() != nil:
		_ = w.call(report, http.MethodPost, "/internal/v1/jobs/"+in.JobID+"/fail", map[string]any{"interrupted": true}, nil)
	case err != nil:
		message, extra := jobexec.Failure(in.Kind, err)
		w.cfg.Logf("%s job %s failed: %s", in.Kind, in.JobID, message)
		_ = w.call(report, http.MethodPost, "/internal/v1/jobs/"+in.JobID+"/fail", map[string]any{"error": message, "extra": extra}, nil)
	default:
		status, err := w.complete(report, in.JobID, out)
		if err == nil {
			return
		}
		w.cfg.Logf("report %s job %s failed: %s", in.Kind, in.JobID, jobexec.Redact(err.Error()))
		// A result the master refuses would be refused again after every requeue.
		if status >= 400 && status < 500 && status != http.StatusConflict && status != http.StatusUnauthorized {
			_ = w.call(report, http.MethodPost, "/internal/v1/jobs/"+in.JobID+"/fail", map[string]any{"error": "主端拒绝了任务结果：" + jobexec.Redact(err.Error())}, nil)
		}
	}
}

// complete sends a gzip-compressed result, retrying transient failures. The
// master acknowledges a repeated delivery of an already committed result.
func (w *Worker) complete(ctx context.Context, jobID string, out jobexec.Result) (int, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(out); err != nil {
		return 0, err
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	var err error
	var status int
	for attempt := 0; attempt < 4; attempt++ {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, w.url("/internal/v1/jobs/"+jobID+"/complete"), bytes.NewReader(buf.Bytes()))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		status, err = w.do(req, nil)
		if err == nil || (status >= 400 && status < 500) {
			return status, err
		}
		sleep(ctx, time.Duration(attempt+1)*2*time.Second)
	}
	return status, err
}

func (w *Worker) url(path string) string { return w.base.String() + path }

func (w *Worker) call(ctx context.Context, method, path string, body, out any) error {
	_, err := w.callStatus(ctx, method, path, body, out)
	return err
}

func (w *Worker) callStatus(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.url(path), reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return w.do(req, out)
}

func (w *Worker) do(req *http.Request, out any) (int, error) {
	req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
	resp, err := w.cfg.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var env struct {
			Error struct {
				Code, Message string
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&env)
		return resp.StatusCode, fmt.Errorf("主端返回 HTTP %d: %s", resp.StatusCode, env.Error.Message)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, json.Unmarshal(env.Data, out)
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
