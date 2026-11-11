package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
)

// runningDecide creates a leased decide job made for the pack's revision.
func (e *syncEnv) runningDecide(packID string, revision int) model.Job {
	e.t.Helper()
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	payload, _ := json.Marshal(decidePayload{Revision: revision})
	job := model.Job{ID: newID(), Kind: model.JobDecide, Status: model.JobRunning, PackID: packID, PayloadJSON: string(payload),
		LeaseOwner: "test-node", LeaseUntil: &lease, StartedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := e.conn.Create(&job).Error; err != nil {
		e.t.Fatal(err)
	}
	return job
}

func (e *syncEnv) pack(id string) model.Pack {
	e.t.Helper()
	var p model.Pack
	if err := e.conn.Unscoped().Where("id = ?", id).First(&p).Error; err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *syncEnv) job(id string) model.Job {
	e.t.Helper()
	var j model.Job
	if err := e.conn.Where("id = ?", id).First(&j).Error; err != nil {
		e.t.Fatal(err)
	}
	return j
}

func TestCommitDecideKeepsHumanFields(t *testing.T) {
	e := newSyncEnv(t)
	q := e.material("卷", "卷.pdf", []byte("q"), func(p *model.Pack) {
		p.Grade = "grade11"
		p.LockedJSON = model.EncodeStrings([]string{catalogai.FieldGrade})
	})
	a := e.material("卷答案", "卷答案.pdf", []byte("a"), nil)
	if err := e.conn.Model(&model.Material{}).Where("id = ?", a.ID).
		Updates(map[string]any{"pack_id": q.PackID, "part": model.PartTeacher, "part_locked": true}).Error; err != nil {
		t.Fatal(err)
	}
	job := e.runningDecide(q.PackID, e.pack(q.PackID).Revision)
	out := &jobexec.DecideResult{Usable: true, Model: "m", Decision: catalogai.Decision{
		DocType: "final", Subjects: []string{"physics"}, Grade: "grade12", Cohort: 2027,
		Files: []catalogai.FileDecision{{ID: q.ID, Role: model.PartQuestions}, {ID: a.ID, Role: model.PartAnswers}},
	}}
	if err := e.svc.commitDecide(e.ctx, job, out); err != nil {
		t.Fatal(err)
	}
	p := e.pack(q.PackID)
	if p.Grade != "grade11" {
		t.Fatalf("locked grade overwritten: %s", p.Grade)
	}
	if p.DocType != "final" || p.Cohort != 2027 || model.DecodeStrings(p.SubjectsJSON)[0] != "physics" {
		t.Fatalf("unlocked fields not applied: %+v", p)
	}
	if got := e.reload(a); got.Part != model.PartTeacher {
		t.Fatalf("locked part changed to %s", got.Part)
	}
	if j := e.job(job.ID); j.Status != model.JobSucceeded {
		t.Fatalf("job = %s %s", j.Status, j.Error)
	}
	var d model.PackDecision
	if err := e.conn.Where("pack_id = ?", q.PackID).First(&d).Error; err != nil || d.JobID != job.ID {
		t.Fatalf("decision record = %+v %v", d, err)
	}
}

func TestCommitDecideForStaleMembersRequeues(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("stale"), nil)
	before := e.pack(m.PackID)
	job := e.runningDecide(m.PackID, before.Revision-1)
	out := &jobexec.DecideResult{Usable: true, Decision: catalogai.Decision{DocType: "final",
		Files: []catalogai.FileDecision{{ID: m.ID, Role: model.PartQuestions}}}}
	if err := e.svc.commitDecide(e.ctx, job, out); err != nil {
		t.Fatal(err)
	}
	if after := e.pack(m.PackID); after.DocType != before.DocType {
		t.Fatal("a decision for an older member list must not be applied")
	}
	var pending int64
	e.conn.Model(&model.Job{}).Where("kind = ? AND pack_id = ? AND status = ?", model.JobDecide, m.PackID, model.JobPending).Count(&pending)
	if pending != 1 {
		t.Fatalf("pending decisions = %d, want 1", pending)
	}
}

func TestCommitDecideAfterLeaseLossIsRefused(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("lease"), nil)
	job := e.runningDecide(m.PackID, e.pack(m.PackID).Revision)
	job.LeaseOwner = "someone-else"
	out := &jobexec.DecideResult{Usable: true, Decision: catalogai.Decision{DocType: "final",
		Files: []catalogai.FileDecision{{ID: m.ID, Role: model.PartQuestions}}}}
	if err := e.svc.commitDecide(e.ctx, job, out); err == nil {
		t.Fatal("commit without the lease must fail")
	}
	if e.pack(m.PackID).DocType == "final" {
		t.Fatal("a fenced-out result was applied")
	}
}

func TestCommitDecideForPurgedPackSucceedsQuietly(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("gone"), nil)
	job := e.runningDecide(m.PackID, e.pack(m.PackID).Revision)
	if err := e.conn.Unscoped().Where("id = ?", m.PackID).Delete(&model.Pack{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.commitDecide(e.ctx, job, &jobexec.DecideResult{Usable: true}); err != nil {
		t.Fatal(err)
	}
	if j := e.job(job.ID); j.Status != model.JobSucceeded {
		t.Fatalf("job = %s", j.Status)
	}
}
