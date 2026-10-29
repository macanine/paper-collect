package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
)

func TestQuestionIndexOnlyAppliesToExamRoles(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "index-eligibility")
	ctx := context.Background()
	now := time.Now().UTC()
	material := model.Material{
		ID: newID(), Title: "讲义", Description: "", Grade: "", TagsJSON: "[]",
		Role: model.RoleLecture, Status: model.StatusArchived, BlobID: "blob",
		FileName: "讲义.pdf", MediaType: "application/pdf", SizeBytes: 1,
		SHA256: strings.Repeat("a", 64), SourcePath: "讲义.pdf", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: "admin", Role: model.RoleAdmin}

	state, err := svc.GetMaterialIndexState(ctx, material.ID)
	if err != nil || state.Status != model.IndexStatusNotApplicable {
		t.Fatalf("lecture index state = %q, err=%v", state.Status, err)
	}
	if _, _, err := svc.EnqueueMaterialIndex(ctx, principal, material.ID, false); err == nil {
		t.Fatal("lecture must not be manually queued for indexing")
	}

	exam := model.RoleExam
	if _, err := svc.UpdateMaterial(ctx, principal, material.ID, MaterialPatch{Role: &exam}); err != nil {
		t.Fatal(err)
	}
	state, err = svc.GetMaterialIndexState(ctx, material.ID)
	if err != nil || state.Status != "pending" || state.JobID == "" {
		t.Fatalf("exam must be queued after role change: %+v err=%v", state, err)
	}

	lecture := model.RoleLecture
	if _, err := svc.UpdateMaterial(ctx, principal, material.ID, MaterialPatch{Role: &lecture}); err != nil {
		t.Fatal(err)
	}
	var stateCount, jobCount int64
	if err := db.Model(&model.MaterialIndexState{}).Where("material_id = ?", material.ID).Count(&stateCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Job{}).Where("material_id = ? AND kind = ?", material.ID, model.JobIndex).Count(&jobCount).Error; err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 || jobCount != 0 {
		t.Fatalf("non-exam role left index state/job: state=%d job=%d", stateCount, jobCount)
	}
}
