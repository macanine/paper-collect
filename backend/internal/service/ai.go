package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const (
	maxAIProviderName    = 128
	maxModelNameRunes    = 200
	maxModelsPerProvider = 200
)

// AIProviderView is the safe projection of a provider; the key is never returned.
type AIProviderView struct {
	Provider model.AIProvider
	Models   []string
}

// AIProviderInput creates a provider.
type AIProviderInput struct {
	Name         string
	BaseURL      string
	APIKey       *string
	Models       []string
	DefaultModel string
	VisionModel  string
	Enabled      bool
	Priority     int
}

// AIProviderPatch updates a provider. Nil leaves a field unchanged; an empty
// APIKey string clears the stored key.
type AIProviderPatch struct {
	Name         *string
	BaseURL      *string
	APIKey       *string
	Models       *[]string
	DefaultModel *string
	VisionModel  *string
	Enabled      *bool
	Priority     *int
}

// ProbeInput probes an endpoint either with a supplied key or a saved provider.
type ProbeInput struct {
	BaseURL    string
	APIKey     *string
	ProviderID string
}

// ProbeResult is a model list from an OpenAI-compatible endpoint.
type ProbeResult struct {
	Models []string
	Count  int
}

// AITestResult reports one minimal chat attempt.
type AITestResult struct {
	Healthy bool
	Model   string
	Message string
}

// AISettingsView hides OCR provider tokens.
type AISettingsView struct {
	Settings      model.AISettings
	OCRConfigured bool
}

// AISettingsInput is a full settings replacement.
type AISettingsInput struct {
	OCRProviderID *string
	MaxPages      int
	MaxImages     int
}

func (s *Service) openAISecret(cipher, aad string) (string, error) {
	if cipher == "" {
		return "", nil
	}
	raw, err := s.box.Open(cipher, []byte(aad))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, raw := range models {
		name := strings.TrimSpace(raw)
		if name == "" || len([]rune(name)) > maxModelNameRunes || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= maxModelsPerProvider {
			break
		}
	}
	return out
}

func validateAIProvider(baseURL, name, defaultModel, visionModel string, priority int) (string, error) {
	if n := runeLen(strings.TrimSpace(name)); n < 1 || n > maxAIProviderName {
		return "", apperr.Validation("模型名称长度必须为 1 到 128 个字符")
	}
	normalized, err := ai.ValidateBaseURL(baseURL)
	if err != nil {
		return "", apperr.Validation("Base URL 无效: " + err.Error())
	}
	if utf8.RuneCountInString(defaultModel) > maxModelNameRunes || utf8.RuneCountInString(visionModel) > maxModelNameRunes {
		return "", apperr.Validation("模型名不能超过 200 个字符")
	}
	if priority < 0 || priority > 1000 {
		return "", apperr.Validation("priority 必须在 0 到 1000 之间")
	}
	return normalized, nil
}

// ListAIProviders returns providers ordered by scheduling priority.
func (s *Service) ListAIProviders(ctx context.Context) ([]AIProviderView, error) {
	var rows []model.AIProvider
	if err := s.db.WithContext(ctx).Order("priority ASC").Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]AIProviderView, 0, len(rows))
	for _, row := range rows {
		out = append(out, AIProviderView{Provider: row, Models: model.DecodeStrings(row.ModelsJSON)})
	}
	return out, nil
}

// CreateAIProvider stores an OpenAI-compatible endpoint with an encrypted key.
func (s *Service) CreateAIProvider(ctx context.Context, p auth.Principal, in AIProviderInput) (AIProviderView, error) {
	baseURL, err := validateAIProvider(in.BaseURL, in.Name, in.DefaultModel, in.VisionModel, in.Priority)
	if err != nil {
		return AIProviderView{}, err
	}
	id := newID()
	cipher := ""
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		cipher, err = s.box.Seal([]byte(strings.TrimSpace(*in.APIKey)), []byte(id))
		if err != nil {
			return AIProviderView{}, apperr.Internal(err)
		}
	}
	now := s.now()
	row := model.AIProvider{
		ID: id, Name: strings.TrimSpace(in.Name), BaseURL: baseURL, APIKeyCipher: cipher,
		ModelsJSON:   model.EncodeStrings(normalizeModels(in.Models)),
		DefaultModel: strings.TrimSpace(in.DefaultModel), VisionModel: strings.TrimSpace(in.VisionModel),
		Enabled: in.Enabled, Priority: in.Priority, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "ai.provider_create", row.ID)
	}); err != nil {
		return AIProviderView{}, apperr.Internal(err)
	}
	s.wakeWaitingAI(ctx)
	return AIProviderView{Provider: row, Models: model.DecodeStrings(row.ModelsJSON)}, nil
}

