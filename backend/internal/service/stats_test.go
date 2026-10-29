package service

import (
	"context"
	"testing"

	"github.com/nic/paper-collect/backend/internal/model"
)

func TestStatusStatistics(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "stats")
	ctx := context.Background()
	materials, err := svc.GetMaterialStats(ctx)
	if err != nil || materials != (MaterialStats{}) {
		t.Fatalf("empty material stats: %+v %v", materials, err)
	}
	jobs, err := svc.GetJobStats(ctx)
	if err != nil || jobs != (JobStats{}) {
		t.Fatalf("empty job stats: %+v %v", jobs, err)
	}
	for _, status := range []string{model.StatusArchived, model.StatusArchived, model.StatusPendingStorage, model.StatusDegraded, "future_status"} {
		if err := db.Create(&model.Material{ID: newID(), Status: status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{model.JobPending, model.JobRunning, model.JobSucceeded, model.JobSucceeded, model.JobFailed, "future_status"} {
		if err := db.Create(&model.Job{ID: newID(), Kind: model.JobIngest, Status: status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	materials, err = svc.GetMaterialStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (MaterialStats{Total: 5, PendingStorage: 1, Archived: 2, Degraded: 1}); materials != want {
		t.Fatalf("material stats: got %+v, want %+v", materials, want)
	}
	jobs, err = svc.GetJobStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (JobStats{Total: 6, Pending: 1, Running: 1, Succeeded: 2, Failed: 1}); jobs != want {
		t.Fatalf("job stats: got %+v, want %+v", jobs, want)
	}
}
