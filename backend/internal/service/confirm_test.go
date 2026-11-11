package service

import (
	"testing"

	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/model"
)

func (e *syncEnv) attentionPack() (model.Material, model.Material) {
	e.t.Helper()
	q := e.material("卷", "卷.pdf", []byte("confirm-q"), nil)
	a := e.material("卷答案", "卷答案.pdf", []byte("confirm-a"), nil)
	for _, m := range []model.Material{q, a} {
		if err := e.conn.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"pack_id": q.PackID, "stage": model.StageAttention, "attention_json": `["grade_missing"]`,
		}).Error; err != nil {
			e.t.Fatal(err)
		}
	}
	return q, a
}

func TestConfirmPackAppliesEditsAndAcceptsTogether(t *testing.T) {
	e := newSyncEnv(t)
	q, a := e.attentionPack()
	grade := "grade10"
	view, n, err := e.svc.ConfirmPack(e.ctx, syncAdmin, q.PackID, PackPatch{Grade: &grade},
		[]MemberEdit{{ID: a.ID, Part: model.PartAnswers}})
	if err != nil || n != 2 {
		t.Fatalf("confirm = %d, %v", n, err)
	}
	if view.Pack.Grade != grade || !view.Pack.Locked()[catalogai.FieldGrade] || len(view.Pack.Locked()) != 1 {
		t.Fatalf("pack after confirm: grade=%s locked=%v", view.Pack.Grade, view.Pack.Locked())
	}
	got := e.reload(a)
	if got.Part != model.PartAnswers || !got.PartLocked || got.Stage != model.StageReady {
		t.Fatalf("member after confirm = part %s locked %v stage %s", got.Part, got.PartLocked, got.Stage)
	}
	if e.reload(q).Stage != model.StageReady {
		t.Fatal("untouched member not accepted")
	}
}

func TestConfirmPackSavesNothingWhenAnEditFails(t *testing.T) {
	e := newSyncEnv(t)
	q, a := e.attentionPack()
	other := e.material("别的", "别的.pdf", []byte("other"), nil)
	grade := "grade10"
	_, _, err := e.svc.ConfirmPack(e.ctx, syncAdmin, q.PackID, PackPatch{Grade: &grade},
		[]MemberEdit{{ID: a.ID, Part: model.PartAnswers}, {ID: other.ID, Part: model.PartAnswers}})
	if err == nil {
		t.Fatal("editing a file of another set must fail")
	}
	if p := e.pack(q.PackID); p.Grade == grade || len(p.Locked()) != 0 {
		t.Fatal("catalogue edit kept after a failed confirm")
	}
	if got := e.reload(a); got.Stage != model.StageAttention || got.PartLocked {
		t.Fatal("member changed after a failed confirm")
	}
}

func TestConfirmPackWithoutAttentionIsConflict(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("ready"), nil)
	if _, _, err := e.svc.ConfirmPack(e.ctx, syncAdmin, m.PackID, PackPatch{}, nil); err == nil {
		t.Fatal("confirming a settled set must be refused")
	}
}
