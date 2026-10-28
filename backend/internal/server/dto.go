package server

import (
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
	"github.com/nic/paper-collect/backend/internal/storage"
)

type userDTO struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type apiKeyDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
	Revoked   bool       `json:"revoked"`
	CreatedAt time.Time  `json:"created_at"`
}

type auditDTO struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	TargetID  string    `json:"target_id"`
	CreatedAt time.Time `json:"created_at"`
}

type materialDTO struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	SubjectCodes     []string  `json:"subject_codes"`
	Grade            string    `json:"grade"`
	AcademicYear     int       `json:"academic_year"`
	Tags             []string  `json:"tags"`
	Role             string    `json:"role"`
	BlobID           string    `json:"blob_id"`
	FileName         string    `json:"file_name"`
	MediaType        string    `json:"media_type"`
	SizeBytes        int64     `json:"size_bytes"`
	SHA256           string    `json:"sha256"`
	SourcePath       string    `json:"source_path"`
	ParentMaterialID string    `json:"parent_material_id"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type materialDetailDTO struct {
	materialDTO
	Replicas []replicaDTO `json:"replicas"`
}

type replicaDTO struct {
	ID             string     `json:"id"`
	BlobID         string     `json:"blob_id"`
	ProviderID     string     `json:"provider_id"`
	Status         string     `json:"status"`
	SizeBytes      int64      `json:"size_bytes"`
	Verification   string     `json:"verification"`
	Error          string     `json:"error"`
	ShareStatus    string     `json:"share_status"`
	ShareURL       string     `json:"share_url"`
	SharePassword  string     `json:"share_password"`
	ShareExpiresAt *time.Time `json:"share_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type collectionDTO struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Kind         string    `json:"kind"`
	ParentID     string    `json:"parent_id"`
	SubjectCodes []string  `json:"subject_codes"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type collectionItemDTO struct {
	ID           string       `json:"id"`
	CollectionID string       `json:"collection_id"`
	MaterialID   string       `json:"material_id"`
	Position     int          `json:"position"`
	Role         string       `json:"role"`
	RelativePath string       `json:"relative_path"`
	Material     *materialDTO `json:"material,omitempty"`
}

type collectionDetailDTO struct {
	collectionDTO
	Items []collectionItemDTO `json:"items"`
}

type uploadEntryDTO struct {
	ID           string `json:"id"`
	RelativePath string `json:"relative_path"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	Status       string `json:"status"`
}

