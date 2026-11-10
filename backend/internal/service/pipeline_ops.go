package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const (
	// maxJobAttempts bounds retries of temporary failures and lease losses.
	maxJobAttempts = 6
	// waitAIRetry is how often a job waiting for AI configuration looks again
	// on its own; saving AI settings wakes it at once.
	waitAIRetry = 15 * time.Minute
)

// retryBackoff is the delay after the n-th failed attempt.
var retryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

func backoffFor(attempts int) time.Duration {
	i := attempts - 1
	if i < 0 {
		i = 0
	}
	if i >= len(retryBackoff) {
		i = len(retryBackoff) - 1
	}
	return retryBackoff[i]
}

func isAIKind(kind string) bool {
	return kind == model.JobAnalyze || kind == model.JobGroup || kind == model.JobDecide || kind == model.JobEntity
}

// retryLater returns a running job to the queue to run again at a given time.
// counted=false gives the attempt back (waiting for configuration is not a
// failure of the job itself).
func (s *Service) retryLater(job model.Job, at time.Time, message string, counted bool, projections ...func(*gorm.DB) error) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobPending, ""); err != nil {
			return err
		}
		updates := map[string]any{"next_attempt_at": at, "progress_message": redact(message), "error": redact(message)}
		if !counted {
			updates["attempts"] = gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END")
		}
		if err := tx.Model(&model.Job{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
			return err
		}
		for _, projection := range projections {
			if err := projection(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, errLeaseLost) {
			s.logf("requeue job: %v", err)
		}
		return false
	}
	s.notify()
	return true
}

// aiJobMaterials lists the files whose stage follows an AI job.
func aiJobMaterials(tx *gorm.DB, job model.Job) []string {
	var ids []string
	switch job.Kind {
	case model.JobAnalyze:
		if job.MaterialID != "" {
			ids = []string{job.MaterialID}
		}
	case model.JobGroup:
		var latest model.Job
		if tx.Select("id", "payload_json").Where("id = ?", job.ID).First(&latest).Error == nil {
			var payload groupPayload
			if json.Unmarshal([]byte(latest.PayloadJSON), &payload) == nil && len(payload.Files) > 0 {
				return payload.Files
			}
		}
		_ = tx.Model(&model.Material{}).Where("batch_id = ? AND ai_step = ?", job.BatchID, model.StepGroup).Pluck("id", &ids).Error
	case model.JobDecide:
		_ = tx.Model(&model.Material{}).Where("pack_id = ? AND ai_step = ?", job.PackID, model.StepDecide).Pluck("id", &ids).Error
	}
	return ids
}

func setStages(ids []string, from []string, to string) func(*gorm.DB) error {
	return func(tx *gorm.DB) error {
		if len(ids) == 0 {
			return nil
		}
		return tx.Model(&model.Material{}).Where("id IN ? AND stage IN ?", ids, from).
			Updates(map[string]any{"stage": to, "updated_at": time.Now()}).Error
	}
}

// failAIJob applies the failure policy of the AI pipeline:
// configuration problems wait for AI, temporary problems retry with backoff,
// and only then does a file need attention. Grouping never leaves files
// without a set: it falls back to single-file sets.
func (s *Service) failAIJob(job model.Job, class, message string) {
	ids := aiJobMaterials(s.db, job)
	now := s.now()
	switch {
	case class == jobexec.ClassConfig:
		s.retryLater(job, now.Add(waitAIRetry), "等待 AI 配置："+message, false,
			setStages(ids, []string{model.StageProcessing}, model.StageWaitingAI))
		return
	case class == jobexec.ClassTransient && job.Attempts < maxJobAttempts:
		delay := backoffFor(job.Attempts)
		s.retryLater(job, now.Add(delay), fmt.Sprintf("%s（%s 后自动重试）", message, delay), true,
			setStages(ids, []string{model.StageWaitingAI}, model.StageProcessing))
		return
	}
	switch job.Kind {
	case model.JobAnalyze:
		s.failJob(job.ID, job.LeaseOwner, message, func(tx *gorm.DB) error {
			return s.readFailedTx(tx, job, message)
		})
	case model.JobGroup:
		s.applyGroupFallback(job)
	case model.JobDecide:
		s.failJob(job.ID, job.LeaseOwner, message, func(tx *gorm.DB) error {
			return s.decisionFailedTx(tx, job.PackID, job.ID, message, nil)
		})
	default:
		s.failJob(job.ID, job.LeaseOwner, message)
	}
	s.notify()
}

