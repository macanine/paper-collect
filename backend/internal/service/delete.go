package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// trashRetention is how long trashed packs stay restorable.
const trashRetention = 30 * 24 * time.Hour

// TrashPacks moves packs and all their files to the recycle bin. Nothing is
// removed from disk until the pack is purged or the retention period ends.
func (s *Service) TrashPacks(ctx context.Context, p auth.Principal, ids []string) (int, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, apperr.Validation("一次需要处理 1 到 500 个套卷")
	}
	var n int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		n, err = s.trashPacksTx(tx, p, ids)
		return err
	})
	return int(n), err
}

func (s *Service) trashPacksTx(tx *gorm.DB, p auth.Principal, ids []string) (int64, error) {
	res := tx.Where("id IN ?", ids).Delete(&model.Pack{})
	if res.Error != nil {
		return 0, wrap("trash packs", res.Error)
	}
	if err := tx.Where("pack_id IN ?", ids).Delete(&model.Material{}).Error; err != nil {
		return 0, wrap("trash materials", err)
	}
	return res.RowsAffected, s.audit(tx, p, "pack.trash", strings.Join(ids, ","))
}

// TrashMaterial removes one file from its pack into the recycle bin. The file
// is split into a pack of its own first, so restoring brings back just it.
func (s *Service) TrashMaterial(ctx context.Context, p auth.Principal, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Material
		if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
			return apperr.NotFound("文件不存在")
		}
		var members int64
		if err := tx.Model(&model.Material{}).Where("pack_id = ?", m.PackID).Count(&members).Error; err != nil {
			return wrap("count members", err)
		}
		packID := m.PackID
		if members > 1 {
			fresh, err := s.splitIntoNewPackTx(tx, m, s.now())
			if err != nil {
				return err
			}
			if _, err := s.refreshPackNamesTx(tx, m.PackID); err != nil {
				return err
			}
			// The remaining files form a different set now; the AI decides it again.
			if err := s.membersChangedTx(tx, m.PackID, s.now()); err != nil {
				return err
			}
			if _, err := s.refreshPackNamesTx(tx, fresh); err != nil {
				return err
			}
			packID = fresh
		}
		_, err := s.trashPacksTx(tx, p, []string{packID})
		return err
	})
	return err
}

// RestorePacks brings trashed packs and their files back.
func (s *Service) RestorePacks(ctx context.Context, p auth.Principal, ids []string) (int, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, apperr.Validation("一次需要处理 1 到 500 个套卷")
	}
	var n int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Unscoped().Model(&model.Pack{}).Where("id IN ? AND deleted_at IS NOT NULL", ids).Update("deleted_at", nil)
		if res.Error != nil {
			return wrap("restore packs", res.Error)
		}
		n = res.RowsAffected
		if err := tx.Unscoped().Model(&model.Material{}).Where("pack_id IN ?", ids).Update("deleted_at", nil).Error; err != nil {
			return wrap("restore materials", err)
		}
		return s.audit(tx, p, "pack.restore", strings.Join(ids, ","))
	})
	return int(n), err
}

// PurgePacks permanently deletes trashed packs. Packs not in the bin are refused.
func (s *Service) PurgePacks(ctx context.Context, p auth.Principal, ids []string) (int, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, apperr.Validation("一次需要处理 1 到 500 个套卷")
	}
	var c purgeCleanup
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The bin check runs in the deleting transaction, so a pack restored
		// concurrently is either refused here or already gone.
		trashed, err := lockTrashedPacksTx(tx, ids, time.Time{})
		if err != nil {
			return err
		}
		if len(trashed) != len(ids) {
			return apperr.Conflict("只能彻底删除回收站中的套卷")
		}
		if c, err = s.purgePacksTx(tx, trashed); err != nil {
			return err
		}
		return s.audit(tx, p, "pack.purge", strings.Join(trashed, ","))
	})
	if err != nil {
		return 0, asAppErr(err)
	}
	s.finishPurge(c)
	return len(ids), nil
}

// PurgeExpiredTrash permanently deletes packs trashed longer than the retention period.
func (s *Service) PurgeExpiredTrash(ctx context.Context) (int, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Unscoped().Model(&model.Pack{}).
		Where("deleted_at IS NOT NULL AND deleted_at < ?", s.now().Add(-trashRetention)).
		Limit(500).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var c purgeCleanup
	var purged []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if purged, err = lockTrashedPacksTx(tx, ids, s.now().Add(-trashRetention)); err != nil || len(purged) == 0 {
			return err
		}
		if c, err = s.purgePacksTx(tx, purged); err != nil {
			return err
		}
		return s.auditSystem(tx, "pack.purge_expired", strings.Join(purged, ","))
	})
	if err != nil {
		return 0, asAppErr(err)
	}
	s.finishPurge(c)
	return len(purged), nil
}

