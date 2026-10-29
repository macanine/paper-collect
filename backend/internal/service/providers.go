package service

import (
	"context"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

const (
	maxProviderNameRunes  = 128
	maxMinimumReplicas    = 20
	providerHealthTimeout = 30 * time.Second
)

// ProviderInput is the validated body of a provider create request.
type ProviderInput struct {
	Name     string
	Kind     string
	Role     string
	Enabled  bool
	Priority int
	Weight   int
	Config   map[string]string
	Secret   map[string]string
}

// ProviderPatch is a partial provider update. Kind is immutable.
type ProviderPatch struct {
	Name     *string
	Role     *string
	Enabled  *bool
	Priority *int
	Weight   *int
	Config   *map[string]string
	Secret   *map[string]string
}

// ProviderTest is the result of an explicit adapter health check.
type ProviderTest struct {
	Healthy      bool
	Message      string
	Capabilities storage.Capabilities
}

// PolicyInput is the complete storage policy.
type PolicyInput struct {
	Mode            string
	MinimumReplicas int
}

var providerConfigKeys = map[string]map[string]bool{
	model.KindLocal:    {},
	model.KindWebDAV:   {"base_url": true, "path_prefix": true, "public_base_url": true},
	model.KindLanzou:   {"base_url": true, "folder_id": true, "verify_remote_hash": true},
	model.KindPan123:   {"base_url": true, "parent_file_id": true},
	model.KindS3:       {"endpoint": true, "region": true, "bucket": true, "path_prefix": true, "path_style": true, "share_expires_seconds": true},
	model.KindOneDrive: {"auth_mode": true, "client_id": true, "tenant": true, "path_prefix": true, "share_scope": true},
}

var providerSecretKeys = map[string]map[string]bool{
	model.KindLocal:    {},
	model.KindWebDAV:   {"username": true, "password": true},
	model.KindLanzou:   {"cookie": true},
	model.KindPan123:   {"client_id": true, "client_secret": true},
	model.KindS3:       {"access_key_id": true, "secret_access_key": true, "session_token": true},
	model.KindOneDrive: {"client_secret": true, "refresh_token": true, "share_url": true},
}

// ListProviders returns every endpoint, including disabled ones, by read priority.
func (s *Service) ListProviders(ctx context.Context) ([]model.StorageProvider, error) {
	items := []model.StorageProvider{}
	if err := s.db.WithContext(ctx).Order("priority ASC").Order("created_at ASC").Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	return items, nil
}

// CreateProvider stores a new endpoint and verifies its adapter configuration.
func (s *Service) CreateProvider(ctx context.Context, p auth.Principal, in ProviderInput) (model.StorageProvider, error) {
	if err := validateProviderCommon(in.Name, in.Kind, in.Role, in.Priority, in.Weight); err != nil {
		return model.StorageProvider{}, err
	}
	if err := validateProviderMaps(in.Kind, in.Config, in.Secret); err != nil {
		return model.StorageProvider{}, err
	}
	id := newID()
	if _, err := s.buildProviderAdapter(in.Kind, id, in.Config, in.Secret); err != nil {
		return model.StorageProvider{}, apperr.Validation("存储端点配置无效: " + storageErrorText(err))
	}
	cipher, err := s.sealSecret(id, in.Secret)
	if err != nil {
		return model.StorageProvider{}, apperr.Internal(err)
	}
	now := s.now()
	provider := model.StorageProvider{
		ID:           id,
		Name:         strings.TrimSpace(in.Name),
		Kind:         in.Kind,
		Role:         in.Role,
		Enabled:      in.Enabled,
		Priority:     in.Priority,
		Weight:       in.Weight,
		ConfigJSON:   model.EncodeMap(in.Config),
		SecretCipher: cipher,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&provider).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "storage.provider_create", provider.ID)
	})
	if err != nil {
		return model.StorageProvider{}, apperr.Internal(err)
	}
	return provider, nil
}