// readFailedTx handles a file the model could not read after every retry.
// The file is not left behind: it continues to grouping and decision with
// what its name and path say, flagged so a person checks it.
func (s *Service) readFailedTx(tx *gorm.DB, job model.Job, message string) error {
	now := s.now()
	if err := tx.Model(&model.FileCard{}).Where("material_id = ?", job.MaterialID).
		Updates(map[string]any{"status": model.AnalysisFailed, "error": message, "updated_at": now}).Error; err != nil {
		return err
	}
	var m model.Material
	if err := tx.Where("id = ?", job.MaterialID).First(&m).Error; err != nil {
		return nil
	}
	reasons := appendUnique(carryReasons(model.DecodeStrings(m.AttentionJSON)), model.AttentionMetadataOnly)
	next := model.StepDecide
	if m.BatchID != "" || m.PackID == "" {
		next = model.StepGroup
	}
	if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
		"stage": model.StageProcessing, "ai_step": next, "attention_json": model.EncodeStrings(reasons), "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if next == model.StepGroup {
		batch := m.BatchID
		if batch == "" {
			batch = newID()
			if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Update("batch_id", batch).Error; err != nil {
				return err
			}
		}
		return s.maybeEnqueueGroupTx(tx, batch, now)
	}
	return s.enqueueDecideTx(tx, m.PackID, now)
}

// wakeWaitingAI lets jobs that wait for AI configuration run now.
func (s *Service) wakeWaitingAI(ctx context.Context) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Job{}).
			Where("status = ? AND kind IN ? AND next_attempt_at IS NOT NULL", model.JobPending,
				[]string{model.JobAnalyze, model.JobGroup, model.JobDecide, model.JobEntity}).
			Update("next_attempt_at", nil).Error; err != nil {
			return err
		}
		return tx.Model(&model.Material{}).Where("stage = ?", model.StageWaitingAI).
			Updates(map[string]any{"stage": model.StageProcessing, "updated_at": s.now()}).Error
	})
	if err != nil {
		s.logf("wake waiting ai jobs: %v", err)
		return
	}
	s.notify()
}

