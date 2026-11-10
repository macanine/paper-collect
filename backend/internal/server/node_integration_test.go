package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"github.com/nic/paper-collect/backend/internal/worker"
)

type nodeBody struct {
	Data struct {
		ID       string `json:"id"`
		Token    string `json:"token"`
		Status   string `json:"status"`
		IsMaster bool   `json:"is_master"`
		Running  int    `json:"running"`
		MemTotal int64  `json:"mem_total"`
	} `json:"data"`
}

// webdavDrive is a remote drive; workers store it through remoteDriveAt.
var webdavDrive = map[string]any{"name": "远端网盘", "kind": "webdav", "config": map[string]string{"base_url": "https://dav.example.test/"}, "secret": map[string]string{"username": "u", "password": "p"}}

// remoteDriveAt stands in for a cloud drive that only the worker talks to.
func remoteDriveAt(dir string) func(storage.Config) (storage.Adapter, error) {
	return func(cfg storage.Config) (storage.Adapter, error) {
		if cfg.Kind == model.KindWebDAV {
			cfg.Kind, cfg.Settings = model.KindLocal, map[string]string{"root": dir}
		}
		return storage.New(cfg)
	}
}

// A worker node without database or DATA_DIR claims a replicate job from the
// master, downloads the verified original, uploads it, and reports the result.
func TestWorkerNodeExecutesReplicateJob(t *testing.T) {
	env := newEnvWith(t, 0, []string{model.JobAnalyze})
	configureCatalogAI(t, env, newFakeAI(t))
	driveID := env.syncAllToDrive(t, webdavDrive)
	uploadID, _ := env.upload(t, "从端测试", map[string][]byte{"学生版.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)

	var replica model.Replica
	if err := env.db.Where("drive_id = ?", driveID).First(&replica).Error; err != nil || replica.Status != model.ReplicaPending {
		t.Fatalf("master must leave the replicate job to nodes: %+v %v", replica, err)
	}

	status, body := env.do(t, http.MethodPost, "/api/v1/nodes", map[string]any{"name": "家里 NAS"}, nil)
	var created nodeBody
	if status != http.StatusCreated || json.Unmarshal(body, &created) != nil || !strings.HasPrefix(created.Data.Token, "nd-") {
		t.Fatalf("create node %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/nodes", nil, nil)
	if status != http.StatusOK || strings.Contains(string(body), created.Data.Token) {
		t.Fatalf("node list must not expose tokens %d: %s", status, body)
	}

	// The node token works only on the node protocol, and user tokens never do.
	nodeAuth := map[string]string{"Authorization": "Bearer " + created.Data.Token}
	if status, _ := env.do(t, http.MethodGet, "/api/v1/materials", nil, nodeAuth); status != http.StatusUnauthorized {
		t.Fatalf("node token on public API = %d, want 401", status)
	}
	if status, _ := env.do(t, http.MethodPost, "/internal/v1/heartbeat", map[string]any{}, nil); status != http.StatusUnauthorized {
		t.Fatalf("user token on node protocol = %d, want 401", status)
	}

	cacheDir := t.TempDir()
	remoteDir := t.TempDir()
	w, err := worker.New(worker.Config{
		MasterURL: env.base, Token: created.Data.Token, Version: "test", CacheDir: cacheDir, CacheMax: 1 << 30,
		Slots: map[string]int{"replicate": 1}, Heartbeat: 50 * time.Millisecond, ClaimWait: 200 * time.Millisecond,
		Factory: remoteDriveAt(remoteDir), Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := env.db.Where("id = ?", replica.ID).First(&replica).Error; err != nil {
			t.Fatal(err)
		}
		if replica.Status == model.ReplicaReady || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if replica.Status != model.ReplicaReady || replica.Verification != "sha256" {
		t.Fatalf("replica after node run: %+v", replica)
	}
	if _, err := os.Stat(filepath.Join(remoteDir, filepath.FromSlash(replica.RemotePath))); err != nil {
		t.Fatalf("uploaded object missing: %v", err)
	}
	var job model.Job
	if err := env.db.Where("replica_id = ?", replica.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobSucceeded || job.NodeID != created.Data.ID || job.NodeName != "家里 NAS" {
		t.Fatalf("job must record the executing node: %+v", job)
	}
	if entries, _ := os.ReadDir(filepath.Join(cacheDir, "blobs")); len(entries) != 1 {
		t.Fatalf("node cache entries = %d, want 1", len(entries))
	}

	time.Sleep(120 * time.Millisecond) // at least one heartbeat with a sample
	status, body = env.do(t, http.MethodGet, "/api/v1/nodes", nil, nil)
	var listed struct {
		Data []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			IsMaster bool   `json:"is_master"`
			MemTotal int64  `json:"mem_total"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &listed) != nil || len(listed.Data) != 2 || !listed.Data[0].IsMaster {
		t.Fatalf("list nodes %d: %s", status, body)
	}
	if listed.Data[1].Status != "online" || listed.Data[1].MemTotal == 0 {
		t.Fatalf("worker heartbeat not recorded: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/nodes/"+created.Data.ID+"/metrics", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "cpu_percent") {
		t.Fatalf("node metrics %d: %s", status, body)
	}

	// A disabled node is rejected at once; its leases are left to expire.
	disabled := false
	if status, body := env.do(t, http.MethodPatch, "/api/v1/nodes/"+created.Data.ID, map[string]any{"enabled": disabled}, nil); status != http.StatusOK {
		t.Fatalf("disable node %d: %s", status, body)
	}
	if status, _ := env.do(t, http.MethodPost, "/internal/v1/heartbeat", map[string]any{}, nodeAuth); status != http.StatusUnauthorized {
		t.Fatalf("disabled node heartbeat = %d, want 401", status)
	}
}

// A node can only report on jobs it currently leases.
func TestNodeCannotCompleteAnotherNodesJob(t *testing.T) {
	env := newEnvWith(t, 0, []string{model.JobAnalyze})
	configureCatalogAI(t, env, newFakeAI(t))
	env.syncAllToDrive(t, webdavDrive)
	uploadID, _ := env.upload(t, "租约测试", map[string][]byte{"学生版.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	token := func(name string) string {
		status, body := env.do(t, http.MethodPost, "/api/v1/nodes", map[string]any{"name": name}, nil)
		var created nodeBody
		if status != http.StatusCreated || json.Unmarshal(body, &created) != nil {
			t.Fatalf("create node %d: %s", status, body)
		}
		return created.Data.Token
	}
	a := map[string]string{"Authorization": "Bearer " + token("A")}
	b := map[string]string{"Authorization": "Bearer " + token("B")}
	status, body := env.do(t, http.MethodPost, "/internal/v1/jobs/claim", map[string]any{"kinds": []string{"replicate"}, "protocol": jobexec.Protocol}, a)
	var claimed struct {
		Data struct {
			JobID     string `json:"job_id"`
			Replicate struct {
				Drive struct {
					Kind string `json:"kind"`
				} `json:"drive"`
			} `json:"replicate"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &claimed) != nil || claimed.Data.JobID == "" || claimed.Data.Replicate.Drive.Kind != "webdav" {
		t.Fatalf("claim %d: %s", status, body)
	}
	if status, _ := env.do(t, http.MethodPost, "/internal/v1/jobs/"+claimed.Data.JobID+"/fail", map[string]any{"error": "x"}, b); status != http.StatusConflict {
		t.Fatalf("other node fail = %d, want 409", status)
	}
	if status, _ := env.do(t, http.MethodPost, "/internal/v1/jobs/"+claimed.Data.JobID+"/fail", map[string]any{"interrupted": true}, a); status != http.StatusNoContent {
		t.Fatalf("owner interrupt = %d, want 204", status)
	}
	var job model.Job
	if err := env.db.Where("id = ?", claimed.Data.JobID).First(&job).Error; err != nil || job.Status != model.JobPending {
		t.Fatalf("interrupted job must return to the queue: %+v %v", job, err)
	}
	if status, _ := env.do(t, http.MethodGet, "/internal/v1/blobs/"+strings.Repeat("0", 64), nil, a); status != http.StatusNotFound {
		t.Fatalf("unknown blob = %d, want 404", status)
	}
}
