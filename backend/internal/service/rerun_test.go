package service

import (
	"context"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
)

// A card made from the file name alone is not a reading, so a full re-run
// reads the file again instead of grouping it on that card.
func TestRerunFullReadsNameOnlyCardsAgain(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("试题", "试题.pdf", []byte("rerun-name-only"), nil)
	now := time.Now().UTC()
	if err := e.conn.Model(&model.Material{}).Where("id = ?", m.ID).
		Updates(map[string]any{"stage": model.StageAttention, "ai_step": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.conn.Save(&model.FileCard{MaterialID: m.ID, Status: model.AnalysisSucceeded, MetadataOnly: true,
		CardJSON: "{}", ProblemsJSON: "[]", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RerunAI(context.Background(), auth.Principal{}, RerunModeFull, RerunScopeAttention, false); err != nil {
		t.Fatal(err)
	}
	var got model.Material
	if err := e.conn.Where("id = ?", m.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.AIStep != model.StepRead {
		t.Fatalf("ai_step = %q, want %q", got.AIStep, model.StepRead)
	}
	var jobs int64
	e.conn.Model(&model.Job{}).Where("kind = ? AND material_id = ? AND status = ?", model.JobAnalyze, m.ID, model.JobPending).Count(&jobs)
	if jobs != 1 {
		t.Fatalf("pending read jobs = %d, want 1", jobs)
	}
}