// poisonJob fails a job that keeps losing its lease (for example because it
// crashes the process) instead of requeueing it forever.
func (s *Service) poisonJob(tx *gorm.DB, job model.Job, now time.Time) error {
	const message = "任务多次中断，已停止自动重试"
	if err := tx.Model(&model.Job{}).Where("id = ?", job.ID).Updates(map[string]any{
		"status": model.JobFailed, "error": message, "progress_message": "处理失败：" + message,
		"lease_owner": "", "lease_until": nil, "heartbeat_at": nil, "finished_at": now, "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if err := releaseJobLockTx(tx, job.ID); err != nil {
		return err
	}
	switch job.Kind {
	case model.JobAnalyze:
		return s.readFailedTx(tx, job, message)
	case model.JobDecide:
		return s.decisionFailedTx(tx, job.PackID, job.ID, message, nil)
	case model.JobGroup:
		// Never leave files without a set: keep each one on its own.
		ids := aiJobMaterials(tx, job)
		if len(ids) == 0 {
			return nil
		}
		var rows []model.Material
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		in := catalogai.GroupInput{}
		for _, m := range rows {
			in.Files = append(in.Files, catalogai.GroupFile{ID: m.ID, CurrentSet: m.PackID})
		}
		payload, _ := json.Marshal(groupPayload{Files: ids})
		if err := tx.Model(&model.Job{}).Where("id = ?", job.ID).Update("payload_json", string(payload)).Error; err != nil {
			return err
		}
		_, err := s.applyGroupingTx(tx, job, catalogai.Singletons(in), true)
		return err
	case model.JobReplicate:
		if job.ReplicaID == "" {
			return nil
		}
		return tx.Model(&model.Replica{}).Where("id = ?", job.ReplicaID).
			Updates(map[string]any{"status": model.ReplicaFailed, "error": message, "updated_at": now}).Error
	case model.JobIngest:
		if job.UploadID == "" {
			return nil
		}
		return tx.Model(&model.Upload{}).Where("id = ? AND job_id = ?", job.UploadID, job.ID).
			Updates(map[string]any{"status": model.UploadFailed, "updated_at": now}).Error
	}
	return nil
}

// resetAIProjectionTx puts the files of a manually retried AI job back into
// processing at the step the job performs.
func (s *Service) resetAIProjectionTx(tx *gorm.DB, job model.Job, now time.Time) error {
	ids := aiJobMaterials(tx, job)
	step := ""
	switch job.Kind {
	case model.JobAnalyze:
		step = model.StepRead
		if err := tx.Model(&model.FileCard{}).Where("material_id = ?", job.MaterialID).
			Updates(map[string]any{"status": model.AnalysisPending, "error": "", "job_id": job.ID, "updated_at": now}).Error; err != nil {
			return err
		}
	case model.JobGroup:
		step = model.StepGroup
	case model.JobDecide:
		step = model.StepDecide
		if job.PackID != "" {
			if err := tx.Model(&model.Material{}).Where("pack_id = ? AND ai_step <> ?", job.PackID, model.StepRead).
				Pluck("id", &ids).Error; err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []model.Material
	if err := tx.Select("id", "attention_json").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	for _, m := range rows {
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"stage": model.StageProcessing, "ai_step": step,
			"attention_json": model.EncodeStrings(carryReasons(model.DecodeStrings(m.AttentionJSON))), "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// RetryFailedJobs retries every failed job of a kind (all kinds when empty).
func (s *Service) RetryFailedJobs(ctx context.Context, p auth.Principal, kind string) (int, error) {
	q := s.db.WithContext(ctx).Model(&model.Job{}).Where("status = ?", model.JobFailed)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var ids []string
	if err := q.Order("created_at ASC").Limit(500).Pluck("id", &ids).Error; err != nil {
		return 0, apperr.Internal(err)
	}
	n := 0
	for _, id := range ids {
		if _, err := s.RetryJob(ctx, p, id); err == nil {
			n++
		}
	}
	return n, nil
}

// RerunScope selects the files a library re-run covers.
const (
	RerunScopeAttention = "attention" // files that need attention
	RerunScopeAll       = "all"       // the whole library
)

// Re-run modes.
const (
	RerunModeDecide = "decide" // keep the sets, decide their catalogues again
	RerunModeFull   = "full"   // read (cached text is reused), group and decide again
)

// RerunPreview reports what a re-run would do.
type RerunPreview struct {
	Materials      int64
	Packs          int64
	EstimatedCalls int64
	Applied        bool
}

// RerunAI puts existing files through the AI pipeline again. Locked fields
// and roles set by people are kept. With dryRun it only counts.
func (s *Service) RerunAI(ctx context.Context, p auth.Principal, mode, scope string, dryRun bool) (RerunPreview, error) {
	if mode != RerunModeDecide && mode != RerunModeFull {
		return RerunPreview{}, apperr.Validation("mode 只能是 decide 或 full")
	}
	if scope != RerunScopeAttention && scope != RerunScopeAll {
		return RerunPreview{}, apperr.Validation("scope 只能是 attention 或 all")
	}
	db := s.db.WithContext(ctx)
	selectMaterials := func(tx *gorm.DB) *gorm.DB {
		q := tx.Model(&model.Material{}).Where("ai_step = ''")
		if scope == RerunScopeAttention {
			q = q.Where("stage = ?", model.StageAttention)
		}
		return q
	}
	var out RerunPreview
	if err := selectMaterials(db).Count(&out.Materials).Error; err != nil {
		return out, apperr.Internal(err)
	}
	if err := selectMaterials(db).Where("pack_id <> ''").Distinct("pack_id").Count(&out.Packs).Error; err != nil {
		return out, apperr.Internal(err)
	}
	out.EstimatedCalls = out.Packs
	var uncarded int64
	if mode == RerunModeFull {
		if err := selectMaterials(db).Where("id NOT IN (?)", db.Model(&model.FileCard{}).Select("material_id").
			Where("status = ?", model.AnalysisSucceeded)).Count(&uncarded).Error; err != nil {
			return out, apperr.Internal(err)
		}
		out.EstimatedCalls += uncarded + (out.Materials+groupChunkFiles-1)/groupChunkFiles
	}
	if dryRun || out.Materials == 0 {
		return out, nil
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		now := s.now()
		var rows []model.Material
		if err := selectMaterials(tx).Select("id", "pack_id", "attention_json").Find(&rows).Error; err != nil {
			return err
		}
		if mode == RerunModeDecide {
			packs := map[string]bool{}
			for _, m := range rows {
				if m.PackID != "" {
					packs[m.PackID] = true
				}
			}
			for packID := range packs {
				if err := tx.Model(&model.Material{}).Where("pack_id = ? AND ai_step = ''", packID).
					Updates(map[string]any{"stage": model.StageProcessing, "ai_step": model.StepDecide, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := s.enqueueDecideTx(tx, packID, now); err != nil {
					return err
				}
			}
			return s.audit(tx, p, "ai.rerun_decide", scope)
		}
		batch := newID()
		ids := make([]string, 0, len(rows))
		for _, m := range rows {
			ids = append(ids, m.ID)
		}
		cards := loadCards(tx, ids)
		for _, m := range rows {
			if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Update("batch_id", batch).Error; err != nil {
				return err
			}
			if _, ok := cards[m.ID]; ok {
				if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
					"stage": model.StageProcessing, "ai_step": model.StepGroup, "attention_json": "[]", "updated_at": now,
				}).Error; err != nil {
					return err
				}
				continue
			}
			if err := s.enqueueReadTx(tx, m.ID, now); err != nil {
				return err
			}
		}
		if err := s.maybeEnqueueGroupTx(tx, batch, now); err != nil {
			return err
		}
		return s.audit(tx, p, "ai.rerun_full", scope)
	})
	if err != nil {
		return out, apperr.Internal(err)
	}
	s.notify()
	out.Applied = true
	return out, nil
}