// UpdateProvider replaces the supplied fields. Config and secret are whole-object
// replacements when present; an empty secret object clears it. Kind cannot change.
func (s *Service) UpdateProvider(ctx context.Context, p auth.Principal, id string, pt ProviderPatch) (model.StorageProvider, error) {
	db := s.db.WithContext(ctx)
	var provider model.StorageProvider
	if err := db.Where("id = ?", id).First(&provider).Error; err != nil {
		return model.StorageProvider{}, apperr.NotFound("存储端点不存在")
	}

	name := provider.Name
	role := provider.Role
	enabled := provider.Enabled
	priority := provider.Priority
	weight := provider.Weight
	if pt.Name != nil {
		name = *pt.Name
	}
	if pt.Role != nil {
		role = *pt.Role
	}
	if pt.Enabled != nil {
		enabled = *pt.Enabled
	}
	if pt.Priority != nil {
		priority = *pt.Priority
	}
	if pt.Weight != nil {
		weight = *pt.Weight
	}
	if err := validateProviderCommon(name, provider.Kind, role, priority, weight); err != nil {
		return model.StorageProvider{}, err
	}

	cfg := model.DecodeMap(provider.ConfigJSON)
	oldAuthMode := cfg["auth_mode"]
	if oldAuthMode == "" {
		oldAuthMode = "direct"
	}
	if pt.Config != nil {
		cfg = *pt.Config
	}
	newAuthMode := cfg["auth_mode"]
	if newAuthMode == "" {
		newAuthMode = "direct"
	}
	if provider.Kind == model.KindOneDrive && oldAuthMode != newAuthMode && pt.Secret == nil {
		return model.StorageProvider{}, apperr.Validation("切换 OneDrive 授权方式时，请填写对应的刷新令牌")
	}
	secret := map[string]string{}
	if provider.SecretCipher != "" {
		raw, err := s.box.Open(provider.SecretCipher, []byte(provider.ID))
		if err != nil {
			return model.StorageProvider{}, apperr.Internal(err)
		}
		secret = model.DecodeMap(string(raw))
	}
	if provider.Kind == model.KindOneDrive && oldAuthMode != newAuthMode && pt.Secret != nil && (*pt.Secret)["refresh_token"] == secret["refresh_token"] {
		return model.StorageProvider{}, apperr.Validation("切换 OneDrive 授权方式时，请填写新应用对应的刷新令牌")
	}
	if pt.Secret != nil {
		secret = *pt.Secret
	}
	if err := validateProviderMaps(provider.Kind, cfg, secret); err != nil {
		return model.StorageProvider{}, err
	}
	if _, err := s.buildProviderAdapter(provider.Kind, provider.ID, cfg, secret); err != nil {
		return model.StorageProvider{}, apperr.Validation("存储端点配置无效: " + storageErrorText(err))
	}

	updates := map[string]any{
		"name":        strings.TrimSpace(name),
		"role":        role,
		"enabled":     enabled,
		"priority":    priority,
		"weight":      weight,
		"config_json": model.EncodeMap(cfg),
		"updated_at":  s.now(),
	}
	if pt.Secret != nil {
		cipher, err := s.sealSecret(provider.ID, secret)
		if err != nil {
			return model.StorageProvider{}, apperr.Internal(err)
		}
		updates["secret_cipher"] = cipher
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.StorageProvider{}).Where("id = ?", provider.ID).Updates(updates).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "storage.provider_update", provider.ID)
	})
	if err != nil {
		return model.StorageProvider{}, apperr.Internal(err)
	}
	if err := db.Where("id = ?", provider.ID).First(&provider).Error; err != nil {
		return model.StorageProvider{}, apperr.Internal(err)
	}
	return provider, nil
}

