package service

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

// loadPolicy returns the singleton storage policy, falling back to the documented
// default when the row is missing. A malformed stored policy is normalised rather
// than making the whole service unusable.
func (s *Service) loadPolicy(tx *gorm.DB) (model.Policy, error) {
	var pol model.Policy
	err := tx.Where("id = ?", 1).First(&pol).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Policy{ID: 1, Mode: model.ModeMirrorAll, MinimumReplicas: 1}, nil
	}
	if err != nil {
		return model.Policy{}, wrap("load policy", err)
	}
	if !isOneOf(pol.Mode, model.PolicyModes...) {
		pol.Mode = model.ModeMirrorAll
	}
	if pol.MinimumReplicas < 1 {
		pol.MinimumReplicas = 1
	}
	if pol.MinimumReplicas > maxMinimumReplicas {
		pol.MinimumReplicas = maxMinimumReplicas
	}
	return pol, nil
}

// loadProviders returns enabled endpoints ordered by priority. Disabled endpoints
// keep their historical replicas but never receive new work.
func loadProviders(tx *gorm.DB) ([]model.StorageProvider, error) {
	var providers []model.StorageProvider
	err := tx.Where("enabled = ?", true).
		Order("priority ASC").Order("id ASC").Find(&providers).Error
	if err != nil {
		return nil, wrap("load providers", err)
	}
	return providers, nil
}

func splitProviders(providers []model.StorageProvider) (primary, backup []model.StorageProvider) {
	for _, p := range providers {
		if p.Role == model.RolePrimary {
			primary = append(primary, p)
		} else {
			backup = append(backup, p)
		}
	}
	return primary, backup
}