// lockTrashedPacksTx returns those of ids that are in the bin (and, with a
// non-zero cutoff, were trashed before it), locking them on PostgreSQL.
func lockTrashedPacksTx(tx *gorm.DB, ids []string, cutoff time.Time) ([]string, error) {
	q := tx.Unscoped().Model(&model.Pack{}).Select("id").Where("id IN ? AND deleted_at IS NOT NULL", ids)
	if !cutoff.IsZero() {
		q = q.Where("deleted_at < ?", cutoff)
	}
	if tx.Dialector.Name() != "sqlite" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var trashed []string
	if err := q.Pluck("id", &trashed).Error; err != nil {
		return nil, err
	}
	return trashed, nil
}

func asAppErr(err error) error {
	if _, ok := err.(*apperr.Error); ok {
		return err
	}
	return apperr.Internal(err)
}

// purgeLocalFile is a local-drive copy to remove once the purge commits.
type purgeLocalFile struct {
	driveID   string
	remoteKey string
}

// purgeCleanup is the disk work of a committed purge. Files are only removed
// after the database no longer references them.
type purgeCleanup struct {
	materialIDs []string
	localFiles  []purgeLocalFile
	blobSHAs    []string
	runningJobs []string
}

func (c *purgeCleanup) merge(o purgeCleanup) {
	c.materialIDs = append(c.materialIDs, o.materialIDs...)
	c.localFiles = append(c.localFiles, o.localFiles...)
	c.blobSHAs = append(c.blobSHAs, o.blobSHAs...)
	c.runningJobs = append(c.runningJobs, o.runningJobs...)
}

// purgePacksTx removes packs, their files and all pack-scoped rows. Work
// still running for them is dropped and stopped once the purge commits.
func (s *Service) purgePacksTx(tx *gorm.DB, packIDs []string) (purgeCleanup, error) {
	var c purgeCleanup
	var materialIDs []string
	if err := tx.Unscoped().Model(&model.Material{}).Where("pack_id IN ?", packIDs).Pluck("id", &materialIDs).Error; err != nil {
		return c, err
	}
	mc, err := s.purgeMaterialsTx(tx, materialIDs)
	if err != nil {
		return c, err
	}
	c.merge(mc)
	if err := tx.Where("pack_id IN ?", packIDs).Delete(&model.PackSubject{}).Error; err != nil {
		return c, err
	}
	if err := tx.Where("pack_id IN ?", packIDs).Delete(&model.CollectionItem{}).Error; err != nil {
		return c, err
	}
	if err := tx.Where("pack_id IN ?", packIDs).Delete(&model.PackDecision{}).Error; err != nil {
		return c, err
	}
	var running []string
	if err := tx.Model(&model.Job{}).Where("kind = ? AND pack_id IN ? AND status = ?", model.JobDecide, packIDs, model.JobRunning).
		Pluck("id", &running).Error; err != nil {
		return c, err
	}
	c.runningJobs = append(c.runningJobs, running...)
	activeDecide := tx.Model(&model.Job{}).Select("id").
		Where("kind = ? AND pack_id IN ? AND status IN ?", model.JobDecide, packIDs, []string{model.JobPending, model.JobRunning})
	if err := tx.Where("job_id IN (?)", activeDecide).Delete(&model.JobLock{}).Error; err != nil {
		return c, err
	}
	if err := tx.Where("kind = ? AND pack_id IN ? AND status IN ?", model.JobDecide, packIDs, []string{model.JobPending, model.JobRunning}).
		Delete(&model.Job{}).Error; err != nil {
		return c, err
	}
	return c, tx.Unscoped().Where("id IN ?", packIDs).Delete(&model.Pack{}).Error
}

// purgeMaterials permanently removes files outside any pack purge.
func (s *Service) purgeMaterials(ctx context.Context, ids []string) error {
	var c purgeCleanup
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		c, err = s.purgeMaterialsTx(tx, ids)
		return err
	})
	if err != nil {
		return apperr.Internal(err)
	}
	s.finishPurge(c)
	return nil
}

