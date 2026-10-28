package service

import (
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// audit appends one metadata-only row. Callers pass identifiers only, never bodies,
// tokens, cookies, share URLs or passwords. Agent actions are marked with an "agent:" prefix.
func (s *Service) audit(tx *gorm.DB, p auth.Principal, action, targetID string) error {
	if p.IsAgent() {
		action = "agent:" + action
	}
	row := model.Audit{
		ID:        newID(),
		UserID:    p.UserID,
		Action:    action,
		TargetID:  targetID,
		CreatedAt: s.now(),
	}
	return wrap("audit", tx.Create(&row).Error)
}

// auditSystem records work performed by the background worker (no user).
func (s *Service) auditSystem(tx *gorm.DB, action, targetID string) error {
	return s.audit(tx, auth.Principal{}, action, targetID)
}

// ListAudit returns audit metadata, newest first. Requires audit:read.
func (s *Service) ListAudit(pg PageQuery) (Page[model.Audit], error) {
	var total int64
	if err := s.db.Model(&model.Audit{}).Count(&total).Error; err != nil {
		return Page[model.Audit]{}, wrap("count audit", err)
	}
	items := []model.Audit{}
	err := s.db.Model(&model.Audit{}).Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error
	if err != nil {
		return Page[model.Audit]{}, wrap("list audit", err)
	}
	return newPage(items, total, pg), nil
}

func newPage[T any](items []T, total int64, pg PageQuery) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Total: total, Page: pg.Page, PageSize: pg.PageSize}
}