// planBlob freezes the target endpoints of one blob into Replica rows.
//
//	mirror_all / primary_backup: every enabled endpoint (primary and backup)
//	balanced: minimum_replicas distinct primaries selected by weight, plus every backup
//
// Rows that already exist are kept, so a retry or reconcile never re-randomises a
// previously chosen target. The mode in force at planning time is stored on each row.
func (s *Service) planBlob(tx *gorm.DB, b model.Blob, pol model.Policy, providers []model.StorageProvider) ([]model.Replica, error) {
	var existing []model.Replica
	if err := tx.Where("blob_id = ?", b.ID).Find(&existing).Error; err != nil {
		return nil, wrap("load replicas", err)
	}
	have := make(map[string]model.Replica, len(existing))
	for _, r := range existing {
		have[r.ProviderID] = r
	}

	primary, backup := splitProviders(providers)
	var selected []model.StorageProvider
	switch pol.Mode {
	case model.ModeBalanced:
		want := pol.MinimumReplicas
		if want > len(primary) {
			want = len(primary)
		}
		chosen := make([]model.StorageProvider, 0, want)
		// Existing primaries are already frozen targets and fill the slots first.
		for _, p := range primary {
			if len(chosen) >= want {
				break
			}
			if _, ok := have[p.ID]; ok {
				chosen = append(chosen, p)
			}
		}
		if len(chosen) < want {
			var candidates []model.StorageProvider
			for _, p := range primary {
				if _, ok := have[p.ID]; !ok {
					candidates = append(candidates, p)
				}
			}
			chosen = append(chosen, weightedSample(candidates, want-len(chosen))...)
		}
		selected = append(selected, chosen...)
		selected = append(selected, backup...)
	default: // mirror_all and primary_backup write every enabled endpoint.
		selected = append(selected, primary...)
		selected = append(selected, backup...)
	}

	now := s.now()
	out := make([]model.Replica, 0, len(selected))
	seen := make(map[string]bool, len(selected))
	for _, p := range selected {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		if r, ok := have[p.ID]; ok {
			out = append(out, r)
			continue
		}
		r := model.Replica{
			ID:          newID(),
			BlobID:      b.ID,
			ProviderID:  p.ID,
			Status:      model.ReplicaPending,
			SizeBytes:   b.SizeBytes,
			ShareStatus: model.ShareStatusPending,
			PlanMode:    pol.Mode,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Create(&r).Error; err != nil {
			return nil, wrap("create replica", err)
		}
		have[p.ID] = r
		out = append(out, r)
	}
	return out, nil
}

// planBlobWork plans replicas and queues a replicate job for every pending one.
// It returns the number of newly queued jobs.
func (s *Service) planBlobWork(tx *gorm.DB, b model.Blob, pol model.Policy, providers []model.StorageProvider) (int, error) {
	replicas, err := s.planBlob(tx, b, pol, providers)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, r := range replicas {
		if r.Status != model.ReplicaPending {
			continue
		}
		created, err := s.queueReplicaJob(tx, r.ID)
		if err != nil {
			return queued, err
		}
		if created {
			queued++
		}
	}
	if err := s.refreshBlobStatus(tx, b.ID, pol.MinimumReplicas); err != nil {
		return queued, err
	}
	return queued, nil
}

// refreshBlobStatus recomputes the material status of one blob from replica facts.
// A material becomes archived only when the policy minimum is actually ready.
func (s *Service) refreshBlobStatus(tx *gorm.DB, blobID string, minimum int) error {
	if minimum < 1 {
		minimum = 1
	}
	var rows []struct {
		Status string
		N      int64
	}
	if err := tx.Model(&model.Replica{}).
		Select("status, COUNT(*) AS n").
		Where("blob_id = ?", blobID).
		Group("status").Scan(&rows).Error; err != nil {
		return wrap("count replicas", err)
	}
	counts := map[string]int64{}
	for _, r := range rows {
		counts[r.Status] += r.N
	}
	status := model.StatusPendingStorage
	switch {
	case counts[model.ReplicaReady] >= int64(minimum):
		status = model.StatusArchived
	case counts[model.ReplicaReady] == 0 &&
		counts[model.ReplicaPending] == 0 &&
		counts[model.ReplicaUploading] == 0 &&
		counts[model.ReplicaFailed] > 0:
		status = model.StatusDegraded
	}
	err := tx.Model(&model.Material{}).Where("blob_id = ?", blobID).
		Updates(map[string]any{"status": status, "updated_at": s.now()}).Error
	if err != nil {
		return wrap("update material status", err)
	}
	return nil
}

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

// Reconcile adds missing replicas (and their jobs) for existing blobs under the
// current policy. Extra replicas are never removed.
func (s *Service) Reconcile(ctx context.Context, p auth.Principal) (int, error) {
	db := s.db.WithContext(ctx)
	queued := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		pol, err := s.loadPolicy(tx)
		if err != nil {
			return err
		}
		providers, err := loadProviders(tx)
		if err != nil {
			return err
		}
		var blobs []model.Blob
		if err := tx.Order("created_at ASC").Order("id ASC").Find(&blobs).Error; err != nil {
			return wrap("list blobs", err)
		}
		for _, b := range blobs {
			n, err := s.planBlobWork(tx, b, pol, providers)
			if err != nil {
				return err
			}
			queued += n
		}
		return s.audit(tx, p, "storage.reconcile", "")
	})
	if err != nil {
		return 0, apperr.Internal(err)
	}
	if queued > 0 {
		s.notify()
	}
	return queued, nil
}

// RunReplicate executes one replicate job. Upload success and share success are
// tracked separately: a share failure never rolls back an uploaded replica.
func (s *Service) RunReplicate(ctx context.Context, jobID string) {
	db := s.db.WithContext(ctx)
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil || job.Kind != model.JobReplicate {
		return
	}
	if err := s.replicate(ctx, job); err != nil {
		if ctx.Err() != nil {
			if !errors.Is(context.Cause(ctx), errLeaseLost) {
				s.requeueJob(job.ID, job.LeaseOwner, func(tx *gorm.DB) error {
					return tx.Model(&model.Replica{}).
						Where("id = ? AND status = ?", job.ReplicaID, model.ReplicaUploading).
						Updates(map[string]any{"status": model.ReplicaPending, "updated_at": s.now()}).Error
				})
			}
			return
		}
		if errors.Is(err, errLeaseLost) {
			return
		}
		s.logf("replicate job failed: %s", storageErrorText(err))
		s.failReplicaJob(job.ID, job.LeaseOwner, job.ReplicaID, storageErrorText(err))
	}
}

