package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	"gorm.io/gorm/clause"
)

const (
	maxAIProviderName    = 128
	maxModelNameRunes    = 200
	maxModelsPerProvider = 200
	analysisExcerptRunes = 4000
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
	AnalysisEnabled bool
	OCRProviderID   *string
	MaxPages        int
	MaxImages       int
	// AutoAcceptConfidence nil keeps the stored threshold.
	AutoAcceptConfidence *float64
}

// Suggestions is the model output whitelist. Empty fields are omitted from JSON.
// Pack-level fields describe the whole set; Part and AnswerStartPage describe
// this file. Uncertain lists the fields the model was unsure about.
type Suggestions struct {
	ExamName        string   `json:"exam_name,omitempty"`
	DocType         string   `json:"doc_type,omitempty"`
	SubjectCodes    []string `json:"subject_codes,omitempty"`
	Grade           string   `json:"grade,omitempty"`
	Term            string   `json:"term,omitempty"`
	Cohort          int      `json:"cohort,omitempty"`
	Region          string   `json:"region,omitempty"`
	Organization    string   `json:"organization,omitempty"`
	ExamDate        string   `json:"exam_date,omitempty"`
	Chapter         string   `json:"chapter,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Part            string   `json:"part,omitempty"`
	AnswerStartPage int      `json:"answer_start_page,omitempty"`
	Uncertain       []string `json:"uncertain,omitempty"`
}

// AnalysisView is the API projection of one material analysis.
type AnalysisView struct {
	Analysis        model.MaterialAnalysis
	ProviderName    string
	Suggestions     Suggestions
	MarkdownExcerpt string
	ImageCount      int
	Confidence      float64
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
	return model.AISettings{ID: 1, AnalysisEnabled: false, MaxPages: 3, MaxImages: 2, AutoAcceptConfidence: 0.8}
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
		"analysis_enabled": in.AnalysisEnabled, "ocr_provider_id": providerID,
		"max_pages": in.MaxPages, "max_images": in.MaxImages, "updated_at": s.now(),
	}
	if in.AutoAcceptConfidence != nil {
		v := *in.AutoAcceptConfidence
		if v < 0 || v > 1 {
			return AISettingsView{}, apperr.Validation("auto_accept_confidence 必须在 0 到 1 之间")
		}
		updates["auto_accept_confidence"] = v
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
	return s.GetAISettings(ctx)
}
func toAnalysisView(row model.MaterialAnalysis, providerName string) AnalysisView {
	var suggestions Suggestions
	_ = json.Unmarshal([]byte(row.SuggestionsJSON), &suggestions)
	excerpt := ""
	if row.MarkdownPath != "" {
		if data, err := os.ReadFile(row.MarkdownPath); err == nil {
			excerpt = excerptRunes(string(data), analysisExcerptRunes)
		}
	}
	return AnalysisView{
		Analysis: row, ProviderName: providerName, Suggestions: suggestions,
		MarkdownExcerpt: excerpt, ImageCount: row.ImageCount, Confidence: row.Confidence,
	}
}

func excerptRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit])
}

// GetMaterialAnalysis returns the latest analysis for a material.
func (s *Service) GetMaterialAnalysis(ctx context.Context, materialID string) (AnalysisView, error) {
	var row model.MaterialAnalysis
	if err := s.db.WithContext(ctx).Where("material_id = ?", materialID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AnalysisView{}, apperr.NotFound("该资料尚无 AI 识别结果")
		}
		return AnalysisView{}, apperr.Internal(err)
	}
	return toAnalysisView(row, s.providerName(ctx, row.ProviderID)), nil
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

// EnqueueMaterialAnalysis creates or reuses an analysis and its job.
func (s *Service) EnqueueMaterialAnalysis(ctx context.Context, p auth.Principal, materialID string, force bool) (AnalysisView, string, error) {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", materialID).First(&material).Error; err != nil {
		return AnalysisView{}, "", apperr.NotFound("文件不存在")
	}
	var analysis model.MaterialAnalysis
	found := true
	if err := db.Where("material_id = ?", materialID).First(&analysis).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return AnalysisView{}, "", apperr.Internal(err)
		}
		found = false
	}
	if found {
		if analysis.Status == model.AnalysisPending || analysis.Status == model.AnalysisRunning {
			return toAnalysisView(analysis, s.providerName(ctx, analysis.ProviderID)), analysis.JobID, nil
		}
		if analysis.Status == model.AnalysisSucceeded && !force {
			return toAnalysisView(analysis, s.providerName(ctx, analysis.ProviderID)), analysis.JobID, nil
		}
	}
	now := s.now()
	jobID := newID()
	err := db.Transaction(func(tx *gorm.DB) error {
		if !found {
			analysis = model.MaterialAnalysis{
				ID: newID(), MaterialID: materialID, Status: model.AnalysisPending,
				ImagesJSON: "[]", SuggestionsJSON: "{}", CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&analysis).Error; err != nil {
				return err
			}
		}
		analysis.Status = model.AnalysisPending
		analysis.JobID = jobID
		analysis.Error = ""
		analysis.SuggestionsJSON = "{}"
		analysis.Confidence = 0
		analysis.AppliedAt = nil
		analysis.UpdatedAt = now
		if err := tx.Model(&model.MaterialAnalysis{}).Where("id = ?", analysis.ID).Updates(map[string]any{
			"status": analysis.Status, "job_id": jobID, "error": "", "suggestions_json": "{}",
			"confidence": 0, "applied_at": nil, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		job := model.Job{ID: jobID, Kind: model.JobAnalyze, Status: model.JobPending, MaterialID: materialID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		if err := s.setStageTx(tx, materialID, model.StageProcessing, nil, ""); err != nil {
			return err
		}
		return s.audit(tx, p, "material.ai_enqueue", materialID)
	})
	if err != nil {
		return AnalysisView{}, "", apperr.Internal(err)
	}
	s.notify()
	return toAnalysisView(analysis, s.providerName(ctx, analysis.ProviderID)), jobID, nil
}

// ApplyMaterialAnalysis re-applies the stored suggestions as a human decision:
// pack fields overwrite, the file part is set and the file leaves the inbox.
func (s *Service) ApplyMaterialAnalysis(ctx context.Context, p auth.Principal, materialID string) (MaterialView, error) {
	var row model.MaterialAnalysis
	if err := s.db.WithContext(ctx).Where("material_id = ?", materialID).First(&row).Error; err != nil {
		return MaterialView{}, apperr.NotFound("该文件尚无 AI 识别结果")
	}
	if row.Status != model.AnalysisSucceeded {
		return MaterialView{}, apperr.Conflict("AI 识别尚未成功完成")
	}
	var suggestions Suggestions
	if err := json.Unmarshal([]byte(row.SuggestionsJSON), &suggestions); err != nil {
		return MaterialView{}, apperr.Internal(err)
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now()
		var m model.Material
		if err := tx.Where("id = ?", materialID).First(&m).Error; err != nil {
			return apperr.NotFound("文件不存在")
		}
		ids, err := s.applySuggestionsTx(tx, m, suggestions, true, now)
		if err != nil {
			return err
		}
		renamed = ids
		if err := tx.Model(&model.MaterialAnalysis{}).Where("id = ?", row.ID).Update("applied_at", now).Error; err != nil {
			return err
		}
		if err := s.setStageTx(tx, materialID, model.StageReady, nil, ""); err != nil {
			return err
		}
		return s.audit(tx, p, "material.ai_apply", materialID)
	})
	if err != nil {
		return MaterialView{}, err
	}
	s.notify()
	s.renameMaterialFiles(ctx, renamed)
	return s.GetMaterial(ctx, materialID)
}

// applySuggestionsTx writes validated suggestions. The file part always
// follows the model. Pack fields overwrite when the catalogue is still
// machine-owned (or force is set); once a person edited the pack, analysis
// only fills fields that are still empty.
func (s *Service) applySuggestionsTx(tx *gorm.DB, m model.Material, sg Suggestions, force bool, now time.Time) ([]string, error) {
	var renamed []string
	filePatch := MaterialPatch{}
	if sg.Part != "" {
		part := sg.Part
		filePatch.Part = &part
	}
	if sg.AnswerStartPage > 0 {
		page := sg.AnswerStartPage
		filePatch.AnswerStartPage = &page
	}
	if !filePatch.empty() {
		ids, err := s.applyMaterialPatchTx(tx, m.ID, filePatch, now)
		if err != nil {
			return nil, err
		}
		renamed = append(renamed, ids...)
	}
	var pack model.Pack
	if err := tx.Where("id = ?", m.PackID).First(&pack).Error; err != nil {
		return renamed, nil
	}
	overwrite := force || !pack.CatalogLocked
	patch := PackPatch{}
	setStr := func(dst **string, have, value string) {
		if value != "" && (overwrite || have == "") {
			v := value
			*dst = &v
		}
	}
	setStr(&patch.ExamName, pack.ExamName, sg.ExamName)
	setStr(&patch.DocType, pack.DocType, sg.DocType)
	setStr(&patch.Grade, pack.Grade, sg.Grade)
	setStr(&patch.Term, pack.Term, sg.Term)
	setStr(&patch.Region, pack.Region, sg.Region)
	setStr(&patch.Organization, pack.Organization, sg.Organization)
	setStr(&patch.ExamDate, pack.ExamDate, sg.ExamDate)
	setStr(&patch.Chapter, pack.Chapter, sg.Chapter)
	if sg.Cohort != 0 && (overwrite || pack.Cohort == 0) {
		v := sg.Cohort
		patch.Cohort = &v
	}
	if len(sg.SubjectCodes) > 0 && (overwrite || len(model.DecodeStrings(pack.SubjectsJSON)) == 0) {
		codes := append([]string{}, sg.SubjectCodes...)
		patch.SubjectCodes = &codes
	}
	if len(sg.Tags) > 0 {
		merged := model.DecodeStrings(pack.TagsJSON)
		seen := map[string]bool{}
		for _, t := range merged {
			seen[t] = true
		}
		for _, t := range sg.Tags {
			if !seen[t] && len(merged) < maxTags {
				merged = append(merged, t)
				seen[t] = true
			}
		}
		patch.Tags = &merged
	}
	if patch.empty() {
		return renamed, nil
	}
	ids, err := s.applyPackPatchTx(tx, pack.ID, patch, force, now)
	if err != nil {
		return nil, err
	}
	return append(renamed, ids...), nil
}

// prepareAnalyze marks the analysis running and builds the executor input with
// decrypted model and OCR credentials.
func (s *Service) prepareAnalyze(ctx context.Context, job model.Job, ocrProviderID string) (*jobexec.AnalyzeInput, error) {
	db := s.db.WithContext(ctx)
	var analysis model.MaterialAnalysis
	if err := db.Where("material_id = ?", job.MaterialID).First(&analysis).Error; err != nil {
		return nil, apperr.Conflict("分析记录不存在")
	}
	_ = db.Model(&model.MaterialAnalysis{}).Where("id = ?", analysis.ID).
		Updates(map[string]any{"status": model.AnalysisRunning, "error": "", "updated_at": s.now()}).Error
	settings, err := s.ensureAISettings(ctx)
	if err != nil {
		return nil, err
	}
	var material model.Material
	if err := db.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
		return nil, err
	}
	if material.MediaType == "application/zip" {
		return nil, apperr.Validation("不支持对 ZIP 容器本身做识别")
	}
	in := &jobexec.AnalyzeInput{
		Blob:     jobexec.Blob{SHA256: material.SHA256, Size: material.SizeBytes, FileName: material.FileName, MediaType: material.MediaType},
		Metadata: s.analysisMetadata(ctx, material), MaxPages: settings.MaxPages, MaxImages: settings.MaxImages,
	}
	var providers []model.AIProvider
	if err := db.Where("enabled = ?", true).Order("priority ASC").Order("created_at ASC").Find(&providers).Error; err != nil {
		return nil, err
	}
	vision := false
	for _, p := range providers {
		vision = vision || p.VisionModel != ""
		key, err := s.openAISecret(p.APIKeyCipher, p.ID)
		if err != nil {
			s.logf("analysis provider %s key unavailable", p.Name)
			continue
		}
		in.Models = append(in.Models, jobexec.ModelConfig{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: key, Model: p.DefaultModel, VisionModel: p.VisionModel})
	}
	ocr, key, err := s.ocrConfigForProvider(ctx, settings, ocrProviderID, material.MediaType)
	if err != nil {
		return nil, err
	}
	in.OCR, in.ParseKey = ocr, key
	// Visual analysis still needs page images, which are deliberately not cached.
	if !(vision && settings.MaxImages > 0) {
		if in.Cached = s.cachedText(ctx, material, key); in.Cached != nil && in.Cached.Markdown == "" {
			in.Cached = nil
		}
	}
	return in, nil
}

// commitAnalyze validates the model reply against the whitelist, stores the
// analysis and applies it to the catalogue in one transaction.
func (s *Service) commitAnalyze(ctx context.Context, job model.Job, out *jobexec.AnalyzeResult) error {
	if out == nil {
		return errors.New("分析结果为空")
	}
	db := s.db.WithContext(ctx)
	var analysis model.MaterialAnalysis
	if err := db.Where("material_id = ?", job.MaterialID).First(&analysis).Error; err != nil {
		return err
	}
	suggestions, confidence, err := parseSuggestions(out.Content)
	if err != nil {
		return err
	}
	if out.Parsed != nil {
		s.saveParsedText(ctx, job.MaterialID, out.Parsed)
	}
	markdown := out.Markdown
	if len(markdown) > jobexec.AnalysisStoreMarkdownLimit {
		markdown = markdown[:jobexec.AnalysisStoreMarkdownLimit]
	}
	dir := filepath.Join(s.blobs.DataDir(), "analysis", job.MaterialID)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	markdownPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o600); err != nil {
		return err
	}
	raw, _ := json.Marshal(suggestions)
	result := analyzeResult{
		ProviderID: out.ProviderID, Model: out.Model, MarkdownPath: markdownPath,
		PageCount: out.PageCount, SuggestionsJSON: string(raw), Confidence: confidence,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		now := s.now()
		res := tx.Model(&model.MaterialAnalysis{}).
			Where("id = ? AND job_id = ?", analysis.ID, job.ID).
			Updates(map[string]any{
				"status": model.AnalysisSucceeded, "provider_id": result.ProviderID, "model": result.Model,
				"markdown_path": result.MarkdownPath, "images_json": model.EncodeStrings(result.Images),
				"page_count": result.PageCount, "image_count": len(result.Images),
				"suggestions_json": result.SuggestionsJSON, "confidence": result.Confidence,
				"error": "", "applied_at": nil, "updated_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errLeaseLost
		}
		var suggestions Suggestions
		if err := json.Unmarshal([]byte(result.SuggestionsJSON), &suggestions); err != nil {
			return err
		}
		var material model.Material
		if err := tx.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
			return err
		}
		renamed, err := s.applySuggestionsTx(tx, material, suggestions, false, now)
		if err != nil {
			return err
		}
		result.Renamed = renamed
		if err := tx.Model(&model.MaterialAnalysis{}).
			Where("id = ? AND job_id = ?", analysis.ID, job.ID).
			Update("applied_at", now).Error; err != nil {
			return err
		}
		settings, err := s.loadAISettingsTx(tx)
		if err != nil {
			return err
		}
		if result.Confidence >= settings.AutoAcceptConfidence && len(suggestions.Uncertain) == 0 {
			if err := s.setStageTx(tx, job.MaterialID, model.StageReady, nil, ""); err != nil {
				return err
			}
		} else if err := s.setStageTx(tx, job.MaterialID, model.StageReview, []string{model.ReviewLowConfidence}, ""); err != nil {
			return err
		}
		// Pairing runs as its own job so a slow search never holds this lease.
		if err := s.enqueueGroupTx(tx, job.MaterialID, now); err != nil {
			return err
		}
		if err := s.auditSystem(tx, "material.ai_auto_apply", job.MaterialID); err != nil {
			return err
		}
		return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "")
	})
	if err != nil {
		return err
	}
	s.notify()
	s.renameMaterialFiles(context.WithoutCancel(ctx), result.Renamed)
	return nil
}

// failAnalyzeTx records an analysis failure next to the job.
func (s *Service) failAnalyzeTx(tx *gorm.DB, job model.Job, message string) error {
	if err := tx.Model(&model.MaterialAnalysis{}).
		Where("material_id = ? AND job_id = ?", job.MaterialID, job.ID).
		Updates(map[string]any{"status": model.AnalysisFailed, "error": message, "updated_at": s.now()}).Error; err != nil {
		return err
	}
	return s.setStageTx(tx, job.MaterialID, model.StageFailed, []string{model.ReviewAnalysisFailed}, "")
}

type analyzeResult struct {
	ProviderID      string
	Model           string
	MarkdownPath    string
	Images          []string
	PageCount       int
	ImageCount      int
	SuggestionsJSON string
	Confidence      float64
	Renamed         []string
}

// analysisMetadata collects the weak evidence the model may use: original
// name, arrival paths, the batch hints and the names of the other files in
// the same pack.
func (s *Service) analysisMetadata(ctx context.Context, m model.Material) string {
	db := s.db.WithContext(ctx)
	lines := []string{"文件名：" + m.OriginalName}
	var sources []model.MaterialSource
	_ = db.Where("material_id = ?", m.ID).Order("created_at ASC").Limit(5).Find(&sources).Error
	for _, src := range sources {
		lines = append(lines, "来源路径："+src.Path)
	}
	if len(sources) > 0 && sources[0].UploadID != "" {
		var up model.Upload
		if err := db.Where("id = ?", sources[0].UploadID).First(&up).Error; err == nil {
			h := decodeHints(up.HintsJSON)
			hint := []string{}
			if h.DocType != "" {
				hint = append(hint, "类型="+docTypeLabels[h.DocType])
			}
			if g := gradeLabels[h.Grade]; g != "" {
				hint = append(hint, "年级="+g)
			}
			for _, code := range h.SubjectCodes {
				hint = append(hint, "学科="+subjectLabel(code))
			}
			if h.Cohort > 0 {
				hint = append(hint, fmt.Sprintf("届=%d", h.Cohort))
			}
			if h.Region != "" {
				hint = append(hint, "地区="+h.Region)
			}
			if h.Organization != "" {
				hint = append(hint, "机构="+h.Organization)
			}
			if len(hint) > 0 {
				lines = append(lines, "上传者给这批文件的提示："+strings.Join(hint, "，"))
			}
		}
	}
	var siblings []string
	_ = db.Model(&model.Material{}).Where("pack_id = ? AND id <> ?", m.PackID, m.ID).Limit(10).Pluck("original_name", &siblings).Error
	if len(siblings) > 0 {
		lines = append(lines, "同一套里的其他文件："+strings.Join(siblings, "、"))
	}
	return strings.Join(lines, "\n")
}

func init() {
	// Nodes reject replies the master would reject, so the next endpoint is tried.
	jobexec.ValidReply = func(content string) error {
		_, _, err := parseSuggestions(content)
		return err
	}
}

func parseSuggestions(content string) (Suggestions, float64, error) {
	trimmed := strings.TrimSpace(content)
	if i := strings.Index(trimmed, "{"); i >= 0 {
		if j := strings.LastIndex(trimmed, "}"); j > i {
			trimmed = trimmed[i : j+1]
		}
	}
	var payload struct {
		Suggestions
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return Suggestions{}, 0, fmt.Errorf("模型输出不是合法 JSON")
	}
	out := sanitizeSuggestions(payload.Suggestions)
	confidence := payload.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return out, confidence, nil
}

// sanitizeSuggestions drops every value that fails the same validation as a
// manual edit; the model never gets a field past the whitelist.
func sanitizeSuggestions(in Suggestions) Suggestions {
	out := Suggestions{}
	if v, err := validateShortText("", in.ExamName, 255); err == nil {
		out.ExamName = v
	}
	if in.DocType != "" && validateDocType(in.DocType) == nil {
		out.DocType = in.DocType
	}
	if len(in.SubjectCodes) > 0 {
		if codes, err := validateSubjects(in.SubjectCodes); err == nil {
			out.SubjectCodes = codes
		}
	}
	if validateGrade(in.Grade) == nil {
		out.Grade = in.Grade
	}
	if validateTerm(in.Term) == nil {
		out.Term = in.Term
	}
	if validateCohort(in.Cohort) == nil {
		out.Cohort = in.Cohort
	}
	if v, err := validateShortText("", in.Region, 64); err == nil {
		out.Region = v
	}
	if v, err := validateShortText("", in.Organization, 128); err == nil {
		out.Organization = v
	}
	if validateExamDate(in.ExamDate) == nil {
		out.ExamDate = in.ExamDate
	}
	if v, err := validateShortText("", in.Chapter, 255); err == nil {
		out.Chapter = v
	}
	if len(in.Tags) > 0 {
		if tags, err := validateTags(in.Tags); err == nil {
			out.Tags = tags
		}
	}
	if in.Part != "" && validatePart(in.Part) == nil {
		out.Part = in.Part
	}
	if out.Part == model.PartFull && in.AnswerStartPage > 0 && in.AnswerStartPage <= 10000 {
		out.AnswerStartPage = in.AnswerStartPage
	}
	known := map[string]bool{"exam_name": true, "doc_type": true, "subject_codes": true, "grade": true, "term": true,
		"cohort": true, "region": true, "organization": true, "exam_date": true, "chapter": true, "tags": true, "part": true}
	for _, f := range in.Uncertain {
		if known[f] && len(out.Uncertain) < 12 {
			out.Uncertain = append(out.Uncertain, f)
		}
	}
	return out
}

// analyzable reports whether OCR and the model can read a file. Audio and
// spreadsheets are catalogued by hand or through their pack.
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

// autoEnqueueAnalysesTx creates pending analyses for new readable files when
// automatic analysis is enabled. Files that cannot be read go to the inbox
// unless they joined a pack with readable siblings (listening audio).
func (s *Service) autoEnqueueAnalysesTx(tx *gorm.DB, materials []*model.Material, settings model.AISettings, now time.Time) error {
	if !settings.AnalysisEnabled {
		return nil
	}
	for _, m := range materials {
		if !analyzable(m.MediaType) {
			var siblings int64
			if err := tx.Model(&model.Material{}).Where("pack_id = ? AND id <> ?", m.PackID, m.ID).Count(&siblings).Error; err != nil {
				return err
			}
			stage, reasons := model.StageReady, []string(nil)
			if siblings == 0 {
				stage, reasons = model.StageReview, []string{model.ReviewManual}
			}
			if err := s.setStageTx(tx, m.ID, stage, reasons, ""); err != nil {
				return err
			}
			continue
		}
		jobID := newID()
		analysis := model.MaterialAnalysis{
			ID: newID(), MaterialID: m.ID, Status: model.AnalysisPending, JobID: jobID,
			ImagesJSON: "[]", SuggestionsJSON: "{}", CreatedAt: now, UpdatedAt: now,
		}
		inserted := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "material_id"}}, DoNothing: true}).Create(&analysis)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			continue
		}
		job := model.Job{
			ID: jobID, Kind: model.JobAnalyze, Status: model.JobPending,
			MaterialID: m.ID, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
	}
	return nil
}