// UpdateAIProvider patches a provider. Secrets are whole-value: omitted keeps, empty clears.
func (s *Service) UpdateAIProvider(ctx context.Context, p auth.Principal, id string, pt AIProviderPatch) (AIProviderView, error) {
	db := s.db.WithContext(ctx)
	var row model.AIProvider
	if err := db.Where("id = ?", id).First(&row).Error; err != nil {
		return AIProviderView{}, apperr.NotFound("模型端点不存在")
	}
	name, baseURL := row.Name, row.BaseURL
	defaultModel, visionModel := row.DefaultModel, row.VisionModel
	enabled, priority := row.Enabled, row.Priority
	models := model.DecodeStrings(row.ModelsJSON)
	if pt.Name != nil {
		name = *pt.Name
	}
	if pt.BaseURL != nil {
		baseURL = *pt.BaseURL
	}
	if pt.DefaultModel != nil {
		defaultModel = *pt.DefaultModel
	}
	if pt.VisionModel != nil {
		visionModel = *pt.VisionModel
	}
	if pt.Enabled != nil {
		enabled = *pt.Enabled
	}
	if pt.Priority != nil {
		priority = *pt.Priority
	}
	if pt.Models != nil {
		models = normalizeModels(*pt.Models)
	}
	normalized, err := validateAIProvider(baseURL, name, defaultModel, visionModel, priority)
	if err != nil {
		return AIProviderView{}, err
	}
	updates := map[string]any{
		"name": strings.TrimSpace(name), "base_url": normalized,
		"default_model": strings.TrimSpace(defaultModel), "vision_model": strings.TrimSpace(visionModel),
		"models_json": model.EncodeStrings(models), "enabled": enabled, "priority": priority,
		"updated_at": s.now(),
	}
	if pt.APIKey != nil {
		key := strings.TrimSpace(*pt.APIKey)
		if key == "" {
			updates["api_key_cipher"] = ""
		} else {
			cipher, err := s.box.Seal([]byte(key), []byte(row.ID))
			if err != nil {
				return AIProviderView{}, apperr.Internal(err)
			}
			updates["api_key_cipher"] = cipher
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AIProvider{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "ai.provider_update", row.ID)
	}); err != nil {
		return AIProviderView{}, apperr.Internal(err)
	}
	if err := db.Where("id = ?", row.ID).First(&row).Error; err != nil {
		return AIProviderView{}, apperr.Internal(err)
	}
	s.wakeWaitingAI(ctx)
	return AIProviderView{Provider: row, Models: model.DecodeStrings(row.ModelsJSON)}, nil
}

// ProbeAIModels probes /models using either the supplied key or a saved provider key.
func (s *Service) ProbeAIModels(ctx context.Context, in ProbeInput) (ProbeResult, error) {
	baseURL := strings.TrimSpace(in.BaseURL)
	apiKey := ""
	if in.ProviderID != "" {
		var row model.AIProvider
		if err := s.db.WithContext(ctx).Where("id = ?", in.ProviderID).First(&row).Error; err != nil {
			return ProbeResult{}, apperr.NotFound("模型端点不存在")
		}
		if baseURL == "" {
			baseURL = row.BaseURL
		}
		key, err := s.openAISecret(row.APIKeyCipher, row.ID)
		if err != nil {
			return ProbeResult{}, apperr.Internal(err)
		}
		apiKey = key
	}
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		apiKey = strings.TrimSpace(*in.APIKey)
	}
	normalized, err := ai.ValidateBaseURL(baseURL)
	if err != nil {
		return ProbeResult{}, apperr.Validation("Base URL 无效: " + err.Error())
	}
	client, err := ai.NewOpenAI(normalized, apiKey, nil)
	if err != nil {
		return ProbeResult{}, apperr.Validation(err.Error())
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	models, err := client.Models(probeCtx)
	if err != nil {
		return ProbeResult{}, upstreamError(err)
	}
	return ProbeResult{Models: models, Count: len(models)}, nil
}

// TestAIProvider performs one minimal chat call and never fakes success.
func (s *Service) TestAIProvider(ctx context.Context, id, selectedModel string) (AITestResult, error) {
	var row model.AIProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return AITestResult{}, apperr.NotFound("模型端点不存在")
	}
	modelName := strings.TrimSpace(selectedModel)
	if modelName == "" {
		modelName = row.DefaultModel
	}
	if modelName == "" {
		return AITestResult{Healthy: false, Model: "", Message: "未选择模型"}, nil
	}
	key, err := s.openAISecret(row.APIKeyCipher, row.ID)
	if err != nil {
		return AITestResult{}, apperr.Internal(err)
	}
	client, err := ai.NewOpenAI(row.BaseURL, key, nil)
	if err != nil {
		return AITestResult{Healthy: false, Message: ai.Sanitize(err.Error())}, nil
	}
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = client.Chat(testCtx, modelName, []ai.ChatMessage{
		ai.TextMessage("system", "你是连通性测试。"),
		ai.TextMessage("user", "只回复 OK"),
	})
	if err != nil {
		return AITestResult{Healthy: false, Model: modelName, Message: ai.Sanitize(err.Error())}, nil
	}
	return AITestResult{Healthy: true, Model: modelName, Message: "模型调用成功"}, nil
}

func upstreamError(err error) error {
	message := ai.Sanitize(err.Error())
	return apperr.New(502, "upstream_error", message)
}

