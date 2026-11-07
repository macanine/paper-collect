package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const (
	maxOCRProviderName = 128
	maxOCRModelName    = 200
	ocrHealthTimeout   = 20 * time.Second
)

// OCRProviderView hides the access token.
type OCRProviderView struct {
	Provider model.OCRProvider
}

// OCRProviderInput creates a hosted OCR endpoint.
type OCRProviderInput struct {
	Name     string
	Kind     string
	BaseURL  string
	Token    *string
	Model    string
	Enabled  bool
	Priority int
}

// OCRProviderPatch updates an OCR endpoint. Token nil keeps; empty clears.
type OCRProviderPatch struct {
	Name     *string
	BaseURL  *string
	Token    *string
	Model    *string
	Enabled  *bool
	Priority *int
}

// OCRTestResult is the result of a PaddleOCR/MinerU health check.
type OCRTestResult struct {
	Healthy bool
	Message string
}

func defaultOCRModel(kind string) string {
	if kind == model.OCRKindPaddleOCR {
		return "PP-OCRv6"
	}
	if kind == model.OCRKindRapidOCR {
		return "PP-OCRv5"
	}
	return ""
}

func defaultOCRBaseURL(kind string) string {
	if kind == model.OCRKindPaddleOCR {
		return "https://paddleocr.aistudio-app.com"
	}
	if kind == model.OCRKindRapidOCR {
		return "http://rapidocr:9003"
	}
	return "https://mineru.net"
}

func validateOCRProvider(name, kind, baseURL, modelName string, priority int) (string, error) {
	if n := runeLen(strings.TrimSpace(name)); n < 1 || n > maxOCRProviderName {
		return "", apperr.Validation("OCR 服务商名称长度必须为 1 到 128 个字符")
	}
	if !isOneOf(kind, model.OCRKindMinerU, model.OCRKindPaddleOCR, model.OCRKindRapidOCR) {
		return "", apperr.Validation("OCR 服务商类型只能是 mineru、paddleocr 或 rapidocr")
	}
	normalized, err := ai.ValidateBaseURL(baseURL)
	if err != nil {
		return "", apperr.Validation("OCR Base URL 无效: " + err.Error())
	}
	if utf8RuneLen(modelName) > maxOCRModelName {
		return "", apperr.Validation("OCR 模型名不能超过 200 个字符")
	}
	if priority < 0 || priority > 1000 {
		return "", apperr.Validation("priority 必须在 0 到 1000 之间")
	}
	return normalized, nil
}

func utf8RuneLen(value string) int {
	return len([]rune(strings.TrimSpace(value)))
}

// ListOCRProviders returns every OCR endpoint ordered by scheduling priority.
func (s *Service) ListOCRProviders(ctx context.Context) ([]OCRProviderView, error) {
	var rows []model.OCRProvider
	if err := s.db.WithContext(ctx).Order("priority ASC").Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]OCRProviderView, 0, len(rows))
	for _, row := range rows {
		out = append(out, OCRProviderView{Provider: row})
	}
	return out, nil
}

// CreateOCRProvider stores an encrypted OCR token.
func (s *Service) CreateOCRProvider(ctx context.Context, p auth.Principal, in OCRProviderInput) (OCRProviderView, error) {
	kind := strings.TrimSpace(in.Kind)
	modelName := strings.TrimSpace(in.Model)
	if modelName == "" {
		modelName = defaultOCRModel(kind)
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = defaultOCRBaseURL(kind)
	}
	normalized, err := validateOCRProvider(in.Name, kind, baseURL, modelName, in.Priority)
	if err != nil {
		return OCRProviderView{}, err
	}
	id := newID()
	cipher := ""
	if in.Token != nil && strings.TrimSpace(*in.Token) != "" {
		cipher, err = s.box.Seal([]byte(strings.TrimSpace(*in.Token)), []byte(id))
		if err != nil {
			return OCRProviderView{}, apperr.Internal(err)
		}
	}
	now := s.now()
	row := model.OCRProvider{
		ID: id, Name: strings.TrimSpace(in.Name), Kind: kind, BaseURL: normalized,
		TokenCipher: cipher, Model: modelName, Enabled: in.Enabled, Priority: in.Priority,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "ai.ocr_provider_create", row.ID)
	}); err != nil {
		return OCRProviderView{}, apperr.Internal(err)
	}
	return OCRProviderView{Provider: row}, nil
}

