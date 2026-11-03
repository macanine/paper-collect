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
		res := tx.Where("id IN ?", ids).Delete(&model.Pack{})
		if res.Error != nil {
			return wrap("trash packs", res.Error)
		}
		n = res.RowsAffected
		if err := tx.Where("pack_id IN ?", ids).Delete(&model.Material{}).Error; err != nil {
			return wrap("trash materials", err)
		}
		if err := tx.Model(&model.Material{}).Where("suggested_pack_id IN ?", ids).Update("suggested_pack_id", "").Error; err != nil {
			return wrap("clear suggestions", err)
		}
		return s.audit(tx, p, "pack.trash", strings.Join(ids, ","))
	})
	return int(n), err
}

// TrashMaterial removes one file from its pack into the recycle bin. The file
// is split into a pack of its own first, so restoring brings back just it.
func (s *Service) TrashMaterial(ctx context.Context, p auth.Principal, id string) error {
	var packID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Material
		if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
			return apperr.NotFound("文件不存在")
		}
		var members int64
		if err := tx.Model(&model.Material{}).Where("pack_id = ?", m.PackID).Count(&members).Error; err != nil {
			return wrap("count members", err)
		}
		packID = m.PackID
		if members > 1 {
			fresh, err := s.splitIntoNewPackTx(tx, m, s.now())
			if err != nil {
				return err
			}
			if _, err := s.refreshPackNamesTx(tx, m.PackID); err != nil {
				return err
			}
			if _, err := s.refreshPackNamesTx(tx, fresh); err != nil {
				return err
			}
			packID = fresh
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = s.TrashPacks(ctx, p, []string{packID})
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
	db := s.db.WithContext(ctx)
	var trashed []string
	if err := db.Unscoped().Model(&model.Pack{}).Where("id IN ? AND deleted_at IS NOT NULL", ids).Pluck("id", &trashed).Error; err != nil {
		return 0, apperr.Internal(err)
	}
	if len(trashed) != len(ids) {
		return 0, apperr.Conflict("只能彻底删除回收站中的套卷")
	}
	if err := s.purgePacks(ctx, trashed); err != nil {
		return 0, err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return s.audit(tx, p, "pack.purge", strings.Join(ids, ","))
	}); err != nil {
		return 0, err
	}
	return len(trashed), nil
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
	return len(ids), s.purgePacks(ctx, ids)
}

func (s *Service) purgePacks(ctx context.Context, packIDs []string) error {
	db := s.db.WithContext(ctx)
	var materialIDs []string
	if err := db.Unscoped().Model(&model.Material{}).Where("pack_id IN ?", packIDs).Pluck("id", &materialIDs).Error; err != nil {
		return apperr.Internal(err)
	}
	if err := s.purgeMaterials(ctx, materialIDs); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pack_id IN ?", packIDs).Delete(&model.PackSubject{}).Error; err != nil {
			return err
		}
		if err := tx.Where("pack_id IN ?", packIDs).Delete(&model.CollectionItem{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("id IN ?", packIDs).Delete(&model.Pack{}).Error
	})
}

// purgeMaterials permanently removes files. Their local CAS originals and
// local replicas are deleted; remote cloud objects are NOT deleted because
// adapters have no delete capability, so only their replica rows go.
func (s *Service) purgeMaterials(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	db := s.db.WithContext(ctx)
	var toDelete []model.Material
	if err := db.Unscoped().Where("id IN ?", ids).Find(&toDelete).Error; err != nil {
		return apperr.Internal(err)
	}
	blobIDs := map[string]bool{}
	for _, m := range toDelete {
		blobIDs[m.BlobID] = true
	}

	type localFile struct {
		providerID string
		remoteKey  string
	}
	var staleReplicas []localFile
	var blobSHAs []string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("material_id IN ?", ids).Delete(&model.MaterialSource{}).Error; err != nil {
			return err
		}
		if err := tx.Where("material_id IN ?", ids).Delete(&model.MaterialAnalysis{}).Error; err != nil {
			return err
		}
		// Remove file-scoped work together with its lease locks.
		materialJobs := tx.Model(&model.Job{}).
			Select("id").
			Where("kind IN ? AND material_id IN ?", []string{model.JobAnalyze, model.JobIndex, model.JobGroup}, ids)
		if err := tx.Where("job_id IN (?)", materialJobs).Delete(&model.JobLock{}).Error; err != nil {
			return err
		}
		if err := tx.Where("kind IN ? AND material_id IN ?", []string{model.JobAnalyze, model.JobIndex, model.JobGroup}, ids).
			Delete(&model.Job{}).Error; err != nil {
			return err
		}
		if err := deleteSearchDataForMaterials(tx, toDelete); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("id IN ?", ids).Delete(&model.Material{}).Error; err != nil {
			return err
		}
		for blobID := range blobIDs {
			var remaining int64
			if err := tx.Unscoped().Model(&model.Material{}).Where("blob_id = ?", blobID).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining > 0 {
				continue
			}
			var blob model.Blob
			if err := tx.Where("id = ?", blobID).First(&blob).Error; err != nil {
				continue
			}
			blobSHAs = append(blobSHAs, blob.SHA256)
			var replicas []model.Replica
			if err := tx.Where("blob_id = ?", blobID).Find(&replicas).Error; err != nil {
				return err
			}
			replicaIDs := make([]string, 0, len(replicas))
			for _, replica := range replicas {
				replicaIDs = append(replicaIDs, replica.ID)
				var provider model.StorageProvider
				err := tx.Where("id = ?", replica.ProviderID).First(&provider).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil && provider.Kind == model.KindLocal {
					staleReplicas = append(staleReplicas, localFile{providerID: provider.ID, remoteKey: replica.RemoteKey})
				}
			}
			if len(replicaIDs) > 0 {
				replicaJobs := tx.Model(&model.Job{}).
					Select("id").
					Where("kind = ? AND replica_id IN ?", model.JobReplicate, replicaIDs)
				if err := tx.Where("job_id IN (?)", replicaJobs).Delete(&model.JobLock{}).Error; err != nil {
					return err
				}
				if err := tx.Where("kind = ? AND replica_id IN ?", model.JobReplicate, replicaIDs).
					Delete(&model.Job{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("blob_id = ?", blobID).Delete(&model.Replica{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", blobID).Delete(&model.Blob{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return apperr.Internal(err)
	}

	for _, id := range ids {
		_ = os.RemoveAll(filepath.Join(s.blobs.DataDir(), "analysis", id))
	}
	for _, replica := range staleReplicas {
		root := filepath.Join(s.blobs.DataDir(), "providers", replica.providerID)
		target := filepath.Join(root, filepath.FromSlash(replica.remoteKey))
		if strings.HasPrefix(filepath.Clean(target), filepath.Clean(root)+string(filepath.Separator)) {
			_ = os.Remove(target)
		}
	}
	for _, sha := range blobSHAs {
		_ = os.Remove(s.blobs.Path(sha))
		s.office.Remove(sha)
	}
	return nil
}
