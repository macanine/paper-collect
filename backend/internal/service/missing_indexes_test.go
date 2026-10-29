package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func TestEnqueueMissingIndexesConcurrentAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	first, db := newWorkerTestService(t, dir, "index-first")
	second, _ := newWorkerTestService(t, dir, "index-second")
	ctx := context.Background()
	if _, err := first.ensureSearchSettings(ctx); err != nil {
		t.Fatal(err)
	}
	materials := []model.Material{
		{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleExam},
		{ID: newID(), Status: model.StatusDegraded, MediaType: "application/pdf", Role: model.RoleQuestionsAnswers},
		{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleLecture},
		{ID: newID(), Status: model.StatusArchived, MediaType: "application/zip", Role: model.RoleExam},
		{ID: newID(), Status: model.StatusPendingStorage, MediaType: "application/pdf", Role: model.RoleExam},
	}
	if err := db.Create(&materials).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MaterialIndexState{MaterialID: materials[2].ID, Status: "succeeded", JobID: "previous-job"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Job{ID: "previous-job", Kind: model.JobIndex, Status: model.JobSucceeded, MaterialID: materials[2].ID}).Error; err != nil {
		t.Fatal(err)
	}
	type result struct {
		n   int
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, svc := range []*Service{first, second} {
		wg.Add(1)
		go func(s *Service) { defer wg.Done(); n, err := s.EnqueueMissingIndexes(ctx); results <- result{n, err} }(svc)
	}
	wg.Wait()
	close(results)
	total := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		total += r.n
	}
	if total != 2 {
		t.Fatalf("queued %d jobs, expected 2", total)
	}
	if n, err := first.EnqueueMissingIndexes(ctx); err != nil || n != 0 {
		t.Fatalf("repeat queued %d jobs: %v", n, err)
	}
	var states []model.MaterialIndexState
	if err := db.Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d index states", len(states))
	}
	var jobs []model.Job
	if err := db.Where("kind = ? AND status = ?", model.JobIndex, model.JobPending).Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d index jobs", len(jobs))
	}
	for _, job := range jobs {
		var state model.MaterialIndexState
		if err := db.Where("material_id = ?", job.MaterialID).First(&state).Error; err != nil || state.JobID != job.ID {
			t.Fatal("job and index state must match")
		}
	}
}

func TestEnqueueMissingIndexesRollsBackStateWhenJobFails(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "index-rollback")
	ctx := context.Background()
	if _, err := svc.ensureSearchSettings(ctx); err != nil {
		t.Fatal(err)
	}
	material := model.Material{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleExam}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail-index-job", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.Job); ok {
			tx.AddError(errors.New("simulated failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.EnqueueMissingIndexes(ctx); err == nil || n != 0 {
		t.Fatal("job failure must be reported")
	}
	var count int64
	if err := db.Model(&model.MaterialIndexState{}).Where("material_id = ?", material.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed job must not leave an index state")
	}
}
