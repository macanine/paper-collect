package service

import (
	"context"
	"io"
	"os"
	"sort"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

// queueReplicaJob creates at most one pending/running replicate job per replica.
func (s *Service) queueReplicaJob(tx *gorm.DB, replicaID string) (bool, error) {
	var active int64
	if err := tx.Model(&model.Job{}).
		Where("replica_id = ? AND status IN ?", replicaID, []string{model.JobPending, model.JobRunning}).
		Count(&active).Error; err != nil {
		return false, wrap("count replica jobs", err)
	}
	if active > 0 {
		return false, nil
	}
	var replica model.Replica
	if err := tx.Where("id = ?", replicaID).First(&replica).Error; err != nil {
		return false, wrap("load replica", err)
	}
	if replica.Status != model.ReplicaPending {
		return false, nil
	}
	now := s.now()
	job := model.Job{
		ID:        newID(),
		Kind:      model.JobReplicate,
		Status:    model.JobPending,
		ReplicaID: replicaID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := tx.Create(&job).Error; err != nil {
		return false, wrap("create replicate job", err)
	}
	return true, nil
}

// Reconcile replans catalogued files under the current rules.
func (s *Service) Reconcile(ctx context.Context, p auth.Principal) (int, error) {
	n := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		var ms []model.Material
		if err := tx.Order("created_at ASC").Order("id ASC").Find(&ms).Error; err != nil {
			return err
		}
		for _, m := range ms {
			q, err := s.planMaterialTx(tx, m.ID, false)
			if err != nil {
				return err
			}
			n += q
		}
		return s.audit(tx, p, "storage.reconcile", "")
	})
	if err == nil {
		s.notify()
	}
	return n, err
}

// prepareReplicate marks the replica in progress and builds the executor input.
// A nil input means there is nothing to transfer; the job completes at once.
func (s *Service) prepareReplicate(ctx context.Context, job model.Job) (*jobexec.ReplicateInput, error) {
	var in *jobexec.ReplicateInput
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		var replica model.Replica
		var drive model.Drive
		var blob model.Blob
		if err := tx.Where("id = ?", job.ReplicaID).First(&replica).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", replica.DriveID).First(&drive).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", replica.BlobID).First(&blob).Error; err != nil {
			return err
		}
		if !drive.Enabled || replica.Status == model.ReplicaSkipped || replica.Status == model.ReplicaOrphaned || replica.Status == model.ReplicaReady {
			return nil
		}
		cfg, err := s.driveConfig(drive)
		if err != nil {
			return err
		}
		var m model.Material
		_ = tx.Select("file_name", "media_type").Where("blob_id = ?", blob.ID).First(&m).Error
		in = &jobexec.ReplicateInput{
			Drive:       cfg,
			Blob:        jobexec.Blob{SHA256: blob.SHA256, Size: blob.SizeBytes, FileName: m.FileName, MediaType: m.MediaType},
			DesiredPath: replica.DesiredPath, RemoteID: replica.RemoteID, RemotePath: replica.RemotePath,
			Move: drive.OnPathChange == model.OnPathChangeMove, Share: replica.Share,
		}
		state := model.ReplicaUploading
		if replica.RemoteID != "" {
			state = model.ReplicaMoving
		}
		return tx.Model(&replica).Updates(map[string]any{"status": state, "error": "", "updated_at": s.now()}).Error
	})
	return in, err
}

// commitReplicate stores the observed remote state. Upload success and share
// success are tracked separately: a share failure never rolls back an upload.
func (s *Service) commitReplicate(job model.Job, out *jobexec.ReplicateResult) error {
	var replica model.Replica
	if err := s.db.Where("id = ?", job.ReplicaID).First(&replica).Error; err != nil {
		return err
	}
	if out == nil {
		return s.completeReplicateInTx(job, replica, nil)
	}
	updates := map[string]any{"status": model.ReplicaReady, "error": "", "updated_at": s.now()}
	if out.Uploaded {
		updates["remote_id"] = out.RemoteID
		updates["remote_path"] = out.RemotePath
		updates["size_bytes"] = out.SizeBytes
		updates["verification"] = out.Verification
	} else if out.Moved {
		updates["remote_id"] = out.RemoteID
		updates["remote_path"] = out.RemotePath
	}
	if replica.Share {
		updates["share_url"] = out.ShareURL
		updates["share_password"] = out.SharePassword
		updates["share_expires_at"] = out.ShareExpiresAt
		updates["share_status"] = out.ShareStatus
		updates["error"] = redact(out.Note)
	} else {
		updates["share_status"] = ""
		updates["share_url"] = ""
		updates["share_password"] = ""
		updates["share_expires_at"] = nil
	}
	return s.completeReplicateInTx(job, replica, updates)
}