// purgeMaterialsTx deletes file rows. Their local CAS originals and local
// replicas are removed by finishPurge; remote cloud objects are NOT deleted
// (removing cloud files automatically is a non-goal), so those replicas stay
// as orphans that a re-upload of the same content adopts again.
func (s *Service) purgeMaterialsTx(tx *gorm.DB, ids []string) (purgeCleanup, error) {
	c := purgeCleanup{materialIDs: ids}
	if len(ids) == 0 {
		return c, nil
	}
	var toDelete []model.Material
	if err := tx.Unscoped().Where("id IN ?", ids).Find(&toDelete).Error; err != nil {
		return c, err
	}
	blobIDs := map[string]bool{}
	for _, m := range toDelete {
		blobIDs[m.BlobID] = true
	}
	var running []string
	if err := tx.Model(&model.Job{}).Where("kind IN ? AND material_id IN ? AND status = ?",
		[]string{model.JobAnalyze, model.JobIndex}, ids, model.JobRunning).Pluck("id", &running).Error; err != nil {
		return c, err
	}
	c.runningJobs = running
	if err := tx.Where("material_id IN ?", ids).Delete(&model.MaterialSource{}).Error; err != nil {
		return c, err
	}
	if err := tx.Where("material_id IN ?", ids).Delete(&model.FileCard{}).Error; err != nil {
		return c, err
	}
	// Remove file-scoped work together with its lease locks.
	materialJobs := tx.Model(&model.Job{}).
		Select("id").
		Where("kind IN ? AND material_id IN ?", []string{model.JobAnalyze, model.JobIndex}, ids)
	if err := tx.Where("job_id IN (?)", materialJobs).Delete(&model.JobLock{}).Error; err != nil {
		return c, err
	}
	if err := tx.Where("kind IN ? AND material_id IN ?", []string{model.JobAnalyze, model.JobIndex}, ids).
		Delete(&model.Job{}).Error; err != nil {
		return c, err
	}
	if err := deleteSearchDataForMaterials(tx, toDelete); err != nil {
		return c, err
	}
	if err := tx.Unscoped().Where("id IN ?", ids).Delete(&model.Material{}).Error; err != nil {
		return c, err
	}
	for blobID := range blobIDs {
		var remaining int64
		if err := tx.Unscoped().Model(&model.Material{}).Where("blob_id = ?", blobID).Count(&remaining).Error; err != nil {
			return c, err
		}
		if remaining > 0 {
			continue
		}
		var blob model.Blob
		if err := tx.Where("id = ?", blobID).First(&blob).Error; err != nil {
			continue
		}
		c.blobSHAs = append(c.blobSHAs, blob.SHA256)
		var replicas []model.Replica
		if err := tx.Where("blob_id = ?", blobID).Find(&replicas).Error; err != nil {
			return c, err
		}
		// Cloud copies are never deleted implicitly: they stay as orphaned
		// replicas (and keep their blob row, so re-uploading the same file
		// adopts them again) until a person deletes them from the drive.
		// Local drive files and never-uploaded rows are removed outright.
		replicaIDs := make([]string, 0, len(replicas))
		dropIDs := []string{}
		kept := 0
		for _, replica := range replicas {
			replicaIDs = append(replicaIDs, replica.ID)
			var provider model.Drive
			err := tx.Where("id = ?", replica.DriveID).First(&provider).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return c, err
			}
			if err == nil && provider.Kind == model.KindLocal {
				c.localFiles = append(c.localFiles, purgeLocalFile{driveID: provider.ID, remoteKey: replica.RemotePath})
			}
			if err != nil || provider.Kind == model.KindLocal || replica.RemoteID == "" {
				dropIDs = append(dropIDs, replica.ID)
				continue
			}
			kept++
			if err := tx.Model(&model.Replica{}).Where("id = ?", replica.ID).Updates(map[string]any{
				"status": model.ReplicaOrphaned, "rule_id": "", "updated_at": s.now(),
			}).Error; err != nil {
				return c, err
			}
		}
		if len(replicaIDs) > 0 {
			replicaJobs := tx.Model(&model.Job{}).
				Select("id").
				Where("kind = ? AND replica_id IN ?", model.JobReplicate, replicaIDs)
			if err := tx.Where("job_id IN (?)", replicaJobs).Delete(&model.JobLock{}).Error; err != nil {
				return c, err
			}
			if err := tx.Where("kind = ? AND replica_id IN ?", model.JobReplicate, replicaIDs).
				Delete(&model.Job{}).Error; err != nil {
				return c, err
			}
		}
		if len(dropIDs) > 0 {
			if err := tx.Where("id IN ?", dropIDs).Delete(&model.Replica{}).Error; err != nil {
				return c, err
			}
		}
		if kept > 0 {
			continue
		}
		if err := tx.Where("id = ?", blobID).Delete(&model.Blob{}).Error; err != nil {
			return c, err
		}
	}
	return c, nil
}

// finishPurge stops dropped executions and removes files the committed purge
// no longer references.
func (s *Service) finishPurge(c purgeCleanup) {
	for _, id := range c.runningJobs {
		s.stopLocalExecution(id)
	}
	for _, id := range c.materialIDs {
		_ = os.RemoveAll(filepath.Join(s.blobs.DataDir(), "analysis", id))
	}
	for _, f := range c.localFiles {
		root := filepath.Join(s.blobs.DataDir(), "drives", f.driveID)
		target := filepath.Join(root, filepath.FromSlash(f.remoteKey))
		if strings.HasPrefix(filepath.Clean(target), filepath.Clean(root)+string(filepath.Separator)) {
			_ = os.Remove(target)
		}
	}
	for _, sha := range c.blobSHAs {
		_ = os.Remove(s.blobs.Path(sha))
		s.office.Remove(sha)
	}
	if len(c.runningJobs) > 0 {
		s.notify()
	}
}
