package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
)

var cancelAdmin = auth.Principal{UserID: "admin", Role: model.RoleAdmin}

// A canceled job ends failed with its file's index state, and can run again.
func TestCancelPendingIndexJobFailsItsProjection(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	materialID := newID()
	job := addPendingJob(t, conn, model.JobIndex, materialID, time.Now().UTC())
	state := model.MaterialIndexState{MaterialID: materialID, Status: "pending", JobID: job.ID, UpdatedAt: time.Now().UTC()}
	if err := conn.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	got, err := svc.CancelJob(context.Background(), cancelAdmin, job.ID)
	if err != nil || got.Status != model.JobFailed || got.Error != "已由管理员取消" {
		t.Fatalf("cancel = %+v, %v", got, err)
	}
	if err := conn.Where("material_id = ?", materialID).First(&state).Error; err != nil || state.Status != model.AnalysisFailed {
		t.Fatalf("index state = %+v, %v; want failed, not stuck waiting", state, err)
	}
	var audits int64
	conn.Model(&model.Audit{}).Where("action = ? AND target_id = ?", "job.cancel", job.ID).Count(&audits)
	if audits != 1 {
		t.Fatalf("audit rows = %d", audits)
	}
	if retried, err := svc.RetryJob(context.Background(), cancelAdmin, job.ID); err != nil || retried.Status != model.JobPending {
		t.Fatalf("retry after cancel = %+v, %v", retried, err)
	}
}

// Canceling a job running on the master stops it at once and fences its result.
func TestCancelRunningLocalJobStopsExecution(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	addPendingJob(t, conn, model.JobIndex, newID(), time.Now().UTC())
	jobs, err := svc.claimBatch(context.Background(), []claimRequest{{Kind: model.JobIndex, Limit: 1, Owner: claimer{ID: "master"}}})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim = %v, %v", jobs, err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	release := svc.holdLease(jobs[0], cancel)
	defer release()
	if _, err := svc.CancelJob(context.Background(), cancelAdmin, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(ctx), errLeaseLost) {
		t.Fatalf("execution cause = %v, want stopped without requeue", context.Cause(ctx))
	}
	if svc.failJob(jobs[0].ID, jobs[0].LeaseOwner, "late result") {
		t.Fatal("the canceled execution could still write its outcome")
	}
	var lock int64
	conn.Model(&model.JobLock{}).Where("job_id = ?", jobs[0].ID).Count(&lock)
	if lock != 0 {
		t.Fatal("canceled job kept its resource lock")
	}
}

// A worker node learns about the cancel from its next heartbeat.
func TestCancelRunningRemoteJobIsLostOnHeartbeat(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	node := addWorkerNode(t, conn, false)
	addPendingJob(t, conn, model.JobIndex, newID(), time.Now().UTC())
	jobs, err := svc.claimBatch(context.Background(), []claimRequest{{Kind: model.JobIndex, Limit: 1, Owner: claimer{ID: node.ID, RemoteOnly: true}}})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim = %v, %v", jobs, err)
	}
	if _, err := svc.CancelJob(context.Background(), cancelAdmin, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	reply, err := svc.NodeHeartbeat(context.Background(), node, Heartbeat{Running: []string{jobs[0].ID}}, "127.0.0.1")
	if err != nil || !slices.Contains(reply.LostJobIDs, jobs[0].ID) {
		t.Fatalf("heartbeat lost = %v, %v", reply.LostJobIDs, err)
	}
}

func TestCancelFinishedJobConflicts(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	job := addPendingJob(t, conn, model.JobIndex, newID(), time.Now().UTC())
	conn.Model(&model.Job{}).Where("id = ?", job.ID).Update("status", model.JobSucceeded)
	var ae *apperr.Error
	if _, err := svc.CancelJob(context.Background(), cancelAdmin, job.ID); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("cancel finished job = %v, want 409", err)
	}
}