func (s *Service) replicate(ctx context.Context, job model.Job) error {
	s.jobProgress(ctx, job, "检查副本、原文件和存储端点", 0, 0)
	db := s.db.WithContext(ctx)
	var replica model.Replica
	if err := db.Where("id = ?", job.ReplicaID).First(&replica).Error; err != nil {
		return fmt.Errorf("load replica: %w", err)
	}
	var blob model.Blob
	if err := db.Where("id = ?", replica.BlobID).First(&blob).Error; err != nil {
		return fmt.Errorf("load blob: %w", err)
	}
	var provider model.StorageProvider
	if err := db.Where("id = ?", replica.ProviderID).First(&provider).Error; err != nil {
		return fmt.Errorf("load provider: %w", err)
	}

	if replica.Status == model.ReplicaReady {
		return s.completeReplicateInTx(job, replica, nil)
	}
	if !provider.Enabled {
		return errors.New("storage provider is disabled")
	}
	if !s.blobs.Exists(blob.SHA256) {
		return errors.New("blob content is missing from the local store")
	}
	adapter, err := s.adapterFor(provider)
	if err != nil {
		return err
	}

	now := s.now()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		res := tx.Model(&model.Replica{}).
			Where("id = ? AND status IN ?", replica.ID, []string{model.ReplicaPending, model.ReplicaFailed, model.ReplicaUploading}).
			Updates(map[string]any{"status": model.ReplicaUploading, "error": "", "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errLeaseLost
		}
		return nil
	}); err != nil {
		return fmt.Errorf("mark replica uploading: %w", err)
	}

	name := safeRemoteName(s.replicaFileName(ctx, blob.ID, blob.FileName))
	obj := storage.Object{
		Key:    objectKey(blob.SHA256, name),
		Name:   name,
		Path:   s.blobs.Path(blob.SHA256),
		SHA256: blob.SHA256,
		Size:   blob.SizeBytes,
	}
	s.jobProgressWithMetrics(ctx, job, "正在上传文件并验证远端副本", 0, 0, map[string]any{
		"provider":   provider.Name,
		"size_bytes": blob.SizeBytes,
	})
	type uploadProgress struct{ done, total int64 }
	updatesProgress := make(chan uploadProgress, 1)
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		var last time.Time
		for p := range updatesProgress {
			if p.total <= 0 || time.Since(last) < 5*time.Second {
				continue
			}
			last = time.Now()
			s.jobProgress(ctx, job, fmt.Sprintf("文件传输中：%.1f / %.1f MiB", float64(p.done)/(1<<20), float64(p.total)/(1<<20)), int(p.done), int(p.total))
		}
	}()
	obj.Progress = func(done, total int64) {
		select {
		case updatesProgress <- uploadProgress{done, total}:
		default:
		}
	}
	uploadStarted := time.Now()
	stored, err := adapter.Put(ctx, obj)
	uploadDuration := time.Since(uploadStarted)
	close(updatesProgress)
	<-progressDone
	if err != nil {
		return err
	}
	size := stored.Size
	if size <= 0 {
		size = blob.SizeBytes
	}
	updates := map[string]any{
		"status":       model.ReplicaReady,
		"size_bytes":   size,
		"verification": stored.Verification,
		"error":        "",
		"remote_id":    stored.RemoteID,
		"remote_key":   obj.Key,
		"remote_name":  obj.Name,
		"updated_at":   now,
	}
	throughput := float64(0)
	if uploadDuration > 0 {
		throughput = float64(size) / uploadDuration.Seconds()
	}
	s.jobProgressWithMetrics(ctx, job, "副本上传完成，正在处理分享链接", 0, 0, map[string]any{
		"provider":               provider.Name,
		"size_bytes":             size,
		"duration_ms":            uploadDuration.Milliseconds(),
		"throughput_bytes_per_s": throughput,
		"verification":           stored.Verification,
	})
	shareStarted := time.Now()
	shareURL, sharePassword, shareExpires, shareStatus, shareErr := s.shareReplica(ctx, adapter, stored.RemoteID)
	shareDuration := time.Since(shareStarted)
	updates["share_status"] = shareStatus
	updates["share_url"] = shareURL
	updates["share_password"] = sharePassword
	updates["share_expires_at"] = shareExpires
	if shareErr != "" {
		updates["error"] = shareErr
		s.jobProgressWithMetrics(ctx, job, "副本已保存，分享链接创建失败，可稍后重试", 0, 0, map[string]any{
			"provider": provider.Name, "duration_ms": shareDuration.Milliseconds(), "share_status": shareStatus,
		})
	} else if shareStatus == model.ShareStatusUnsupported {
		s.jobProgressWithMetrics(ctx, job, "副本已保存，此端点不支持分享链接", 0, 0, map[string]any{
			"provider": provider.Name, "duration_ms": shareDuration.Milliseconds(), "share_status": shareStatus,
		})
	} else {
		s.jobProgressWithMetrics(ctx, job, "副本已保存，分享链接已生成", 0, 0, map[string]any{
			"provider": provider.Name, "duration_ms": shareDuration.Milliseconds(), "share_status": shareStatus,
		})
	}
	return s.completeReplicateInTx(job, replica, updates)
}