func (s *Service) completeReplicateInTx(job model.Job, replica model.Replica, replicaUpdates map[string]any) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockSyncTx(tx); err != nil {
			return err
		}
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		if replicaUpdates != nil {
			res := tx.Model(&model.Replica{}).Where("id = ?", replica.ID).Updates(replicaUpdates)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errLeaseLost
			}
		}
		if err := s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, ""); err != nil {
			return err
		}
		// Preserve a newer plan and schedule follow-up work after this lease ends.
		var current model.Replica
		if err := tx.Where("id = ?", replica.ID).First(&current).Error; err != nil {
			return err
		}
		changed := current.DesiredPath != replica.DesiredPath || current.RuleID != replica.RuleID || current.Share != replica.Share || current.Required != replica.Required
		if changed || replicaUpdates == nil {
			var m model.Material
			if err := tx.Where("blob_id = ?", replica.BlobID).First(&m).Error; err == nil {
				_, err = s.planMaterialTx(tx, m.ID, false)
				return err
			}
		}
		return s.refreshBlobStatus(tx, replica.BlobID)
	})
}

// RefreshReplicaShare rebuilds or refreshes the share of one ready replica.
func (s *Service) RefreshReplicaShare(ctx context.Context, p auth.Principal, id string) (model.Replica, error) {
	db := s.db.WithContext(ctx)
	var replica model.Replica
	if err := db.Where("id = ?", id).First(&replica).Error; err != nil {
		return model.Replica{}, apperr.NotFound("副本不存在")
	}
	if replica.Status != model.ReplicaReady {
		return model.Replica{}, apperr.Conflict("副本尚未就绪，不能创建分享")
	}
	var provider model.Drive
	if err := db.Where("id = ?", replica.DriveID).First(&provider).Error; err != nil {
		return model.Replica{}, apperr.NotFound("存储端点不存在")
	}
	adapter, err := s.adapterFor(provider)
	if err != nil {
		return model.Replica{}, apperr.Validation("存储端点配置无效: " + storageErrorText(err))
	}
	remote := replica.RemoteID
	if remote == "" {
		remote = replica.RemotePath
	}
	url, password, expires, shareStatus, note := jobexec.ShareReplica(ctx, adapter, remote)
	updates := map[string]any{
		"share_status":     shareStatus,
		"share_url":        url,
		"share_password":   password,
		"share_expires_at": nil,
		"error":            note,
		"updated_at":       s.now(),
	}
	if shareStatus == model.ShareStatusReady && expires != nil {
		updates["share_expires_at"] = expires
	}
	if err := db.Model(&model.Replica{}).Where("id = ?", replica.ID).Updates(updates).Error; err != nil {
		return model.Replica{}, apperr.Internal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return s.audit(tx, p, "storage.replica_share", replica.ID)
	}); err != nil {
		return model.Replica{}, apperr.Internal(err)
	}
	if err := db.Where("id = ?", replica.ID).First(&replica).Error; err != nil {
		return model.Replica{}, apperr.Internal(err)
	}
	return replica, nil
}

// Download is an open stream of the original material content.
type Download struct {
	Reader    io.ReadCloser
	MediaType string
	FileName  string
	Size      int64
}

type replicaSource struct {
	replica  model.Replica
	provider model.Drive
}

// readyReplicaSources returns every replica written to a drive, ordered by the
// drive's read_order. Disabled drives and orphaned copies stay readable.
func (s *Service) readyReplicaSources(ctx context.Context, blobID string) ([]replicaSource, error) {
	db := s.db.WithContext(ctx)
	var replicas []model.Replica
	if err := db.Where("blob_id = ? AND remote_id <> ?", blobID, "").
		Find(&replicas).Error; err != nil {
		return nil, err
	}
	if len(replicas) == 0 {
		return nil, nil
	}
	providers := map[string]model.Drive{}
	ids := make([]string, 0, len(replicas))
	for _, r := range replicas {
		ids = append(ids, r.DriveID)
	}
	var rows []model.Drive
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, p := range rows {
		providers[p.ID] = p
	}
	sources := make([]replicaSource, 0, len(replicas))
	for _, r := range replicas {
		if p, ok := providers[r.DriveID]; ok {
			sources = append(sources, replicaSource{replica: r, provider: p})
		}
	}
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].provider.ReadOrder != sources[j].provider.ReadOrder {
			return sources[i].provider.ReadOrder < sources[j].provider.ReadOrder
		}
		if sources[i].replica.DriveID != sources[j].replica.DriveID {
			return sources[i].replica.DriveID < sources[j].replica.DriveID
		}
		return sources[i].replica.CreatedAt.Before(sources[j].replica.CreatedAt)
	})
	return sources, nil
}

