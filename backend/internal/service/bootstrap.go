package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// Bootstrap prepares an empty database: it creates the first administrator from
// the given credentials (only when no user exists), the singleton storage policy,
// and a default local provider. It is idempotent.
func (s *Service) Bootstrap(ctx context.Context, adminUsername, adminPassword string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
			if err := tx.Create(&model.Policy{ID: 1, Mode: model.ModeMirrorAll, MinimumReplicas: 1, UpdatedAt: s.now()}).Error; err != nil {
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
			if err := tx.Create(&row).Error; err != nil {
				return wrap("create ai settings", err)
			}
		}

		var searchSettings int64
		if err := tx.Model(&model.SearchSettings{}).Where("id = ?", 1).Count(&searchSettings).Error; err != nil {
			return wrap("count search settings", err)
		}
		if searchSettings == 0 {
			row := defaultSearchSettings()
			row.UpdatedAt = s.now()
			if err := tx.Create(&row).Error; err != nil {
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

// RecoverInterrupted returns work that was running when the process stopped to pending.
// It must run before the worker starts. Ingest work is idempotent: publication is one transaction.
func (s *Service) RecoverInterrupted(ctx context.Context) error {
	now := s.now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Job{}).Where("status = ?", model.JobRunning).
			Updates(map[string]any{"status": model.JobPending, "updated_at": now}).Error; err != nil {
			return wrap("recover jobs", err)
		}
		if err := tx.Model(&model.Replica{}).Where("status = ?", model.ReplicaUploading).
			Updates(map[string]any{"status": model.ReplicaPending, "updated_at": now}).Error; err != nil {
			return wrap("recover replicas", err)
		}
		if err := tx.Model(&model.MaterialAnalysis{}).Where("status = ?", model.AnalysisRunning).
			Updates(map[string]any{"status": model.AnalysisPending, "updated_at": now}).Error; err != nil {
			return wrap("recover analyses", err)
		}
		if err := tx.Model(&model.MaterialIndexState{}).Where("status = ?", "running").
			Updates(map[string]any{"status": "pending", "updated_at": now}).Error; err != nil {
			return wrap("recover index states", err)
		}
		return nil
	})
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