// UpdateOCRProvider patches a provider; an empty token clears the saved token.
func (s *Service) UpdateOCRProvider(ctx context.Context, p auth.Principal, id string, pt OCRProviderPatch) (OCRProviderView, error) {
	db := s.db.WithContext(ctx)
	var row model.OCRProvider
	if err := db.Where("id = ?", id).First(&row).Error; err != nil {
		return OCRProviderView{}, apperr.NotFound("OCR 服务商不存在")
	}
	name, baseURL, modelName := row.Name, row.BaseURL, row.Model
	enabled, priority := row.Enabled, row.Priority
	if pt.Name != nil {
		name = *pt.Name
	}
	if pt.BaseURL != nil {
		baseURL = *pt.BaseURL
	}
	if pt.Model != nil {
		modelName = *pt.Model
	}
	if pt.Enabled != nil {
		enabled = *pt.Enabled
	}
	if pt.Priority != nil {
		priority = *pt.Priority
	}
	normalized, err := validateOCRProvider(name, row.Kind, baseURL, modelName, priority)
	if err != nil {
		return OCRProviderView{}, err
	}
	updates := map[string]any{
		"name": strings.TrimSpace(name), "base_url": normalized, "model": strings.TrimSpace(modelName),
		"enabled": enabled, "priority": priority, "updated_at": s.now(),
	}
	if pt.Token != nil {
		token := strings.TrimSpace(*pt.Token)
		if token == "" {
			updates["token_cipher"] = ""
		} else {
			cipher, err := s.box.Seal([]byte(token), []byte(row.ID))
			if err != nil {
				return OCRProviderView{}, apperr.Internal(err)
			}
			updates["token_cipher"] = cipher
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.OCRProvider{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "ai.ocr_provider_update", row.ID)
	}); err != nil {
		return OCRProviderView{}, apperr.Internal(err)
	}
	if err := db.Where("id = ?", row.ID).First(&row).Error; err != nil {
		return OCRProviderView{}, apperr.Internal(err)
	}
	return OCRProviderView{Provider: row}, nil
}

// TestOCRProvider probes the saved token without uploading a document.
func (s *Service) TestOCRProvider(ctx context.Context, id string) (OCRTestResult, error) {
	row, err := s.getOCRProvider(ctx, id)
	if err != nil {
		return OCRTestResult{}, err
	}
	cfg, err := s.ocrProviderConfig(row)
	if err != nil {
		return OCRTestResult{Healthy: false, Message: redact(ai.Sanitize(err.Error()))}, nil
	}
	client, err := jobexec.NewOCRClient(cfg)
	if err != nil {
		return OCRTestResult{Healthy: false, Message: redact(ai.Sanitize(err.Error()))}, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, ocrHealthTimeout)
	defer cancel()
	if err := client.Health(probeCtx); err != nil {
		return OCRTestResult{Healthy: false, Message: redact(ai.Sanitize(err.Error()))}, nil
	}
	if row.Kind == model.OCRKindRapidOCR {
		return OCRTestResult{Healthy: true, Message: "RapidOCR 服务可达且模型已加载"}, nil
	}
	return OCRTestResult{Healthy: true, Message: ocrProviderLabel(row.Kind) + " 服务可达且令牌被接受"}, nil
}

func ocrProviderLabel(kind string) string {
	if kind == model.OCRKindPaddleOCR {
		return "PaddleOCR"
	}
	if kind == model.OCRKindRapidOCR {
		return "RapidOCR"
	}
	return "MinerU"
}

func (s *Service) getOCRProvider(ctx context.Context, id string) (model.OCRProvider, error) {
	var row model.OCRProvider
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.OCRProvider{}, apperr.NotFound("OCR 服务商不存在")
		}
		return model.OCRProvider{}, apperr.Internal(err)
	}
	return row, nil
}

// selectedOCRProvider resolves the configured provider before falling back to
// the first enabled provider. A missing provider is a configuration error.
func (s *Service) selectedOCRProvider(ctx context.Context, settings model.AISettings) (model.OCRProvider, error) {
	db := s.db.WithContext(ctx)
	var row model.OCRProvider
	providerID := settings.OCRProviderID
	if providerID == "" {
		// Bootstrap migrates existing installations, but tests and upgraded
		// requests can reach here before the settings row is materialized.
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return s.migrateLegacyOCRProvider(tx, &settings)
		}); err != nil {
			return model.OCRProvider{}, apperr.Internal(err)
		}
		providerID = settings.OCRProviderID
	}
	if providerID != "" {
		if err := db.Where("id = ?", providerID).First(&row).Error; err == nil {
			if row.Enabled {
				return row, nil
			}
			return model.OCRProvider{}, apperr.Validation("所选 OCR 服务商已停用")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return model.OCRProvider{}, apperr.Internal(err)
		}
	}
	if err := db.Where("enabled = ?", true).Order("priority ASC").Order("created_at ASC").First(&row).Error; err == nil {
		return row, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.OCRProvider{}, apperr.Internal(err)
	}
	return model.OCRProvider{}, apperr.Validation("没有可用的 OCR 服务商，请先在 AI 解析中配置并选择")
}

