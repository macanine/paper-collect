package service

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

func newWorkerTestService(t *testing.T, dir, instance string) (*Service, *gorm.DB) {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	store, err := blob.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	box, err := cryptobox.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewSigner(strings.Repeat("s", 32), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Options{
		DB: conn, Blobs: store, Box: box, Signer: signer,
		Limiter: auth.NewLimiter(time.Now, 100, time.Minute, time.Minute),
		Factory: storage.New, Logf: t.Logf,
		Worker: WorkerSettings{
			InstanceID: instance, LeaseDuration: time.Minute,
			HeartbeatInterval: 10 * time.Second, PollInterval: 10 * time.Millisecond,
		},
		ResetAllRunningOnStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, conn
}

func TestConcurrentInstancesClaimEachJobOnce(t *testing.T) {
	dir := t.TempDir()
	svc1, conn := newWorkerTestService(t, dir, "worker-1")
	svc2, _ := newWorkerTestService(t, dir, "worker-2")
	for i := 0; i < 40; i++ {
		job := model.Job{
			ID: newID(), Kind: model.JobAnalyze, Status: model.JobPending,
			MaterialID: newID(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := conn.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}

	type ownerCount struct {
		sync.Mutex
		ids map[string]int
	}
	counts := ownerCount{ids: map[string]int{}}
	var wg sync.WaitGroup
	for _, svc := range []*Service{svc1, svc2} {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(s *Service) {
				defer wg.Done()
				for {
					job, err := s.claimJob(context.Background(), model.JobAnalyze)
					if err == errNoJob {
						return
					}
					if err != nil {
						t.Errorf("claim: %v", err)
						return
					}
					counts.Lock()
					counts.ids[job.ID]++
					counts.Unlock()
				}
			}(svc)
		}
	}
	wg.Wait()
	if len(counts.ids) != 40 {
		t.Fatalf("claimed %d distinct jobs, want 40", len(counts.ids))
	}
	for id, n := range counts.ids {
		if n != 1 {
			t.Fatalf("job %s claimed %d times", id, n)
		}
	}
}

func TestMaterialLockSerializesAnalyzeAndIndex(t *testing.T) {
	dir := t.TempDir()
	svc, conn := newWorkerTestService(t, dir, "worker-1")
	materialID := newID()
	now := time.Now().UTC()
	for i, kind := range []string{model.JobAnalyze, model.JobIndex} {
		job := model.Job{
			ID: newID(), Kind: kind, Status: model.JobPending,
			MaterialID: materialID, CreatedAt: now.Add(time.Duration(i) * time.Second), UpdatedAt: now,
		}
		if err := conn.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.claimJob(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.claimJob(context.Background(), ""); err != errNoJob {
		t.Fatalf("second claim error = %v, want errNoJob", err)
	}
	if !svc.succeedJob(first.ID, first.LeaseOwner) {
		t.Fatal("complete job failed")
	}
	second, err := svc.claimJob(context.Background(), model.JobIndex)
	if err != nil {
		t.Fatal(err)
	}
	if second.MaterialID != materialID || second.ID == first.ID || second.Kind != model.JobIndex {
		t.Fatalf("unexpected second job: %+v", second)
	}
}

func TestExpiredLeaseCanBeReclaimedButStaleOwnerCannotFinish(t *testing.T) {
	dir := t.TempDir()
	svc, conn := newWorkerTestService(t, dir, "worker-1")
	job := model.Job{
		ID: newID(), Kind: model.JobAnalyze, Status: model.JobPending,
		MaterialID: newID(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	first, err := svc.claimJob(context.Background(), model.JobAnalyze)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	if err := conn.Model(&model.Job{}).Where("id = ?", job.ID).
		Updates(map[string]any{"lease_until": past}).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.JobLock{}).Where("job_id = ?", job.ID).
		Updates(map[string]any{"lease_until": past}).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.Job{}).Where("id = ?", job.ID).
		Updates(map[string]any{"status": model.JobPending, "lease_owner": ""}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.claimJob(context.Background(), model.JobAnalyze)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Attempts != first.Attempts+1 {
		t.Fatalf("lease was not reclaimed: first=%+v second=%+v", first, second)
	}
	if svc.succeedJob(first.ID, "stale-owner") {
		t.Fatal("stale owner completed a reclaimed job")
	}
}

func TestRecoverExpiredLeasesResetsProjectionAndReleasesLock(t *testing.T) {
	dir := t.TempDir()
	svc, conn := newWorkerTestService(t, dir, "worker-1")
	materialID := newID()
	analysis := model.MaterialAnalysis{
		ID: newID(), MaterialID: materialID, Status: model.AnalysisRunning,
		ImagesJSON: "[]", SuggestionsJSON: "{}", JobID: newID(),
	}
	job := model.Job{
		ID: analysis.JobID, Kind: model.JobAnalyze, Status: model.JobRunning,
		MaterialID: materialID, LeaseOwner: "dead-worker",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	past := time.Now().UTC().Add(-time.Minute)
	job.LeaseUntil = &past
	if err := conn.Create(&analysis).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	lock := model.JobLock{Key: "material:" + materialID, JobID: job.ID, LeaseUntil: past, UpdatedAt: past}
	if err := conn.Create(&lock).Error; err != nil {
		t.Fatal(err)
	}

	recovered, err := svc.RecoverExpiredLeases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	var gotJob model.Job
	if err := conn.Where("id = ?", job.ID).First(&gotJob).Error; err != nil {
		t.Fatal(err)
	}
	if gotJob.Status != model.JobPending || gotJob.LeaseOwner != "" || gotJob.LeaseUntil != nil {
		t.Fatalf("job was not reset: %+v", gotJob)
	}
	var gotAnalysis model.MaterialAnalysis
	if err := conn.Where("id = ?", analysis.ID).First(&gotAnalysis).Error; err != nil {
		t.Fatal(err)
	}
	if gotAnalysis.Status != model.AnalysisPending {
		t.Fatalf("analysis status = %q, want pending", gotAnalysis.Status)
	}
	var locks int64
	if err := conn.Model(&model.JobLock{}).Where("job_id = ?", job.ID).Count(&locks).Error; err != nil {
		t.Fatal(err)
	}
	if locks != 0 {
		t.Fatalf("expired job lock still present: %d", locks)
	}
}

func TestLiveLeaseIsNotRecovered(t *testing.T) {
	dir := t.TempDir()
	svc, conn := newWorkerTestService(t, dir, "worker-1")
	future := time.Now().UTC().Add(time.Minute)
	job := model.Job{
		ID: newID(), Kind: model.JobAnalyze, Status: model.JobRunning,
		MaterialID: newID(), LeaseOwner: "healthy-worker", LeaseUntil: &future,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.RecoverExpiredLeases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want 0", recovered)
	}
	var got model.Job
	if err := conn.Where("id = ?", job.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != model.JobRunning || got.LeaseOwner != "healthy-worker" {
		t.Fatalf("healthy lease was modified: %+v", got)
	}
}
