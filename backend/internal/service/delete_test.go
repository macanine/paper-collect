package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// purgeMaterials permanently removes files outside any pack purge.
func (s *Service) purgeMaterials(ctx context.Context, ids []string) error {
	var c purgeCleanup
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		c, err = s.purgeMaterialsTx(tx, ids)
		return err
	}); err != nil {
		return err
	}
	s.finishPurge(c)
	return nil
}

func (e *syncEnv) exists(row any, id string) bool {
	e.t.Helper()
	var n int64
	if err := e.conn.Unscoped().Model(row).Where("id = ?", id).Count(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n > 0
}

func (e *syncEnv) trash(packIDs ...string) {
	e.t.Helper()
	if _, err := e.svc.TrashPacks(e.ctx, syncAdmin, packIDs); err != nil {
		e.t.Fatal(err)
	}
}

func TestPurgeRefusesPacksOutsideTheBin(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("purge-restored"), nil)
	e.trash(m.PackID)
	if _, err := e.svc.RestorePacks(e.ctx, syncAdmin, []string{m.PackID}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.PurgePacks(e.ctx, syncAdmin, []string{m.PackID})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.Conflict("").Code {
		t.Fatalf("purge of a restored pack = %v, want conflict", err)
	}
	if !e.exists(&model.Material{}, m.ID) || !e.exists(&model.Pack{}, m.PackID) {
		t.Fatal("a refused purge must not delete anything")
	}
	if _, err := os.Stat(e.svc.blobs.Path(m.SHA256)); err != nil {
		t.Fatalf("original of a restored pack removed: %v", err)
	}
}

func TestPurgeRemovesPackFilesAndActiveDecision(t *testing.T) {
	e := newSyncEnv(t)
	m := e.material("卷", "卷.pdf", []byte("purge-me"), nil)
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	decide := model.Job{ID: newID(), Kind: model.JobDecide, Status: model.JobRunning, PackID: m.PackID,
		LeaseOwner: "node", LeaseUntil: &lease, CreatedAt: now}
	done := model.Job{ID: newID(), Kind: model.JobDecide, Status: model.JobSucceeded, PackID: m.PackID, CreatedAt: now}
	lock := model.JobLock{Key: "pack:" + m.PackID, JobID: decide.ID, LeaseUntil: lease, UpdatedAt: now}
	for _, row := range []any{&decide, &done, &lock} {
		if err := e.conn.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	e.trash(m.PackID)
	n, err := e.svc.PurgePacks(e.ctx, syncAdmin, []string{m.PackID})
	if err != nil || n != 1 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	if e.exists(&model.Pack{}, m.PackID) || e.exists(&model.Material{}, m.ID) || e.exists(&model.Blob{}, m.BlobID) {
		t.Fatal("pack, file and unreferenced blob must be gone")
	}
	if _, err := os.Stat(e.svc.blobs.Path(m.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("original still on disk: %v", err)
	}
	if e.exists(&model.Job{}, decide.ID) {
		t.Fatal("a running decision for a purged pack must be dropped")
	}
	if !e.exists(&model.Job{}, done.ID) {
		t.Fatal("finished job history stays for retention to remove")
	}
	var locks int64
	e.conn.Model(&model.JobLock{}).Where("job_id = ?", decide.ID).Count(&locks)
	if locks != 0 {
		t.Fatal("lease lock of the dropped decision must be released")
	}
}

func TestPurgeExpiredTrashOnlyTakesExpiredAndAudits(t *testing.T) {
	e := newSyncEnv(t)
	old := e.material("旧", "旧.pdf", []byte("old"), nil)
	recent := e.material("新", "新.pdf", []byte("recent"), nil)
	live := e.material("在用", "在用.pdf", []byte("live"), nil)
	e.trash(old.PackID, recent.PackID)
	if err := e.conn.Unscoped().Model(&model.Pack{}).Where("id = ?", old.PackID).
		Update("deleted_at", time.Now().UTC().Add(-trashRetention-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	n, err := e.svc.PurgeExpiredTrash(e.ctx)
	if err != nil || n != 1 {
		t.Fatalf("purge expired = %d, %v", n, err)
	}
	if e.exists(&model.Pack{}, old.PackID) {
		t.Fatal("expired pack not purged")
	}
	if !e.exists(&model.Pack{}, recent.PackID) || !e.exists(&model.Pack{}, live.PackID) {
		t.Fatal("recently trashed or live pack purged")
	}
	var audits []model.Audit
	e.conn.Where("action = ?", "pack.purge_expired").Find(&audits)
	if len(audits) != 1 || audits[0].TargetID != old.PackID || audits[0].UserID != "" {
		t.Fatalf("expiry audit = %+v", audits)
	}
}

func TestTrashMaterialSplitsAndRestoresOnlyThatFile(t *testing.T) {
	e := newSyncEnv(t)
	a := e.material("卷", "卷.pdf", []byte("questions"), nil)
	b := e.material("卷答案", "卷答案.pdf", []byte("answers"), nil)
	if err := e.conn.Model(&model.Material{}).Where("id = ?", b.ID).Update("pack_id", a.PackID).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.svc.TrashMaterial(e.ctx, syncAdmin, b.ID); err != nil {
		t.Fatal(err)
	}
	var trashed model.Material
	if err := e.conn.Unscoped().Where("id = ?", b.ID).First(&trashed).Error; err != nil {
		t.Fatal(err)
	}
	if trashed.PackID == a.PackID || !trashed.DeletedAt.Valid {
		t.Fatalf("trashed file = pack %s deleted %v; want its own trashed pack", trashed.PackID, trashed.DeletedAt.Valid)
	}
	if a2 := e.reload(a); a2.PackID != a.PackID {
		t.Fatal("remaining file moved")
	}
	if _, err := e.svc.RestorePacks(e.ctx, syncAdmin, []string{trashed.PackID}); err != nil {
		t.Fatal(err)
	}
	if b2 := e.reload(b); b2.PackID != trashed.PackID {
		t.Fatalf("restored file in pack %s, want %s", b2.PackID, trashed.PackID)
	}
}