func (s *Service) openReplicaSource(ctx context.Context, source replicaSource) (io.ReadCloser, error) {
	adapter, err := s.adapterFor(source.provider)
	if err != nil {
		return nil, err
	}
	remote := source.replica.RemoteID
	if remote == "" {
		remote = source.replica.RemotePath
	}
	return adapter.Open(ctx, remote)
}

// OpenMaterialDownload returns the local original when present; otherwise it falls
// back to drive replicas in read_order.
func (s *Service) OpenMaterialDownload(ctx context.Context, id string) (Download, error) {
	db := s.db.WithContext(ctx)
	var m model.Material
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		return Download{}, apperr.NotFound("资料不存在")
	}
	if s.blobs.Exists(m.SHA256) {
		f, err := os.Open(s.blobs.Path(m.SHA256))
		if err == nil {
			return Download{Reader: f, MediaType: m.MediaType, FileName: m.FileName, Size: m.SizeBytes}, nil
		}
	}

	sources, err := s.readyReplicaSources(ctx, m.BlobID)
	if err != nil {
		return Download{}, apperr.Internal(err)
	}
	if len(sources) == 0 {
		return Download{}, apperr.Unavailable("没有可用的副本可供下载")
	}

	var lastErr error
	for _, source := range sources {
		body, err := s.openReplicaSource(ctx, source)
		if err != nil {
			lastErr = err
			s.logf("download replica unavailable: %s", storageErrorText(err))
			continue
		}
		return Download{Reader: body, MediaType: m.MediaType, FileName: m.FileName, Size: m.SizeBytes}, nil
	}
	if lastErr != nil {
		s.logf("download exhausted replicas: %s", storageErrorText(lastErr))
	}
	return Download{}, apperr.Unavailable("没有可用的副本可供下载")
}

func (s *Service) failReplicaJob(jobID, owner, replicaID, message string) {
	message = redact(message)
	s.failJob(jobID, owner, message, func(tx *gorm.DB) error {
		if replicaID == "" {
			return nil
		}
		if err := tx.Model(&model.Replica{}).Where("id = ?", replicaID).
			Updates(map[string]any{
				"status": model.ReplicaFailed, "error": message, "updated_at": s.now(),
			}).Error; err != nil {
			return err
		}
		var replica model.Replica
		if err := tx.Where("id = ?", replicaID).First(&replica).Error; err != nil {
			return err
		}
		return s.refreshBlobStatus(tx, replica.BlobID)
	})
}

// driveConfig decrypts a drive row into an executor configuration. Local roots
// are always derived server-side; the API can never supply them.
func (s *Service) driveConfig(p model.Drive) (jobexec.DriveConfig, error) {
	settings := model.DecodeMap(p.ConfigJSON)
	if p.Kind == model.KindLanzou {
		settings["folder_id"] = p.RootPath
	}
	if p.Kind == model.KindLocal {
		settings["root"] = s.localRoot(p.ID)
	}
	secrets := map[string]string{}
	if p.SecretCipher != "" {
		raw, err := s.box.Open(p.SecretCipher, []byte(p.ID))
		if err != nil {
			return jobexec.DriveConfig{}, err
		}
		secrets = model.DecodeMap(string(raw))
	}
	return jobexec.DriveConfig{ID: p.ID, Kind: p.Kind, Settings: settings, Secrets: secrets}, nil
}

// adapterFor builds a storage adapter for work the master performs itself.
func (s *Service) adapterFor(p model.Drive) (storage.Adapter, error) {
	cfg, err := s.driveConfig(p)
	if err != nil {
		return nil, err
	}
	return jobexec.NewAdapter(s.masterEnv(), cfg)
}

// SaveRefreshToken stores a rotated OneDrive refresh token. The write only
// applies while the stored token is still the one that was rotated, so a
// concurrent manual credential change is never overwritten.
func (s *Service) SaveRefreshToken(ctx context.Context, driveID, previous, next string) error {
	if previous == next || next == "" {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d model.Drive
		if err := tx.Where("id = ?", driveID).First(&d).Error; err != nil {
			return err
		}
		if d.Kind != model.KindOneDrive || d.SecretCipher == "" {
			return nil
		}
		raw, err := s.box.Open(d.SecretCipher, []byte(d.ID))
		if err != nil {
			return err
		}
		secrets := model.DecodeMap(string(raw))
		if secrets["refresh_token"] != previous {
			return nil
		}
		secrets["refresh_token"] = next
		cipher, err := s.sealSecret(d.ID, secrets)
		if err != nil {
			return err
		}
		return tx.Model(&model.Drive{}).Where("id = ? AND secret_cipher = ?", d.ID, d.SecretCipher).
			Updates(map[string]any{"secret_cipher": cipher, "updated_at": s.now()}).Error
	})
}