type uploadDTO struct {
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	CollectionKind string           `json:"collection_kind"`
	SubjectCodes   []string         `json:"subject_codes"`
	Grade          string           `json:"grade"`
	AcademicYear   int              `json:"academic_year"`
	Tags           []string         `json:"tags"`
	Status         string           `json:"status"`
	CollectionID   string           `json:"collection_id"`
	MaterialID     string           `json:"material_id"`
	JobID          string           `json:"job_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Entries        []uploadEntryDTO `json:"entries"`
}

type jobDTO struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	UploadID   string    `json:"upload_id"`
	ReplicaID  string    `json:"replica_id"`
	MaterialID string    `json:"material_id"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type providerDTO struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Kind             string            `json:"kind"`
	Role             string            `json:"role"`
	Enabled          bool              `json:"enabled"`
	Priority         int               `json:"priority"`
	Weight           int               `json:"weight"`
	Config           map[string]string `json:"config"`
	SecretConfigured bool              `json:"secret_configured"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type policyDTO struct {
	Mode            string `json:"mode"`
	MinimumReplicas int    `json:"minimum_replicas"`
}

type loginDTO struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    int     `json:"expires_in"`
	User         userDTO `json:"user"`
}

type agentTokenDTO struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type createKeyDTO struct {
	Key    apiKeyDTO `json:"key"`
	Secret string    `json:"secret"`
}

type providerTestDTO struct {
	Healthy      bool                 `json:"healthy"`
	Message      string               `json:"message"`
	Capabilities storage.Capabilities `json:"capabilities"`
}

type reconcileDTO struct {
	Queued int `json:"queued"`
}

type finalizeDTO struct {
	JobID    string `json:"job_id"`
	UploadID string `json:"upload_id"`
}

type pageDTO[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func toUser(u model.User) userDTO {
	return userDTO{ID: u.ID, Username: u.Username, Role: u.Role, Enabled: u.Enabled, CreatedAt: u.CreatedAt}
}

func toAPIKey(k model.APIKey) apiKeyDTO {
	scopes := model.DecodeStrings(k.ScopesJSON)
	return apiKeyDTO{
		ID:        k.ID,
		Name:      k.Name,
		Scopes:    scopes,
		ExpiresAt: k.ExpiresAt,
		Revoked:   k.Revoked,
		CreatedAt: k.CreatedAt,
	}
}

func toAudit(a model.Audit) auditDTO {
	return auditDTO{ID: a.ID, UserID: a.UserID, Action: a.Action, TargetID: a.TargetID, CreatedAt: a.CreatedAt}
}

func toMaterial(m model.Material, subjects []string) materialDTO {
	if subjects == nil {
		subjects = []string{}
	}
	return materialDTO{
		ID:               m.ID,
		Title:            m.Title,
		Description:      m.Description,
		SubjectCodes:     subjects,
		Grade:            m.Grade,
		AcademicYear:     m.AcademicYear,
		Tags:             model.DecodeStrings(m.TagsJSON),
		Role:             m.Role,
		BlobID:           m.BlobID,
		FileName:         m.FileName,
		MediaType:        m.MediaType,
		SizeBytes:        m.SizeBytes,
		SHA256:           m.SHA256,
		SourcePath:       m.SourcePath,
		ParentMaterialID: m.ParentMaterialID,
		Status:           m.Status,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func toMaterialDetail(v service.MaterialView) materialDetailDTO {
	replicas := make([]replicaDTO, 0, len(v.Replicas))
	for _, r := range v.Replicas {
		replicas = append(replicas, toReplica(r))
	}
	return materialDetailDTO{materialDTO: toMaterial(v.Material, v.Subjects), Replicas: replicas}
}

func toReplica(r model.Replica) replicaDTO {
	return replicaDTO{
		ID:             r.ID,
		BlobID:         r.BlobID,
		ProviderID:     r.ProviderID,
		Status:         r.Status,
		SizeBytes:      r.SizeBytes,
		Verification:   r.Verification,
		Error:          r.Error,
		ShareStatus:    r.ShareStatus,
		ShareURL:       r.ShareURL,
		SharePassword:  r.SharePassword,
		ShareExpiresAt: r.ShareExpiresAt,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func toCollection(c model.Collection) collectionDTO {
	return collectionDTO{
		ID:           c.ID,
		Title:        c.Title,
		Description:  c.Description,
		Kind:         c.Kind,
		ParentID:     c.ParentID,
		SubjectCodes: model.DecodeStrings(c.SubjectsJSON),
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func toCollectionDetail(v service.CollectionView) collectionDetailDTO {
	items := make([]collectionItemDTO, 0, len(v.Items))
	for _, it := range v.Items {
		var material *materialDTO
		if it.Material != nil {
			m := toMaterial(it.Material.Material, it.Material.Subjects)
			material = &m
		}
		items = append(items, collectionItemDTO{
			ID:           it.Item.ID,
			CollectionID: it.Item.CollectionID,
			MaterialID:   it.Item.MaterialID,
			Position:     it.Item.Position,
			Role:         it.Item.Role,
			RelativePath: it.Item.RelativePath,
			Material:     material,
		})
	}
	return collectionDetailDTO{collectionDTO: toCollection(v.Collection), Items: items}
}

func toUploadEntry(e model.UploadEntry) uploadEntryDTO {
	return uploadEntryDTO{
		ID: e.ID, RelativePath: e.RelativePath, SizeBytes: e.SizeBytes,
		SHA256: e.SHA256, Status: e.Status,
	}
}

func toUpload(v service.UploadView) uploadDTO {
	entries := make([]uploadEntryDTO, 0, len(v.Entries))
	for _, e := range v.Entries {
		entries = append(entries, toUploadEntry(e))
	}
	return uploadDTO{
		ID:             v.Upload.ID,
		Title:          v.Upload.Title,
		CollectionKind: v.Upload.CollectionKind,
		SubjectCodes:   model.DecodeStrings(v.Upload.SubjectsJSON),
		Grade:          v.Upload.Grade,
		AcademicYear:   v.Upload.AcademicYear,
		Tags:           model.DecodeStrings(v.Upload.TagsJSON),
		Status:         v.Upload.Status,
		CollectionID:   v.Upload.CollectionID,
		MaterialID:     v.Upload.MaterialID,
		JobID:          v.Upload.JobID,
		CreatedAt:      v.Upload.CreatedAt,
		Entries:        entries,
	}
}

func toJob(j model.Job) jobDTO {
	return jobDTO{
		ID:         j.ID,
		Kind:       j.Kind,
		Status:     j.Status,
		UploadID:   j.UploadID,
		ReplicaID:  j.ReplicaID,
		MaterialID: j.MaterialID,
		Attempts:   j.Attempts,
		Error:      j.Error,
		CreatedAt:  j.CreatedAt,
		UpdatedAt:  j.UpdatedAt,
	}
}

func toProvider(p model.StorageProvider) providerDTO {
	cfg := model.DecodeMap(p.ConfigJSON)
	return providerDTO{
		ID:               p.ID,
		Name:             p.Name,
		Kind:             p.Kind,
		Role:             p.Role,
		Enabled:          p.Enabled,
		Priority:         p.Priority,
		Weight:           p.Weight,
		Config:           cfg,
		SecretConfigured: p.SecretCipher != "",
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

func toPolicy(p model.Policy) policyDTO {
	return policyDTO{Mode: p.Mode, MinimumReplicas: p.MinimumReplicas}
}

func toPage[T any, U any](src service.Page[T], convert func(T) U) pageDTO[U] {
	items := make([]U, 0, len(src.Items))
	for _, item := range src.Items {
		items = append(items, convert(item))
	}
	return pageDTO[U]{Items: items, Total: src.Total, Page: src.Page, PageSize: src.PageSize}
}

type aiProviderDTO struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	BaseURL          string    `json:"base_url"`
	Models           []string  `json:"models"`
	DefaultModel     string    `json:"default_model"`
	VisionModel      string    `json:"vision_model"`
	Enabled          bool      `json:"enabled"`
	Priority         int       `json:"priority"`
	APIKeyConfigured bool      `json:"api_key_configured"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type aiSettingsDTO struct {
	AnalysisEnabled  bool      `json:"analysis_enabled"`
	MinerUBaseURL    string    `json:"mineru_base_url"`
	MaxPages         int       `json:"max_pages"`
	MaxImages        int       `json:"max_images"`
	MinerUConfigured bool      `json:"mineru_configured"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type materialAnalysisDTO struct {
	ID              string              `json:"id"`
	MaterialID      string              `json:"material_id"`
	Status          string              `json:"status"`
	ProviderID      string              `json:"provider_id"`
	ProviderName    string              `json:"provider_name"`
	Model           string              `json:"model"`
	PageCount       int                 `json:"page_count"`
	ImageCount      int                 `json:"image_count"`
	Confidence      float64             `json:"confidence"`
	Suggestions     service.Suggestions `json:"suggestions"`
	MarkdownExcerpt string              `json:"markdown_excerpt"`
	Error           string              `json:"error"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

func toAIProvider(v service.AIProviderView) aiProviderDTO {
	models := v.Models
	if models == nil {
		models = []string{}
	}
	return aiProviderDTO{
		ID: v.Provider.ID, Name: v.Provider.Name, BaseURL: v.Provider.BaseURL,
		Models: models, DefaultModel: v.Provider.DefaultModel, VisionModel: v.Provider.VisionModel,
		Enabled: v.Provider.Enabled, Priority: v.Provider.Priority,
		APIKeyConfigured: v.Provider.APIKeyCipher != "",
		CreatedAt:        v.Provider.CreatedAt, UpdatedAt: v.Provider.UpdatedAt,
	}
}

func toAISettings(v service.AISettingsView) aiSettingsDTO {
	return aiSettingsDTO{
		AnalysisEnabled: v.Settings.AnalysisEnabled, MinerUBaseURL: v.Settings.MinerUBaseURL,
		MaxPages: v.Settings.MaxPages, MaxImages: v.Settings.MaxImages,
		MinerUConfigured: v.MinerUConfigured, UpdatedAt: v.Settings.UpdatedAt,
	}
}

func toMaterialAnalysis(v service.AnalysisView) materialAnalysisDTO {
	return materialAnalysisDTO{
		ID: v.Analysis.ID, MaterialID: v.Analysis.MaterialID, Status: v.Analysis.Status,
		ProviderID: v.Analysis.ProviderID, ProviderName: v.ProviderName, Model: v.Analysis.Model,
		PageCount: v.Analysis.PageCount, ImageCount: v.Analysis.ImageCount,
		Confidence: v.Confidence, Suggestions: v.Suggestions, MarkdownExcerpt: v.MarkdownExcerpt,
		Error: v.Analysis.Error, CreatedAt: v.Analysis.CreatedAt, UpdatedAt: v.Analysis.UpdatedAt,
	}
}

type searchHitDTO struct {
	QuestionID   string            `json:"question_id"`
	QuestionNo   string            `json:"question_no"`
	Text         string            `json:"text"`
	Snippet      string            `json:"snippet"`
	Highlights   [][2]int          `json:"highlights"`
	Score        float64           `json:"score"`
	PageNo       int               `json:"page_no"`
	Material     searchMaterialDTO `json:"material"`
	Subjects     []string          `json:"subject_codes"`
	CollectionID string            `json:"collection_id"`
}

type searchMaterialDTO struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	FileName string `json:"file_name"`
}

type searchResultDTO struct {
	Items  []searchHitDTO `json:"items"`
	Total  int            `json:"total"`
	TookMS int64          `json:"took_ms"`
}

type searchSettingsDTO struct {
	Enabled    bool   `json:"enabled"`
	MaxPages   int    `json:"max_pages"`
	BatchPages int    `json:"batch_pages"`
	BatchChars int    `json:"batch_chars"`
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type indexStateDTO struct {
	Status           string     `json:"status"`
	JobID            string     `json:"job_id"`
	PageCount        int        `json:"page_count"`
	QuestionCount    int        `json:"question_count"`
	LLMCalls         int        `json:"llm_calls"`
	LLMFailedBatches int        `json:"llm_failed_batches"`
	Error            string     `json:"error"`
	IndexedAt        *time.Time `json:"indexed_at"`
}

type questionDTO struct {
	ID         string    `json:"id"`
	MaterialID string    `json:"material_id"`
	PageNo     int       `json:"page_no"`
	Seq        int       `json:"seq"`
	QuestionNo string    `json:"question_no"`
	Text       string    `json:"text"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
}

func toSearchHit(h service.QuestionHit) searchHitDTO {
	return searchHitDTO{
		QuestionID: h.Question.ID, QuestionNo: h.Question.QuestionNo, Text: h.Question.Text,
		Snippet: h.Snippet, Highlights: h.Highlights, Score: h.Score, PageNo: h.Question.PageNo,
		Material: searchMaterialDTO{ID: h.Material.ID, Title: h.Material.Title, FileName: h.Material.FileName},
		Subjects: h.Subjects, CollectionID: h.CollectionID,
	}
}

func toSearchSettings(row model.SearchSettings) searchSettingsDTO {
	return searchSettingsDTO{
		Enabled: row.Enabled, MaxPages: row.MaxPages, BatchPages: row.BatchPages,
		BatchChars: row.BatchChars, ProviderID: row.ProviderID, Model: row.Model,
	}
}

func toIndexState(row model.MaterialIndexState) indexStateDTO {
	return indexStateDTO{
		Status: row.Status, JobID: row.JobID, PageCount: row.PageCount, QuestionCount: row.QuestionCount,
		LLMCalls: row.LLMCalls, LLMFailedBatches: row.LLMFailedBatches, Error: row.Error, IndexedAt: row.IndexedAt,
	}
}

func toQuestion(row model.QuestionBlock) questionDTO {
	return questionDTO{
		ID: row.ID, MaterialID: row.MaterialID, PageNo: row.PageNo, Seq: row.Seq,
		QuestionNo: row.QuestionNo, Text: row.Text, Confidence: row.Confidence, CreatedAt: row.CreatedAt,
	}
}
