package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func addPendingJob(t *testing.T, conn *gorm.DB, kind, materialID string, created time.Time) model.Job {
	t.Helper()
	job := model.Job{ID: newID(), Kind: kind, Status: model.JobPending, MaterialID: materialID, CreatedAt: created, UpdatedAt: created}
	if err := conn.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

// One transaction claims every kind's free capacity, oldest first, and still
// honours per-file resource locks inside the batch.
func TestClaimBatchFillsEachKindsCapacity(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	base := time.Now().UTC().Add(-time.Hour)
	shared := newID()
	first := addPendingJob(t, conn, model.JobAnalyze, shared, base)
	addPendingJob(t, conn, model.JobAnalyze, shared, base.Add(time.Second)) // same file: locked
	third := addPendingJob(t, conn, model.JobAnalyze, newID(), base.Add(2*time.Second))
	addPendingJob(t, conn, model.JobAnalyze, newID(), base.Add(3*time.Second))
	for i := range 2 {
		addPendingJob(t, conn, model.JobIndex, newID(), base.Add(time.Duration(i)*time.Second))
	}
	owner := claimer{ID: "master", Name: "主端"}
	jobs, err := svc.claimBatch(context.Background(), []claimRequest{
		{Kind: model.JobAnalyze, Limit: 2, Owner: owner},
		{Kind: model.JobIndex, Limit: 5, Owner: owner},
	})
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string][]string{}
	for _, job := range jobs {
		if job.Status != model.JobRunning || job.LeaseOwner != "master" {
			t.Fatalf("claimed job not leased: %+v", job)
		}
		byKind[job.Kind] = append(byKind[job.Kind], job.ID)
	}
	if got := byKind[model.JobAnalyze]; len(got) != 2 || got[0] != first.ID || got[1] != third.ID {
		t.Fatalf("analyze claims = %v, want the oldest jobs on distinct files", got)
	}
	if got := byKind[model.JobIndex]; len(got) != 2 {
		t.Fatalf("index claims = %v, want both pending jobs", got)
	}
}

// One renewal transaction extends every live lease and stops only the
// executions whose lease moved to another owner.
func TestSharedLeaseRenewalStopsOnlyLostJobs(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	owner := claimer{ID: "master", Name: "主端"}
	base := time.Now().UTC().Add(-time.Hour)
	addPendingJob(t, conn, model.JobAnalyze, newID(), base)
	addPendingJob(t, conn, model.JobAnalyze, newID(), base.Add(time.Second))
	jobs, err := svc.claimBatch(context.Background(), []claimRequest{{Kind: model.JobAnalyze, Limit: 2, Owner: owner}})
	if err != nil || len(jobs) != 2 {
		t.Fatalf("claim = %v, %v", jobs, err)
	}
	if err := conn.Model(&model.Job{}).Where("id = ?", jobs[1].ID).Update("lease_owner", "elsewhere").Error; err != nil {
		t.Fatal(err)
	}
	causes := make([]error, 2)
	var leases []*localLease
	for i, job := range jobs {
		i := i
		leases = append(leases, &localLease{job: job, cancel: func(err error) { causes[i] = err }})
	}
	before := *jobs[0].LeaseUntil
	time.Sleep(5 * time.Millisecond)
	svc.renewLeases(leases)
	if causes[0] != nil || !errors.Is(causes[1], errLeaseLost) {
		t.Fatalf("causes = %v, want only the moved job stopped", causes)
	}
	var kept model.Job
	if err := conn.Where("id = ?", jobs[0].ID).First(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if !kept.LeaseUntil.After(before) {
		t.Fatalf("lease not extended: %v -> %v", before, kept.LeaseUntil)
	}
	var lock model.JobLock
	if err := conn.Where("job_id = ?", jobs[0].ID).First(&lock).Error; err != nil || !lock.LeaseUntil.After(before) {
		t.Fatalf("resource lock not extended: %+v %v", lock, err)
	}
}

// The dispatcher starts pending work as soon as it is notified and does not
// wait for its fallback poll.
func TestDispatcherStartsWorkOnNotify(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	settings := svc.workerSettings()
	settings.PollInterval = time.Hour
	svc.setWorkerSettings(settings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.dispatchLoop(ctx)
	time.Sleep(20 * time.Millisecond) // let the first pass find nothing
	job := addPendingJob(t, conn, model.JobAnalyze, newID(), time.Now().UTC())
	svc.notify()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got model.Job
		// Wait until the execution settles so it cannot log after the test.
		if err := conn.Where("id = ?", job.ID).First(&got).Error; err == nil && got.Attempts > 0 && got.Status != model.JobRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("notified job was not claimed before the fallback poll")
}

// Idle back-off grows, but never sleeps past a scheduled retry.
func TestIdleWaitStopsAtNextRetry(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	settings := svc.workerSettings()
	settings.PollInterval = time.Second
	svc.setWorkerSettings(settings)
	if wait := svc.idleWait(context.Background(), 20); wait != maxIdleWait {
		t.Fatalf("idle wait without retries = %v, want %v", wait, maxIdleWait)
	}
	job := addPendingJob(t, conn, model.JobAnalyze, newID(), time.Now().UTC())
	retry := time.Now().UTC().Add(2 * time.Second)
	if err := conn.Model(&model.Job{}).Where("id = ?", job.ID).Update("next_attempt_at", retry).Error; err != nil {
		t.Fatal(err)
	}
	if wait := svc.idleWait(context.Background(), 20); wait > 2*time.Second || wait < time.Second {
		t.Fatalf("idle wait = %v, want to wake for the retry in about 2s", wait)
	}
}
