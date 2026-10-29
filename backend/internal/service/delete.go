package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// DeleteMaterial removes a material (and, with cascade, its ZIP children) from
// the catalogue. Blobs with no remaining materials lose their local CAS file and
// local replicas. Remote cloud objects are NOT deleted: adapters have no delete
// capability, so their replica rows are removed and the files remain remote.
func (s *Service) DeleteMaterial(ctx context.Context, p auth.Principal, id string, cascade bool) error {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", id).First(&material).Error; err != nil {
		return apperr.NotFound("资料不存在")
	}
	var childCount int64
	if err := db.Model(&model.Material{}).Where("parent_material_id = ?", id).Count(&childCount).Error; err != nil {
		return apperr.Internal(err)
	}
	if childCount > 0 && !cascade {
		return apperr.Conflict("该资料包含 ZIP 展开的子资料，请确认级联删除")
	}

	ids := []string{id}
	if cascade {
		frontier := []string{id}
		for len(frontier) > 0 {
			var next []string
			if err := db.Model(&model.Material{}).Where("parent_material_id IN ?", frontier).Pluck("id", &next).Error; err != nil {
				return apperr.Internal(err)
			}
			ids = append(ids, next...)
			frontier = next
		}
	}

	var toDelete []model.Material
	if err := db.Where("id IN ?", ids).Find(&toDelete).Error; err != nil {
		return apperr.Internal(err)
	}
	blobIDs := map[string]bool{}
	for _, m := range toDelete {
		blobIDs[m.BlobID] = true
	}

	type localFile struct {
		providerKind string
		providerID   string
		remoteKey    string
	}
	var staleReplicas []localFile
	var blobSHAs []string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Upload{}).Where("material_id IN ?", ids).
			Update("material_id", "").Error; err != nil {
			return err
		}
		if err := tx.Where("material_id IN ?", ids).Delete(&model.MaterialSubject{}).Error; err != nil {
			return err
		}
		if err := tx.Where("material_id IN ?", ids).Delete(&model.CollectionItem{}).Error; err != nil {
			return err
		}
		if err := tx.Where("material_id IN ?", ids).Delete(&model.MaterialAnalysis{}).Error; err != nil {
			return err
		}
		if err := tx.Where("kind = ? AND material_id IN ?", model.JobAnalyze, ids).Delete(&model.Job{}).Error; err != nil {
			return err
		}
		if err := deleteSearchDataForMaterials(tx, toDelete); err != nil {
			return err
		}
		if err := tx.Where("id IN ?", ids).Delete(&model.Material{}).Error; err != nil {
			return err
		}
		for blobID := range blobIDs {
			var remaining int64
			if err := tx.Model(&model.Material{}).Where("blob_id = ?", blobID).Count(&remaining).Error; err != nil {
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
			for _, replica := range replicas {
				var provider model.StorageProvider
				if err := tx.Where("id = ?", replica.ProviderID).First(&provider).Error; err == nil && provider.Kind == model.KindLocal {
					staleReplicas = append(staleReplicas, localFile{
						providerKind: provider.Kind, providerID: provider.ID, remoteKey: replica.RemoteKey,
					})
				}
			}
			if err := tx.Where("blob_id = ?", blobID).Delete(&model.Replica{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", blobID).Delete(&model.Blob{}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, p, "material.delete", id)
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

// DeleteCollection removes collections and their items; materials are kept.
// Children (such as a ZIP child collection) require cascade=true.
func (s *Service) DeleteCollection(ctx context.Context, p auth.Principal, id string, cascade bool) error {
	db := s.db.WithContext(ctx)
	var collection model.Collection
	if err := db.Where("id = ?", id).First(&collection).Error; err != nil {
		return apperr.NotFound("合集不存在")
	}
	var childCount int64
	if err := db.Model(&model.Collection{}).Where("parent_id = ?", id).Count(&childCount).Error; err != nil {
		return apperr.Internal(err)
	}
	if childCount > 0 && !cascade {
		return apperr.Conflict("该合集包含子合集，请确认级联删除")
	}
	ids := []string{id}
	if cascade {
		frontier := []string{id}
		for len(frontier) > 0 {
			var next []string
			if err := db.Model(&model.Collection{}).Where("parent_id IN ?", frontier).Pluck("id", &next).Error; err != nil {
				return apperr.Internal(err)
			}
			ids = append(ids, next...)
			frontier = next
		}
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("collection_id IN ?", ids).Delete(&model.CollectionItem{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Upload{}).Where("collection_id IN ?", ids).
			Updates(map[string]any{"collection_id": ""}).Error; err != nil {
			return err
		}
		if err := tx.Where("id IN ?", ids).Delete(&model.Collection{}).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "collection.delete", id)
	})
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}
