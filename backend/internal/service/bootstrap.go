package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Bootstrap prepares an empty database: it creates the first administrator from
// the given credentials (only when no user exists), the singleton storage policy,
// and a default local provider. It is idempotent.
func (s *Service) Bootstrap(ctx context.Context, adminUsername, adminPassword string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize first-start initialization across app instances. On
		// PostgreSQL this advisory lock is released automatically at commit.
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(0x7061706572636f6c)).Error; err != nil {
				return wrap("lock bootstrap", err)
			}
		}
		var users int64
		if err := tx.Model(&model.User{}).Count(&users).Error; err != nil {
			return wrap("count users", err)
		}
		if users == 0 {
			if adminUsername == "" || adminPassword == "" {
				return ErrBootstrapNeeded
			}
			hash, err := auth.HashPassword(adminPassword)
			if err != nil {
				return wrap("hash admin password", err)
			}
			now := s.now()
			admin := model.User{
				ID:           newID(),
				Username:     adminUsername,
				PasswordHash: hash,
				Role:         model.RoleAdmin,
				Enabled:      true,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := tx.Create(&admin).Error; err != nil {
				return wrap("create admin", err)
			}
			if err := s.audit(tx, auth.Principal{UserID: admin.ID}, "bootstrap.admin", admin.ID); err != nil {
				return err
			}
		}

		var policies int64
		if err := tx.Model(&model.Policy{}).Where("id = ?", 1).Count(&policies).Error; err != nil {
			return wrap("count policy", err)
		}
		if policies == 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model.Policy{ID: 1, Mode: model.ModeMirrorAll, MinimumReplicas: 1, UpdatedAt: s.now()}).Error; err != nil {
				return wrap("create policy", err)
			}
		}

		var aiSettings int64
		if err := tx.Model(&model.AISettings{}).Where("id = ?", 1).Count(&aiSettings).Error; err != nil {
			return wrap("count ai settings", err)
		}
		if aiSettings == 0 {
			row := defaultAISettings()
			row.UpdatedAt = s.now()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return wrap("create ai settings", err)
			}
		}
		var existingAISettings model.AISettings
		if err := tx.Where("id = ?", 1).First(&existingAISettings).Error; err == nil {
			if err := s.migrateLegacyOCRProvider(tx, &existingAISettings); err != nil {
				return wrap("migrate OCR provider", err)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return wrap("load ai settings", err)
		}

		var searchSettings int64
		if err := tx.Model(&model.SearchSettings{}).Where("id = ?", 1).Count(&searchSettings).Error; err != nil {
			return wrap("count search settings", err)
		}
		if searchSettings == 0 {
			row := defaultSearchSettings()
			row.UpdatedAt = s.now()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return wrap("create search settings", err)
			}
		}

		var providers int64
		if err := tx.Model(&model.StorageProvider{}).Count(&providers).Error; err != nil {
			return wrap("count providers", err)
		}
		if providers == 0 {
			now := s.now()
			local := model.StorageProvider{
				ID:         newID(),
				Name:       "本地存储",
				Kind:       model.KindLocal,
				Role:       model.RolePrimary,
				Enabled:    true,
				Priority:   0,
				Weight:     1,
				ConfigJSON: "{}",
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := tx.Create(&local).Error; err != nil {
				return wrap("create default provider", err)
			}
			if err := os.MkdirAll(s.localRoot(local.ID), 0o700); err != nil {
				return wrap("create local root", err)
			}
		}
		return nil
	})
}

// RecoverInterrupted recovers work after a restart. SQLite has a single writer,
// so all running jobs are local and can be reset. PostgreSQL only resets rows
// whose lease has expired, preserving work owned by healthy peer instances.
func (s *Service) RecoverInterrupted(ctx context.Context) error {
	if s.resetRunningOnStart {
		return s.recoverJobs(s.db.WithContext(ctx), s.now())
	}
	_, err := s.RecoverExpiredLeases(ctx)
	return err
}

