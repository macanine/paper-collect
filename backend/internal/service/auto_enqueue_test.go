package service

import (
	"errors"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func TestAutomaticAnalysisAndIndexJobsAreIdempotent(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "auto-enqueue")
	if err := db.Create(&model.AISettings{ID: 1, AnalysisEnabled: true, MaxPages: 3, MaxImages: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SearchSettings{ID: 1, Enabled: true, MaxPages: 500, BatchPages: 10, BatchChars: 12000}).Error; err != nil {
		t.Fatal(err)
	}
	pdf1 := &plannedMaterial{ID: newID(), MediaType: "application/pdf", Role: model.RoleExam}
	pdf2 := &plannedMaterial{ID: newID(), MediaType: "application/pdf", Role: model.RoleQuestionsAnswers}
	lecture := &plannedMaterial{ID: newID(), MediaType: "application/pdf", Role: model.RoleLecture}
	answer := &plannedMaterial{ID: newID(), MediaType: "application/pdf", Role: model.RoleAnswer}
	teacher := &plannedMaterial{ID: newID(), MediaType: "application/pdf", Role: model.RoleTeacherEdition}
	zip := &plannedMaterial{ID: newID(), MediaType: "application/zip", Role: model.RoleExam}
	candidates := []*plannedMaterial{pdf1, pdf2, lecture, answer, teacher, zip, pdf1}
	now := time.Now().UTC()
	enqueue := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := svc.autoEnqueueAnalysesTx(tx, candidates, now); err != nil {
				return err
			}
			return svc.autoEnqueueIndexTx(tx, candidates, now)
		})
	}
	if err := enqueue(); err != nil {
		t.Fatal(err)
	}
	if err := enqueue(); err != nil {
		t.Fatal(err)
	}
	var jobs []model.Job
	if err := db.Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 7 {
		t.Fatalf("expected five analysis and two index jobs, got %d", len(jobs))
	}
	for _, candidate := range []*plannedMaterial{pdf1, pdf2, lecture, answer, teacher} {
		var analysis model.MaterialAnalysis
		if err := db.Where("material_id = ?", candidate.ID).First(&analysis).Error; err != nil {
			t.Fatal(err)
		}
		var analysisJob model.Job
		if err := db.Where("id = ?", analysis.JobID).First(&analysisJob).Error; err != nil || analysisJob.Kind != model.JobAnalyze || analysisJob.MaterialID != candidate.ID {
			t.Fatal("analysis and its job are inconsistent")
		}
	}
	for _, candidate := range []*plannedMaterial{pdf1, pdf2} {
		var state model.MaterialIndexState
		if err := db.Where("material_id = ?", candidate.ID).First(&state).Error; err != nil {
			t.Fatal(err)
		}
		var indexJob model.Job
		if err := db.Where("id = ?", state.JobID).First(&indexJob).Error; err != nil || indexJob.Kind != model.JobIndex || indexJob.MaterialID != candidate.ID {
			t.Fatal("index state and its job are inconsistent")
		}
	}
	for _, candidate := range []*plannedMaterial{lecture, answer, teacher} {
		var count int64
		if err := db.Model(&model.MaterialIndexState{}).Where("material_id = ?", candidate.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("ineligible role %s must not be indexed", candidate.Role)
		}
	}
	var count int64
	if err := db.Model(&model.Job{}).Where("material_id = ?", zip.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("ZIP container must not be enqueued")
	}
}

func TestAutomaticAnalysisRollsBackWhenJobFails(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "auto-analysis-rollback")
	if err := db.Create(&model.AISettings{ID: 1, AnalysisEnabled: true, MaxPages: 3, MaxImages: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail-auto-analysis-job", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.Job); ok {
			tx.AddError(errors.New("simulated failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	material := &plannedMaterial{ID: newID(), MediaType: "application/pdf"}
	err := db.Transaction(func(tx *gorm.DB) error {
		return svc.autoEnqueueAnalysesTx(tx, []*plannedMaterial{material}, time.Now())
	})
	if err == nil {
		t.Fatal("failed job creation must fail the transaction")
	}
	var count int64
	if err := db.Model(&model.MaterialAnalysis{}).Where("material_id = ?", material.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed job creation must not leave an analysis record")
	}
}