// replicaFileName follows the catalogue's current display name so a replica
// created after AI renaming (or a manual rename) uses the polished filename.
// Blob.FileName remains the fallback for legacy/shared blobs without materials.
func (s *Service) replicaFileName(ctx context.Context, blobID, fallback string) string {
	var name string
	err := s.db.WithContext(ctx).Model(&model.Material{}).
		Where("blob_id = ?", blobID).
		Order("updated_at DESC").
		Limit(1).
		Pluck("file_name", &name).Error
	if err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}

func (s *Service) completeReplicateInTx(job model.Job, replica model.Replica, replicaUpdates map[string]any) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
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
		pol, err := s.loadPolicy(tx)
		if err != nil {
			pol = model.Policy{Mode: model.ModeMirrorAll, MinimumReplicas: 1}
		}
		return s.refreshBlobStatus(tx, replica.BlobID, pol.MinimumReplicas)
	})
}

// shareReplica asks the adapter for a share link and classifies the outcome.
// An unsupported share is a normal outcome, not a failed replica.
func (s *Service) shareReplica(ctx context.Context, adapter storage.Adapter, remoteID string) (url, password string, expires *time.Time, status, note string) {
	if remoteID == "" {
		return "", "", nil, model.ShareStatusFailed, "share_remote_id_missing"
	}
	share, err := adapter.Share(ctx, remoteID)
	if err != nil {
		if errors.Is(err, storage.ErrUnsupported) || storage.AsCloudError(err).Class == storage.ClassUnsupported {
			return "", "", nil, model.ShareStatusUnsupported, ""
		}
		return "", "", nil, model.ShareStatusFailed, storageErrorText(err)
	}
	if share.Status == "unsupported" {
		return "", "", nil, model.ShareStatusUnsupported, ""
	}
	if share.URL == "" {
		return "", "", nil, model.ShareStatusFailed, "share_url_empty"
	}
	return share.URL, share.Password, share.ExpiresAt, model.ShareStatusReady, ""
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
	var provider model.StorageProvider
	if err := db.Where("id = ?", replica.ProviderID).First(&provider).Error; err != nil {
		return model.Replica{}, apperr.NotFound("存储端点不存在")
	}
	adapter, err := s.adapterFor(provider)
	if err != nil {
		return model.Replica{}, apperr.Validation("存储端点配置无效: " + storageErrorText(err))
	}
	remote := replica.RemoteID
	if remote == "" {
		remote = replica.RemoteKey
	}
	url, password, expires, shareStatus, note := s.shareReplica(ctx, adapter, remote)
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

// OpenMaterialDownload returns the local original when present; otherwise it falls
// back to ready replicas ordered primary first, then backup, then priority.
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

	var replicas []model.Replica
	if err := db.Where("blob_id = ? AND status = ?", m.BlobID, model.ReplicaReady).
		Find(&replicas).Error; err != nil {
		return Download{}, apperr.Internal(err)
	}
	if len(replicas) == 0 {
		return Download{}, apperr.Unavailable("没有可用的副本可供下载")
	}
	providers := map[string]model.StorageProvider{}
	ids := make([]string, 0, len(replicas))
	for _, r := range replicas {
		ids = append(ids, r.ProviderID)
	}
	var rows []model.StorageProvider
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return Download{}, apperr.Internal(err)
	}
	for _, p := range rows {
		providers[p.ID] = p
	}
	sort.SliceStable(replicas, func(i, j int) bool {
		pi, pj := providers[replicas[i].ProviderID], providers[replicas[j].ProviderID]
		ri := providerReadRank(pi.Role)
		rj := providerReadRank(pj.Role)
		if ri != rj {
			return ri < rj
		}
		if pi.Priority != pj.Priority {
			return pi.Priority < pj.Priority
		}
		if replicas[i].ProviderID != replicas[j].ProviderID {
			return replicas[i].ProviderID < replicas[j].ProviderID
		}
		return replicas[i].CreatedAt.Before(replicas[j].CreatedAt)
	})

	var lastErr error
	for _, r := range replicas {
		provider, ok := providers[r.ProviderID]
		if !ok {
			continue
		}
		adapter, err := s.adapterFor(provider)
		if err != nil {
			lastErr = err
			continue
		}
		remote := r.RemoteID
		if remote == "" {
			remote = r.RemoteKey
		}
		body, err := adapter.Open(ctx, remote)
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

func providerReadRank(role string) int {
	if role == model.RolePrimary {
		return 0
	}
	return 1
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
		pol, err := s.loadPolicy(tx)
		if err != nil {
			return err
		}
		return s.refreshBlobStatus(tx, replica.BlobID, pol.MinimumReplicas)
	})
}

