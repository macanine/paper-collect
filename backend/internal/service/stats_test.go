package service

import (
	"context"
	"testing"

	"github.com/nic/paper-collect/backend/internal/model"
)

func TestStatusStatistics(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "stats")
	ctx := context.Background()
	materials, err := svc.GetLibraryStats(ctx)
	if err != nil || materials != (LibraryStats{}) {
		t.Fatalf("empty material stats: %+v %v", materials, err)
	}
	jobs, err := svc.GetJobStats(ctx)
	if err != nil || jobs != (JobStats{}) {
		t.Fatalf("empty job stats: %+v %v", jobs, err)
	}
	stages := []string{model.StageReady, model.StageReady, model.StageReview, model.StageProcessing, model.StageFailed}
	syncs := []string{model.SyncSynced, model.SyncSyncing, model.SyncWaitingCatalog, model.SyncAttention, model.SyncNotRequired}
	for i, status := range []string{model.StatusArchived, model.StatusArchived, model.StatusMissing, model.StatusMissing, "future_status"} {
		if err := db.Create(&model.Material{ID: newID(), Status: status, SyncStatus: syncs[i], Stage: stages[i], SHA256: testSHA(), ReviewReasonsJSON: "[]"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Pack{ID: newID(), DocType: "joint", SubjectsJSON: "[]", TagsJSON: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{model.JobPending, model.JobRunning, model.JobSucceeded, model.JobSucceeded, model.JobFailed, "future_status"} {
		if err := db.Create(&model.Job{ID: newID(), Kind: model.JobIngest, Status: status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	materials, err = svc.GetLibraryStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (LibraryStats{Packs: 1, Files: 5, Processing: 1, Review: 1, Ready: 2, Failed: 1, MissingAnswers: 1,
		Archived: 2, Missing: 2, WaitingCatalog: 1, Syncing: 1, Synced: 1, Attention: 1, NotRequired: 1}); materials != want {
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