// TestProvider performs a real adapter health check. A failed connection is a
// successful API response with healthy=false, never a fabricated success.
func (s *Service) TestProvider(ctx context.Context, id string) (ProviderTest, error) {
	var provider model.StorageProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&provider).Error; err != nil {
		return ProviderTest{}, apperr.NotFound("存储端点不存在")
	}
	adapter, err := s.adapterFor(provider)
	if err != nil {
		return ProviderTest{Healthy: false, Message: storageErrorText(err), Capabilities: storage.Capabilities{}}, nil
	}
	caps := adapter.Capabilities()
	healthCtx, cancel := context.WithTimeout(ctx, providerHealthTimeout)
	defer cancel()
	if err := adapter.Health(healthCtx); err != nil {
		return ProviderTest{Healthy: false, Message: storageErrorText(err), Capabilities: caps}, nil
	}
	return ProviderTest{Healthy: true, Message: "连接成功", Capabilities: caps}, nil
}

// GetPolicy returns the singleton storage policy.
func (s *Service) GetPolicy(ctx context.Context) (model.Policy, error) {
	pol, err := s.loadPolicy(s.db.WithContext(ctx))
	if err != nil {
		return model.Policy{}, apperr.Internal(err)
	}
	return pol, nil
}

// PutPolicy replaces the singleton policy. It only affects future planning;
// Reconcile applies it to existing blobs.
func (s *Service) PutPolicy(ctx context.Context, p auth.Principal, in PolicyInput) (model.Policy, error) {
	if !isOneOf(in.Mode, model.PolicyModes...) {
		return model.Policy{}, apperr.Validation("存储策略模式只能是 mirror_all、primary_backup 或 balanced")
	}
	if in.MinimumReplicas < 1 || in.MinimumReplicas > maxMinimumReplicas {
		return model.Policy{}, apperr.Validation("minimum_replicas 必须在 1 到 20 之间")
	}
	now := s.now()
	pol := model.Policy{ID: 1, Mode: in.Mode, MinimumReplicas: in.MinimumReplicas, UpdatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", 1).Assign(map[string]any{
			"mode": in.Mode, "minimum_replicas": in.MinimumReplicas, "updated_at": now,
		}).FirstOrCreate(&model.Policy{}).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "storage.policy_update", "1")
	})
	if err != nil {
		return model.Policy{}, apperr.Internal(err)
	}
	return pol, nil
}

func validateProviderCommon(name, kind, role string, priority, weight int) error {
	if n := runeLen(strings.TrimSpace(name)); n < 1 || n > maxProviderNameRunes {
		return apperr.Validation("存储端点名称长度必须为 1 到 128 个字符")
	}
	if !isOneOf(kind, model.StorageKinds...) {
		return apperr.Validation("不支持该存储端点类型")
	}
	if !isOneOf(role, model.RolePrimary, model.RoleBackup) {
		return apperr.Validation("存储端点角色只能是 primary 或 backup")
	}
	if priority < 0 || priority > 1000 {
		return apperr.Validation("priority 必须在 0 到 1000 之间")
	}
	if weight < 1 || weight > 1000 {
		return apperr.Validation("weight 必须在 1 到 1000 之间")
	}
	return nil
}

func validateProviderMaps(kind string, config, secret map[string]string) error {
	for key := range config {
		if !providerConfigKeys[kind][key] {
			return apperr.Validation("存储配置包含该类型不支持的字段: " + key)
		}
	}
	for key := range secret {
		if !providerSecretKeys[kind][key] {
			return apperr.Validation("存储密钥包含该类型不支持的字段: " + key)
		}
	}
	return nil
}

func (s *Service) sealSecret(providerID string, secret map[string]string) (string, error) {
	if len(secret) == 0 {
		return "", nil
	}
	return s.box.Seal([]byte(model.EncodeMap(secret)), []byte(providerID))
}

// buildProviderAdapter constructs an adapter for validation or execution. Local
// roots are fixed by the server and can never come from the API.
func (s *Service) buildProviderAdapter(kind, id string, config, secret map[string]string) (storage.Adapter, error) {
	settings := map[string]string{}
	for k, v := range config {
		settings[k] = v
	}
	if kind == model.KindLocal {
		settings["root"] = s.localRoot(id)
	}
	return s.factory(storage.Config{ID: id, Kind: kind, Settings: settings, Secrets: secret})
}