// adapterFor builds a storage adapter from a provider row. Local roots are always
// derived server-side; the API can never supply them.
func (s *Service) adapterFor(p model.StorageProvider) (storage.Adapter, error) {
	settings := model.DecodeMap(p.ConfigJSON)
	if p.Kind == model.KindLocal {
		settings["root"] = s.localRoot(p.ID)
	}
	secrets := map[string]string{}
	if p.SecretCipher != "" {
		raw, err := s.box.Open(p.SecretCipher, []byte(p.ID))
		if err != nil {
			return nil, err
		}
		secrets = model.DecodeMap(string(raw))
	}
	cfg := storage.Config{ID: p.ID, Kind: p.Kind, Settings: settings, Secrets: secrets}
	if p.Kind == model.KindOneDrive {
		cfg.SaveRefreshToken = func(ctx context.Context, previous, next string) error {
			if previous == next || next == "" || secrets["refresh_token"] != previous {
				return nil
			}
			updated := make(map[string]string, len(secrets))
			for key, value := range secrets {
				updated[key] = value
			}
			updated["refresh_token"] = next
			cipher, err := s.sealSecret(p.ID, updated)
			if err != nil {
				return err
			}
			res := s.db.WithContext(ctx).Model(&model.StorageProvider{}).Where("id = ? AND secret_cipher = ?", p.ID, p.SecretCipher).
				Updates(map[string]any{"secret_cipher": cipher, "updated_at": s.now()})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				p.SecretCipher = cipher
				secrets["refresh_token"] = next
			}
			return nil
		}
	}
	return s.factory(cfg)
}

// objectKey is the stable content-addressed key used by local and WebDAV adapters.
func objectKey(sha, name string) string {
	if len(sha) < 2 {
		return name
	}
	return sha[:2] + "/" + sha + "/" + name
}

// safeRemoteName keeps a readable single-segment file name for remote adapters.
func safeRemoteName(raw string) string {
	name := raw
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "blob"
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if utf8.RuneCountInString(base) > 100 {
		base = string([]rune(base)[:100])
	}
	if utf8.RuneCountInString(ext) > 20 {
		ext = ""
	}
	return base + ext
}

// weightedSample picks k distinct providers using weights, without replacement.
func weightedSample(providers []model.StorageProvider, k int) []model.StorageProvider {
	if k <= 0 || len(providers) == 0 {
		return nil
	}
	if k > len(providers) {
		k = len(providers)
	}
	pool := append([]model.StorageProvider{}, providers...)
	out := make([]model.StorageProvider, 0, k)
	for len(out) < k {
		total := 0
		for _, p := range pool {
			total += providerWeight(p)
		}
		if total <= 0 {
			out = append(out, pool[0])
			pool = pool[1:]
			continue
		}
		draw := secureIntn(total)
		acc := 0
		idx := 0
		for i, p := range pool {
			acc += providerWeight(p)
			if draw < acc {
				idx = i
				break
			}
		}
		out = append(out, pool[idx])
		pool = append(pool[:idx], pool[idx+1:]...)
	}
	return out
}

func providerWeight(p model.StorageProvider) int {
	if p.Weight < 1 {
		return 1
	}
	if p.Weight > 1000 {
		return 1000
	}
	return p.Weight
}

func secureIntn(n int) int {
	if n <= 1 {
		return 0
	}
	var buf [8]byte
	if _, err := crand.Read(buf[:]); err != nil {
		return 0
	}
	return int(binary.BigEndian.Uint64(buf[:]) % uint64(n))
}
