package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

func decodeScopes(text string) []string { return model.DecodeStrings(text) }

// ListUsers returns users, newest first. Requires users:manage.
func (s *Service) ListUsers(ctx context.Context, pg PageQuery) (Page[model.User], error) {
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Model(&model.User{}).Count(&total).Error; err != nil {
		return Page[model.User]{}, wrap("count users", err)
	}
	items := []model.User{}
	if err := db.Model(&model.User{}).Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error; err != nil {
		return Page[model.User]{}, wrap("list users", err)
	}
	return newPage(items, total, pg), nil
}

// CreateUser adds an account. Only administrators can call it (checked by the caller).
func (s *Service) CreateUser(ctx context.Context, p auth.Principal, username, password, role string) (model.User, error) {
	if p.IsAgent() {
		return model.User{}, apperr.Forbidden("代理密钥不能创建用户")
	}
	if !usernamePattern.MatchString(username) {
		return model.User{}, apperr.Validation("用户名只允许 3-64 位字母、数字、下划线或连字符")
	}
	if err := validatePassword(password); err != nil {
		return model.User{}, err
	}
	if !isOneOf(role, model.RoleAdmin, model.RoleEditor, model.RoleViewer) {
		return model.User{}, apperr.Validation("角色只能是 admin、editor 或 viewer")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return model.User{}, apperr.Internal(err)
	}
	now := s.now()
	user := model.User{
		ID:           newID(),
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	db := s.db.WithContext(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "user.create", user.ID)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return model.User{}, apperr.Conflict("用户名已存在")
		}
		return model.User{}, apperr.Internal(err)
	}
	return user, nil
}

// ListKeys returns API keys (secrets are never stored in plain text). Requires keys:manage.
func (s *Service) ListKeys(ctx context.Context, pg PageQuery) (Page[model.APIKey], error) {
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Model(&model.APIKey{}).Count(&total).Error; err != nil {
		return Page[model.APIKey]{}, wrap("count keys", err)
	}
	items := []model.APIKey{}
	if err := db.Model(&model.APIKey{}).Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error; err != nil {
		return Page[model.APIKey]{}, wrap("list keys", err)
	}
	return newPage(items, total, pg), nil
}

// CreateKey issues an API key. The full secret is returned exactly once and only its hash is stored.
// Scopes must be known, must belong to the creator's role, and may never include keys:manage or users:manage.
func (s *Service) CreateKey(ctx context.Context, p auth.Principal, name string, scopes []string, expiresAt *time.Time) (model.APIKey, string, error) {
	if p.IsAgent() {
		return model.APIKey{}, "", apperr.Forbidden("代理密钥不能创建或升级密钥")
	}
	trimmed := strings.TrimSpace(name)
	if n := runeLen(trimmed); n < 1 || n > 128 {
		return model.APIKey{}, "", apperr.Validation("密钥名称长度必须为 1 到 128 个字符")
	}
	if len(scopes) == 0 {
		return model.APIKey{}, "", apperr.Validation("至少需要一个 scope")
	}
	allowed := map[string]bool{}
	for _, sc := range auth.RoleScopes(p.Role) {
		allowed[sc] = true
	}
	seen := map[string]bool{}
	for _, sc := range scopes {
		if !auth.IsKnownScope(sc) {
			return model.APIKey{}, "", apperr.Validation("存在未知 scope")
		}
		if auth.IsForbiddenForKeys(sc) {
			return model.APIKey{}, "", apperr.Validation("API Key 不能包含 keys:manage 或 users:manage")
		}
		if !allowed[sc] {
			return model.APIKey{}, "", apperr.Validation("scope 必须是创建者权限的子集")
		}
		if seen[sc] {
			return model.APIKey{}, "", apperr.Validation("scope 不能重复")
		}
		seen[sc] = true
	}
	now := s.now()
	if expiresAt != nil && !expiresAt.After(now) {
		return model.APIKey{}, "", apperr.Validation("过期时间必须晚于当前时间")
	}
	id, full, hash, err := auth.NewAPIKey()
	if err != nil {
		return model.APIKey{}, "", apperr.Internal(err)
	}
	key := model.APIKey{
		ID:         id,
		UserID:     p.UserID,
		Name:       trimmed,
		SecretHash: hash,
		ScopesJSON: model.EncodeStrings(scopes),
		ExpiresAt:  expiresAt,
		Revoked:    false,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&key).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "api_key.create", key.ID)
	})
	if err != nil {
		return model.APIKey{}, "", apperr.Internal(err)
	}
	return key, full, nil
}

// RevokeKey revokes an API key immediately. Revocation is idempotent.
func (s *Service) RevokeKey(ctx context.Context, p auth.Principal, id string) error {
	if p.IsAgent() {
		return apperr.Forbidden("代理密钥不能撤销密钥")
	}
	db := s.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		var key model.APIKey
		if err := tx.Where("id = ?", id).First(&key).Error; err != nil {
			return apperr.NotFound("API Key 不存在")
		}
		if key.Revoked {
			return nil
		}
		now := s.now()
		if err := tx.Model(&model.APIKey{}).Where("id = ?", id).
			Updates(map[string]any{"revoked": true, "revoked_at": now, "updated_at": now}).Error; err != nil {
			return apperr.Internal(err)
		}
		return s.audit(tx, p, "api_key.revoke", id)
	})
}

func validatePassword(password string) error {
	if len(password) > auth.MaxPasswordBytes {
		return apperr.Validation("口令过长")
	}
	if runeLen(password) < auth.MinPasswordLength {
		return apperr.Validation("口令至少 12 个字符")
	}
	return nil
}

func isOneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}