func defaultAISettings() model.AISettings {
	return model.AISettings{ID: 1, MaxPages: 3, MaxImages: 2}
}

// ensureAISettings returns the singleton row, creating defaults when absent.
func (s *Service) ensureAISettings(ctx context.Context) (model.AISettings, error) {
	db := s.db.WithContext(ctx)
	var row model.AISettings
	err := db.Where("id = ?", 1).First(&row).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AISettings{}, apperr.Internal(err)
	}
	row = defaultAISettings()
	row.UpdatedAt = s.now()
	if err := db.Create(&row).Error; err != nil {
		// Another request may have created it concurrently; re-read.
		if err := db.Where("id = ?", 1).First(&row).Error; err != nil {
			return model.AISettings{}, apperr.Internal(err)
		}
	}
	return row, nil
}

// GetAISettings returns settings with provider credentials hidden.
func (s *Service) GetAISettings(ctx context.Context) (AISettingsView, error) {
	row, err := s.ensureAISettings(ctx)
	if err != nil {
		return AISettingsView{}, err
	}
	view := AISettingsView{Settings: row}
	if row.OCRProviderID != "" {
		var provider model.OCRProvider
		if err := s.db.WithContext(ctx).Where("id = ?", row.OCRProviderID).First(&provider).Error; err == nil {
			view.OCRConfigured = provider.TokenCipher != ""
		}
	}
	return view, nil
}

// PutAISettings replaces the complete analysis settings object.
func (s *Service) PutAISettings(ctx context.Context, p auth.Principal, in AISettingsInput) (AISettingsView, error) {
	if in.MaxPages < 1 || in.MaxPages > 10 {
		return AISettingsView{}, apperr.Validation("max_pages 必须在 1 到 10 之间")
	}
	if in.MaxImages < 0 || in.MaxImages > 4 {
		return AISettingsView{}, apperr.Validation("max_images 必须在 0 到 4 之间")
	}
	current, err := s.ensureAISettings(ctx)
	if err != nil {
		return AISettingsView{}, err
	}
	providerID := current.OCRProviderID
	if in.OCRProviderID != nil {
		providerID = strings.TrimSpace(*in.OCRProviderID)
	}
	if providerID != "" {
		var selected model.OCRProvider
		if err := s.db.WithContext(ctx).Where("id = ?", providerID).First(&selected).Error; err != nil {
			return AISettingsView{}, apperr.Validation("所选 OCR 服务商不存在")
		}
		if !selected.Enabled {
			return AISettingsView{}, apperr.Validation("所选 OCR 服务商已停用")
		}
	}
	updates := map[string]any{
		"ocr_provider_id": providerID, "max_pages": in.MaxPages, "max_images": in.MaxImages, "updated_at": s.now(),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AISettings{}).Where("id = ?", 1).Updates(updates).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "ai.settings_update", "1")
	}); err != nil {
		s.logf("put ai settings: %v", err)
		return AISettingsView{}, apperr.Internal(err)
	}
	s.wakeWaitingAI(ctx)
	return s.GetAISettings(ctx)
}
func excerptRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit])
}

func (s *Service) providerName(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	var row model.AIProvider
	if err := s.db.WithContext(ctx).Select("name").Where("id = ?", id).First(&row).Error; err != nil {
		return ""
	}
	return row.Name
}

// analyzable reports whether a file's content can be parsed. Other files
// (audio, spreadsheets) are still read by the model, from name and path only.
func analyzable(mediaType string) bool {
	switch mediaType {
	case blob.MediaTypes[blob.KindPDF], blob.MediaTypes[blob.KindDOC], blob.MediaTypes[blob.KindDOCX],
		blob.MediaTypes[blob.KindPPT], blob.MediaTypes[blob.KindPPTX],
		blob.MediaTypes[blob.KindJPEG], blob.MediaTypes[blob.KindPNG], blob.MediaTypes[blob.KindWEBP]:
		return true
	}
	return false
}

func (s *Service) loadAISettingsTx(tx *gorm.DB) (model.AISettings, error) {
	var settings model.AISettings
	if err := tx.Where("id = ?", 1).First(&settings).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultAISettings(), nil
		}
		return settings, err
	}
	return settings, nil
}

// aiModels decrypts the enabled model endpoints in priority order. Every
// AI step uses this same list, so one set is always decided the same way.
func (s *Service) aiModels(ctx context.Context) ([]jobexec.ModelConfig, error) {
	var providers []model.AIProvider
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("priority ASC").Order("created_at ASC").Find(&providers).Error; err != nil {
		return nil, err
	}
	out := make([]jobexec.ModelConfig, 0, len(providers))
	for _, p := range providers {
		key, err := s.openAISecret(p.APIKeyCipher, p.ID)
		if err != nil {
			s.logf("ai provider %s key unavailable", p.Name)
			continue
		}
		out = append(out, jobexec.ModelConfig{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: key, Model: p.DefaultModel, VisionModel: p.VisionModel})
	}
	return out, nil
}