// ocrProviderConfig decrypts one provider for an executor.
func (s *Service) ocrProviderConfig(row model.OCRProvider) (jobexec.OCRConfig, error) {
	if !row.Enabled {
		return jobexec.OCRConfig{}, errors.New(ocrProviderLabel(row.Kind) + " 服务商已停用")
	}
	token, err := s.openAISecret(row.TokenCipher, row.ID)
	if err != nil && row.Kind != model.OCRKindRapidOCR {
		return jobexec.OCRConfig{}, err
	}
	if row.Kind != model.OCRKindRapidOCR && strings.TrimSpace(token) == "" {
		return jobexec.OCRConfig{}, errors.New("未配置 " + ocrProviderLabel(row.Kind) + " 令牌")
	}
	return jobexec.OCRConfig{Name: row.Name, Kind: row.Kind, BaseURL: row.BaseURL, Model: row.Model, Token: token}, nil
}

// ocrConfig resolves the selected provider and the cache key of its output.
func (s *Service) ocrConfig(ctx context.Context, settings model.AISettings, mediaType string) (*jobexec.OCRConfig, string, error) {
	row, err := s.selectedOCRProvider(ctx, settings)
	if err != nil {
		return nil, "", err
	}
	cfg, err := s.ocrProviderConfig(row)
	if err != nil {
		return nil, "", err
	}
	return &cfg, textParseKey(row, mediaType), nil
}

// cachedText returns parser output stored for the current file content and
// parser, or nil when it must be parsed again.
func (s *Service) cachedText(ctx context.Context, m model.Material, key string) *jobexec.ParsedText {
	var cached model.MaterialText
	if err := s.db.WithContext(ctx).Where("material_id = ?", m.ID).First(&cached).Error; err != nil {
		return nil
	}
	if cached.SourceHash != m.SHA256 || cached.ParseKey != key {
		return nil
	}
	out := &jobexec.ParsedText{ParseKey: key, Markdown: cached.Markdown, PageCount: cached.PageCount}
	_ = json.Unmarshal([]byte(cached.PagesJSON), &out.Pages)
	return out
}

// saveParsedText caches parser output reported by a node. A failed write only
// means the next job parses again.
func (s *Service) saveParsedText(ctx context.Context, materialID string, p *jobexec.ParsedText) {
	var m model.Material
	if err := s.db.WithContext(ctx).Select("id", "sha256").Where("id = ?", materialID).First(&m).Error; err != nil {
		return
	}
	pages, _ := json.Marshal(p.Pages)
	now := s.now()
	if err := s.db.WithContext(ctx).Save(&model.MaterialText{
		MaterialID: m.ID, SourceHash: m.SHA256, ParseKey: p.ParseKey,
		PageCount: p.PageCount, PagesJSON: string(pages), Markdown: p.Markdown, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		s.logf("parsed text cache unavailable")
	}
}

// migrateLegacyOCRProvider creates one MinerU provider from the legacy
// singleton settings. It is idempotent and leaves the old fields intact.
func (s *Service) migrateLegacyOCRProvider(tx *gorm.DB, settings *model.AISettings) error {
	var count int64
	if err := tx.Model(&model.OCRProvider{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		if settings.OCRProviderID != "" {
			return nil
		}
		// A disabled but explicitly configured provider must remain selected;
		// resolution will report that it is disabled instead of silently
		// switching to a different provider.
		var first model.OCRProvider
		err := tx.Where("enabled = ?", true).Order("priority ASC").Order("created_at ASC").First(&first).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = tx.Order("priority ASC").Order("created_at ASC").First(&first).Error
		}
		if err == nil {
			if err := tx.Model(&model.AISettings{}).Where("id = ?", 1).
				Update("ocr_provider_id", first.ID).Error; err != nil {
				return err
			}
			settings.OCRProviderID = first.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	}
	if settings.MinerUTokenCipher == "" {
		return nil
	}
	token, err := s.box.Open(settings.MinerUTokenCipher, []byte("ai.settings"))
	if err != nil {
		return errors.New("无法解密待迁移的 MinerU 令牌")
	}
	id := newID()
	cipher, err := s.box.Seal(token, []byte(id))
	if err != nil {
		return err
	}
	now := s.now()
	row := model.OCRProvider{
		ID: id, Name: "MinerU（迁移）", Kind: model.OCRKindMinerU,
		BaseURL: settings.MinerUBaseURL, TokenCipher: cipher,
		Model: "", Enabled: true, Priority: 100, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.AISettings{}).Where("id = ?", 1).
		Update("ocr_provider_id", row.ID).Error; err != nil {
		return err
	}
	settings.OCRProviderID = row.ID
	return nil
}
