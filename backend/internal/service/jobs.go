package service

import (
	"context"
	"errors"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// JobFilter narrows a job list. Empty fields are ignored.
type JobFilter struct {
	Status string
	Kind   string
}

// ListJobs returns jobs newest first.
func (s *Service) ListJobs(ctx context.Context, f JobFilter, pg PageQuery) (Page[model.Job], error) {
	if f.Status != "" && !isOneOf(f.Status, model.JobPending, model.JobRunning, model.JobSucceeded, model.JobFailed) {
		return Page[model.Job]{}, apperr.Validation("status 不在允许范围内")
	}
	if f.Kind != "" && !isOneOf(f.Kind, model.JobIngest, model.JobReplicate, model.JobAnalyze, model.JobIndex) {
		return Page[model.Job]{}, apperr.Validation("kind 不在允许范围内")
	}
	db := s.db.WithContext(ctx)
	build := func() *gorm.DB {
		tx := db.Model(&model.Job{})
		if f.Status != "" {
			tx = tx.Where("status = ?", f.Status)
		}
		if f.Kind != "" {
			tx = tx.Where("kind = ?", f.Kind)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return Page[model.Job]{}, apperr.Internal(err)
	}
	items := []model.Job{}
	if err := build().Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error; err != nil {
		return Page[model.Job]{}, apperr.Internal(err)
	}
	return newPage(items, total, pg), nil
}

// GetJob returns one job.
func (s *Service) GetJob(ctx context.Context, id string) (model.Job, error) {
	var job model.Job
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&job).Error; err != nil {
		return model.Job{}, apperr.NotFound("任务不存在")
	}
	return job, nil
}

// RetryJob returns a failed job to pending with the same job id. Replicate jobs
// keep their frozen target provider; they are never re-planned here.
func (s *Service) RetryJob(ctx context.Context, p auth.Principal, id string) (model.Job, error) {
	db := s.db.WithContext(ctx)
	var result model.Job
	err := db.Transaction(func(tx *gorm.DB) error {
		var job model.Job
		if err := tx.Where("id = ?", id).First(&job).Error; err != nil {
			return apperr.NotFound("任务不存在")
		}
		if job.Status != model.JobFailed {
			return apperr.Conflict("只有失败的任务可以重试")
		}
		now := s.now()
		switch job.Kind {
		case model.JobIngest:
			if job.UploadID == "" {
				return apperr.Conflict("归档任务缺少上传会话")
			}
			var up model.Upload
			if err := tx.Where("id = ?", job.UploadID).First(&up).Error; err != nil {
				return apperr.Conflict("上传会话不存在")
			}
			if up.JobID != "" && up.JobID != job.ID {
				return apperr.Conflict("上传会话已绑定其他任务")
			}
			if err := tx.Model(&model.Upload{}).Where("id = ?", up.ID).
				Updates(map[string]any{"status": model.UploadProcessing, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobAnalyze:
			if job.MaterialID == "" {
				return apperr.Conflict("分析任务缺少资料")
			}
			var analysis model.MaterialAnalysis
			if err := tx.Where("material_id = ?", job.MaterialID).First(&analysis).Error; err != nil {
				return apperr.Conflict("分析记录不存在")
			}
			if err := tx.Model(&model.MaterialAnalysis{}).Where("id = ?", analysis.ID).
				Updates(map[string]any{"status": model.AnalysisPending, "error": "", "job_id": job.ID, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobIndex:
			if job.MaterialID == "" {
				return apperr.Conflict("索引任务缺少资料")
			}
			if err := tx.Model(&model.MaterialIndexState{}).Where("material_id = ?", job.MaterialID).
				Updates(map[string]any{"status": "pending", "error": "", "job_id": job.ID, "updated_at": now}).Error; err != nil {
				return err
			}
		case model.JobReplicate:
			if job.ReplicaID == "" {
				return apperr.Conflict("复制任务缺少副本")
			}
			var replica model.Replica
			if err := tx.Where("id = ?", job.ReplicaID).First(&replica).Error; err != nil {
				return apperr.Conflict("副本不存在")
			}
			if replica.Status != model.ReplicaReady {
				if err := tx.Model(&model.Replica{}).Where("id = ?", replica.ID).
					Updates(map[string]any{"status": model.ReplicaPending, "error": "", "updated_at": now}).Error; err != nil {
					return err
				}
			}
		default:
			return apperr.Conflict("未知任务类型")
		}
		if err := tx.Model(&model.Job{}).Where("id = ?", job.ID).
			Updates(map[string]any{"status": model.JobPending, "error": "", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", job.ID).First(&result).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "job.retry", job.ID)
	})
	if err != nil {
		if _, ok := err.(*apperr.Error); ok {
			return model.Job{}, err
		}
		return model.Job{}, apperr.Internal(err)
	}
	s.notify()
	return result, nil
}

func (s *Service) succeedJob(jobID string) {
	if err := s.db.Model(&model.Job{}).Where("id = ?", jobID).
		Updates(map[string]any{
			"status": model.JobSucceeded, "error": "", "updated_at": s.now(),
		}).Error; err != nil {
		s.logf("complete job: %v", err)
	}
}

// workerPollInterval is the fallback wake-up when no in-process notification
// arrives (for example after a manual database change).
const workerPollInterval = 2 * time.Second

var errNoJob = errors.New("no pending job")

// StartWorker launches the single-process job worker. It returns immediately.
func (s *Service) StartWorker(ctx context.Context) {
	go s.workerLoop(ctx)
}

func (s *Service) workerLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if s.ProcessNext(ctx) {
			continue
		}
		timer := time.NewTimer(workerPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// ProcessNext claims and runs at most one pending job. It reports whether work was done.
func (s *Service) ProcessNext(ctx context.Context) bool {
	job, err := s.claimJob(ctx)
	if err != nil {
		if errors.Is(err, errNoJob) || ctx.Err() != nil {
			return false
		}
		s.logf("claim job: %v", err)
		return false
	}
	s.executeJob(ctx, job)
	return true
}

// ProcessAvailable drains currently pending jobs up to limit. A limit <= 0 means
// no explicit bound. It is used by tests and by operational smoke checks.
func (s *Service) ProcessAvailable(ctx context.Context, limit int) int {
	done := 0
	for limit <= 0 || done < limit {
		if ctx.Err() != nil {
			break
		}
		if !s.ProcessNext(ctx) {
			break
		}
		done++
	}
	return done
}

func (s *Service) claimJob(ctx context.Context) (model.Job, error) {
	db := s.db.WithContext(ctx)
	var job model.Job
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ?", model.JobPending).
			Order("created_at ASC").Order("id ASC").First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNoJob
			}
			return err
		}
		res := tx.Model(&model.Job{}).
			Where("id = ? AND status = ?", job.ID, model.JobPending).
			Updates(map[string]any{
				"status":     model.JobRunning,
				"attempts":   gorm.Expr("attempts + 1"),
				"updated_at": s.now(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errNoJob
		}
		job.Status = model.JobRunning
		job.Attempts++
		job.UpdatedAt = s.now()
		return nil
	})
	if err != nil {
		return model.Job{}, err
	}
	return job, nil
}

func (s *Service) executeJob(ctx context.Context, job model.Job) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("job %s panicked", job.ID)
			s.failJob(job.ID, "任务执行发生内部错误")
			if job.UploadID != "" {
				_ = s.db.Model(&model.Upload{}).Where("id = ?", job.UploadID).
					Updates(map[string]any{"status": model.UploadFailed, "updated_at": s.now()}).Error
			}
			if job.ReplicaID != "" {
				_ = s.db.Model(&model.Replica{}).
					Where("id = ? AND status = ?", job.ReplicaID, model.ReplicaUploading).
					Updates(map[string]any{"status": model.ReplicaFailed, "error": "任务执行发生内部错误", "updated_at": s.now()}).Error
			}
		}
	}()
	switch job.Kind {
	case model.JobIngest:
		s.RunIngest(ctx, job.ID)
	case model.JobReplicate:
		s.RunReplicate(ctx, job.ID)
	case model.JobAnalyze:
		s.RunAnalyze(ctx, job.ID)
	case model.JobIndex:
		s.RunIndex(ctx, job.ID)
	default:
		s.failJob(job.ID, "未知任务类型")
	}
}