// RecoverExpiredLeases is a lease reaper shared by all instances. The first
// transaction locks a bounded batch with SKIP LOCKED and re-checks expiry before
// resetting it, so a concurrent heartbeat always wins one side of the race.
func (s *Service) RecoverExpiredLeases(ctx context.Context) (int, error) {
	now := s.now()
	recovered := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		query := tx.Model(&model.Job{}).
			Where("status = ? AND (lease_until IS NULL OR lease_until < ?)", model.JobRunning, now).
			Order("created_at ASC").Limit(100)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Pluck("id", &ids).Error; err != nil {
			return wrap("find expired jobs", err)
		}
		if len(ids) == 0 {
			return nil
		}
		res := tx.Model(&model.Job{}).
			Where("id IN ? AND status = ? AND (lease_until IS NULL OR lease_until < ?)",
				ids, model.JobRunning, now).
			Updates(map[string]any{
				"status": model.JobPending, "lease_owner": "", "lease_until": nil,
				"heartbeat_at": nil, "started_at": nil, "finished_at": nil, "duration_ms": 0,
				"progress_message": "任务中断，已重新排队", "progress_current": 0, "progress_total": 0, "updated_at": now,
			})
		if res.Error != nil {
			return wrap("recover jobs", res.Error)
		}
		recovered = int(res.RowsAffected)
		if recovered == 0 {
			return nil
		}
		return s.resetProjectionTx(tx, ids, now)
	})
	return recovered, err
}

func (s *Service) recoverJobs(db *gorm.DB, now time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var ids []string
		if err := tx.Model(&model.Job{}).Where("status = ?", model.JobRunning).
			Pluck("id", &ids).Error; err != nil {
			return wrap("find recoverable jobs", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Model(&model.Job{}).Where("id IN ?", ids).
			Updates(map[string]any{
				"status": model.JobPending, "lease_owner": "", "lease_until": nil,
				"heartbeat_at": nil, "started_at": nil, "finished_at": nil, "duration_ms": 0,
				"progress_message": "任务中断，已重新排队", "progress_current": 0, "progress_total": 0, "updated_at": now,
			}).Error; err != nil {
			return wrap("recover jobs", err)
		}
		return s.resetProjectionTx(tx, ids, now)
	})
}

// resetProjectionTx resets only projections owned by the reclaimed jobs and
// releases their material locks. A peer's healthy job is never downgraded.
func (s *Service) resetProjectionTx(tx *gorm.DB, jobIDs []string, now time.Time) error {
	if len(jobIDs) == 0 {
		return nil
	}
	if err := tx.Model(&model.Replica{}).
		Where("status = ? AND EXISTS (SELECT 1 FROM jobs WHERE jobs.id IN ? AND jobs.kind = ? AND jobs.status = ? AND jobs.replica_id = replicas.id)",
			model.ReplicaUploading, jobIDs, model.JobReplicate, model.JobPending).
		Updates(map[string]any{"status": model.ReplicaPending, "updated_at": now}).Error; err != nil {
		return wrap("recover replicas", err)
	}
	if err := tx.Model(&model.MaterialAnalysis{}).
		Where("status = ? AND EXISTS (SELECT 1 FROM jobs WHERE jobs.id IN ? AND jobs.kind = ? AND jobs.status = ? AND jobs.id = material_analyses.job_id)",
			model.AnalysisRunning, jobIDs, model.JobAnalyze, model.JobPending).
		Updates(map[string]any{"status": model.AnalysisPending, "updated_at": now}).Error; err != nil {
		return wrap("recover analyses", err)
	}
	if err := tx.Model(&model.MaterialIndexState{}).
		Where("status = ? AND EXISTS (SELECT 1 FROM jobs WHERE jobs.id IN ? AND jobs.kind = ? AND jobs.status = ? AND jobs.id = material_index_states.job_id)",
			"running", jobIDs, model.JobIndex, model.JobPending).
		Updates(map[string]any{"status": "pending", "updated_at": now}).Error; err != nil {
		return wrap("recover index states", err)
	}
	if err := tx.Where("job_id IN ?", jobIDs).Delete(&model.JobLock{}).Error; err != nil {
		return wrap("release expired job locks", err)
	}
	return nil
}

// localRoot is the fixed directory of a local provider. The API never accepts a path.
func (s *Service) localRoot(providerID string) string {
	return filepath.Join(s.blobs.DataDir(), "providers", providerID)
}

// isUniqueViolation recognises duplicate-key errors across SQLite and PostgreSQL.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
