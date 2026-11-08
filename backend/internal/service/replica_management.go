package service

import (
	"context"
	"errors"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

func (s *Service) ListReplicas(ctx context.Context, status, driveID string, pg PageQuery) (Page[model.Replica], error) {
	out := Page[model.Replica]{Items: []model.Replica{}, Page: pg.Page, PageSize: pg.PageSize}
	q := s.db.WithContext(ctx).Model(&model.Replica{})
	if status == "drift" {
		q = q.Where("remote_id <> '' AND desired_path <> remote_path AND status <> ?", model.ReplicaOrphaned)
	} else if status != "" {
		if !isOneOf(status, model.ReplicaPending, model.ReplicaUploading, model.ReplicaMoving, model.ReplicaReady, model.ReplicaFailed, model.ReplicaSkipped, model.ReplicaOrphaned) {
			return out, apperr.Validation("副本状态无效")
		}
		q = q.Where("status = ?", status)
	}
	if driveID != "" {
		q = q.Where("drive_id = ?", driveID)
	}
	if err := q.Count(&out.Total).Error; err != nil {
		return out, err
	}
	err := q.Order("updated_at DESC").Offset((pg.Page - 1) * pg.PageSize).Limit(pg.PageSize).Find(&out.Items).Error
	return out, err
}

type ReplicaBulkResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func (s *Service) BulkReplicas(ctx context.Context, p auth.Principal, ids []string, action string) ([]ReplicaBulkResult, error) {
	if len(ids) == 0 || len(ids) > 100 || !isOneOf(action, "retry", "delete_remote", "forget") {
		return nil, apperr.Validation("需要 1 到 100 个副本，操作为 retry、delete_remote 或 forget")
	}
	out := []ReplicaBulkResult{}
	for _, id := range ids {
		err := s.manageReplica(ctx, p, id, action)
		r := ReplicaBulkResult{ID: id, Success: err == nil}
		if err != nil {
			r.Error = storageErrorText(err)
		}
		out = append(out, r)
	}
	s.notify()
	return out, nil
}
func (s *Service) manageReplica(ctx context.Context, p auth.Principal, id, action string) error {
	db := s.db.WithContext(ctx)
	var r model.Replica
	var d model.Drive
	// A persisted running replicate job acts as an exclusive operation lease.
	// Network deletion is outside the database transaction.
	var deletionJob model.Job
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := tx.Where("id = ?", id).First(&r).Error; err != nil {
			return apperr.NotFound("副本不存在")
		}
		var active int64
		if err := tx.Model(&model.Job{}).Where("replica_id = ? AND status IN ?", id, []string{model.JobPending, model.JobRunning}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return apperr.Conflict("副本仍有任务运行，请稍后重试")
		}
		if action == "retry" {
			var m model.Material
			if err := tx.Where("blob_id = ?", r.BlobID).First(&m).Error; err != nil {
				return err
			}
			_, err := s.planMaterialTx(tx, m.ID, false)
			return err
		}
		if r.Status != model.ReplicaOrphaned {
			return apperr.Conflict("仅规则外副本可删除或移除记录")
		}
		if action == "forget" {
			if err := tx.Delete(&r).Error; err != nil {
				return err
			}
			return s.audit(tx, p, "storage.replica_forget", id)
		}
		if err := tx.Where("id = ?", r.DriveID).First(&d).Error; err != nil {
			return err
		}
		now := s.now()
		settings := s.workerSettings()
		until := now.Add(settings.LeaseDuration)
		deletionJob = model.Job{ID: newID(), Kind: model.JobReplicate, Status: model.JobRunning, ReplicaID: r.ID, LeaseOwner: settings.InstanceID, LeaseUntil: &until, HeartbeatAt: &now, StartedAt: &now, CreatedAt: now, UpdatedAt: now}
		return tx.Create(&deletionJob).Error
	})
	if err != nil || action != "delete_remote" {
		return err
	}
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stopHeartbeat := s.heartbeat(runCtx, cancel, deletionJob)
	defer stopHeartbeat()
	adapter, err := s.adapterFor(d)
	if err == nil && r.RemoteID != "" {
		deleter, ok := adapter.(storage.Deleter)
		if !ok {
			err = storage.ErrUnsupported
		} else {
			err = deleter.Delete(runCtx, r.RemoteID)
		}
	}
	operationErr := err
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := s.lockJobLease(tx, deletionJob.ID, deletionJob.LeaseOwner); err != nil {
			return err
		}
		state := model.JobSucceeded
		note := ""
		if operationErr != nil {
			state = model.JobFailed
			note = storageErrorText(operationErr)
		}
		if err := s.finishJobTx(tx, deletionJob.ID, deletionJob.LeaseOwner, state, note); err != nil {
			return err
		}
		if operationErr != nil {
			return nil
		}
		// A rule may have re-adopted this copy while the remote call was running.
		var current model.Replica
		if err := tx.Where("id = ?", id).First(&current).Error; err != nil {
			return err
		}
		if current.Status == model.ReplicaOrphaned {
			if err := tx.Delete(&current).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&current).Updates(map[string]any{"remote_id": "", "remote_path": "", "share_url": "", "share_password": "", "share_status": "", "status": model.ReplicaPending}).Error; err != nil {
				return err
			}
			if _, err := s.queueReplicaJob(tx, current.ID); err != nil {
				return err
			}
		}
		return s.audit(tx, p, "storage.replica_delete", id)
	})
	return errors.Join(operationErr, err)
}
