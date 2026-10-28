// Package model holds GORM table definitions and the fixed domain enumerations.
// List-valued fields are stored as JSON text so the schema stays portable across SQLite and PostgreSQL.
package model

import (
	"encoding/json"
	"time"
)

// Roles of human accounts.
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Scopes understood by the API and MCP.
const (
	ScopeMaterialsRead    = "materials:read"
	ScopeMaterialsWrite   = "materials:write"
	ScopeCollectionsRead  = "collections:read"
	ScopeCollectionsWrite = "collections:write"
	ScopeUploadsWrite     = "uploads:write"
	ScopeJobsRead         = "jobs:read"
	ScopeJobsWrite        = "jobs:write"
	ScopeStorageRead      = "storage:read"
	ScopeStorageWrite     = "storage:write"
	ScopeKeysManage       = "keys:manage"
	ScopeUsersManage      = "users:manage"
	ScopeAuditRead        = "audit:read"
	ScopeAIRead           = "ai:read"
	ScopeAIWrite          = "ai:write"
	ScopeSearchRead       = "search:read"
	ScopeSearchWrite      = "search:write"
)

// AllScopes lists every known scope.
var AllScopes = []string{
	ScopeMaterialsRead, ScopeMaterialsWrite, ScopeCollectionsRead, ScopeCollectionsWrite,
	ScopeUploadsWrite, ScopeJobsRead, ScopeJobsWrite, ScopeStorageRead, ScopeStorageWrite,
	ScopeKeysManage, ScopeUsersManage, ScopeAuditRead,
	ScopeAIRead, ScopeAIWrite, ScopeSearchRead, ScopeSearchWrite,
}

// Material roles describe the answer layout, not who the file is for.
const (
	// RoleExam is a paper with questions only (typically the student copy).
	RoleExam = "exam"
	// RoleAnswer contains answers only (answer key or answer sheet).
	RoleAnswer = "answer"
	// RoleQuestionsAnswers contains questions and answers, with answers grouped
	// at the end or in a separate answer section.
	RoleQuestionsAnswers = "questions_answers"
	// RoleTeacherEdition is a teacher copy where the answer/explanation follows
	// each question.
	RoleTeacherEdition = "teacher_edition"
	RoleOther          = "other"
)

// MaterialRoles lists the accepted material roles.
var MaterialRoles = []string{RoleExam, RoleAnswer, RoleQuestionsAnswers, RoleTeacherEdition, RoleOther}

// Material statuses.
const (
	StatusPendingStorage = "pending_storage"
	StatusArchived       = "archived"
	StatusDegraded       = "degraded"
)

// Collection kinds.
var CollectionKinds = []string{"series", "exam", "folder", "zip", "custom"}

// Collection kind used for ZIP child collections.
const KindZip = "zip"

// Grade values.
var Grades = []string{"", "grade10", "grade11", "grade12"}

// Upload and entry statuses.
const (
	UploadPending    = "pending"
	UploadProcessing = "processing"
	UploadCompleted  = "completed"
	UploadFailed     = "failed"

	EntryPending  = "pending"
	EntryReceived = "received"
)

// Job kinds and statuses.
const (
	JobIngest    = "ingest"
	JobReplicate = "replicate"
	JobAnalyze   = "analyze"
	JobIndex     = "index"

	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// Storage provider kinds, roles and policy modes.
const (
	KindLocal   = "local"
	KindWebDAV  = "webdav"
	KindLanzou  = "lanzou"
	KindPan123  = "123pan"
	RolePrimary = "primary"
	RoleBackup  = "backup"

	ModeMirrorAll     = "mirror_all"
	ModePrimaryBackup = "primary_backup"
	ModeBalanced      = "balanced"
)

// StorageKinds lists provider kinds.
var StorageKinds = []string{KindLocal, KindWebDAV, KindLanzou, KindPan123}

// PolicyModes lists storage policy modes.
var PolicyModes = []string{ModeMirrorAll, ModePrimaryBackup, ModeBalanced}

// Replica states.
const (
	ReplicaPending   = "pending"
	ReplicaUploading = "uploading"
	ReplicaReady     = "ready"
	ReplicaFailed    = "failed"

	ShareStatusPending     = "pending"
	ShareStatusReady       = "ready"
	ShareStatusUnsupported = "unsupported"
	ShareStatusFailed      = "failed"
)

// Subject is a fixed subject code with its Chinese display name.
type Subject struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Subjects is the fixed subject dictionary in display order.
var Subjects = []Subject{
	{"chinese", "语文"}, {"math", "数学"}, {"english", "英语"}, {"physics", "物理"},
	{"chemistry", "化学"}, {"biology", "生物"}, {"politics", "政治"}, {"history", "历史"},
	{"geography", "地理"}, {"information_technology", "信息技术"}, {"general_technology", "通用技术"},
	{"comprehensive", "综合"},
}

// IsSubject reports whether code is in the fixed subject dictionary.
func IsSubject(code string) bool {
	for _, s := range Subjects {
		if s.Code == code {
			return true
		}
	}
	return false
}

// User is a human account.
type User struct {
	ID           string `gorm:"primaryKey;size:36"`
	Username     string `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string `gorm:"size:255;not null"`
	Role         string `gorm:"size:16;not null"`
	Enabled      bool   `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// APIKey is a scoped, revocable credential owned by a user.
type APIKey struct {
	ID         string `gorm:"primaryKey;size:36"`
	UserID     string `gorm:"size:36;not null;index"`
	Name       string `gorm:"size:128;not null"`
	SecretHash string `gorm:"size:64;not null"`
	ScopesJSON string `gorm:"type:text;not null"`
	ExpiresAt  *time.Time
	Revoked    bool `gorm:"not null"`
	RevokedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Session backs a browser login. Only SHA-256 hashes of refresh tokens are stored.
type Session struct {
	ID          string    `gorm:"primaryKey;size:36"`
	UserID      string    `gorm:"size:36;not null;index"`
	RefreshHash string    `gorm:"size:64;not null;uniqueIndex"`
	PrevHash    string    `gorm:"size:64;index"`
	ExpiresAt   time.Time `gorm:"not null;index"`
	Revoked     bool      `gorm:"not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Audit records metadata only: never bodies, tokens, cookies, share URLs or passwords.
type Audit struct {
	ID        string    `gorm:"primaryKey;size:36"`
	UserID    string    `gorm:"size:36;index"`
	Action    string    `gorm:"size:64;not null;index"`
	TargetID  string    `gorm:"size:64"`
	CreatedAt time.Time `gorm:"index"`
}

// Blob is immutable content addressed by SHA-256.
type Blob struct {
	ID        string `gorm:"primaryKey;size:36"`
	SHA256    string `gorm:"size:64;not null;uniqueIndex"`
	SizeBytes int64  `gorm:"not null"`
	MediaType string `gorm:"size:128;not null"`
	FileName  string `gorm:"type:text;not null"`
	CreatedAt time.Time
}

// Material is one logical archived file with its cataloguing metadata.
type Material struct {
	ID               string `gorm:"primaryKey;size:36"`
	Title            string `gorm:"size:255;not null"`
	Description      string `gorm:"type:text;not null"`
	Grade            string `gorm:"size:16;not null"`
	AcademicYear     int    `gorm:"not null"`
	TagsJSON         string `gorm:"type:text;not null"`
	Role             string `gorm:"size:16;not null;index"`
	Status           string `gorm:"size:24;not null;index"`
	BlobID           string `gorm:"size:36;not null;index"`
	FileName         string `gorm:"type:text;not null"`
	MediaType        string `gorm:"size:128;not null"`
	SizeBytes        int64  `gorm:"not null"`
	SHA256           string `gorm:"size:64;not null;index"`
	SourcePath       string `gorm:"type:text;not null"`
	ParentMaterialID string `gorm:"size:36;index"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// MaterialSubject links a material to subject codes (filterable).
type MaterialSubject struct {
	MaterialID string `gorm:"primaryKey;size:36"`
	Code       string `gorm:"primaryKey;size:32;index"`
}

// Collection groups materials. ParentID is empty for roots.
type Collection struct {
	ID           string `gorm:"primaryKey;size:36"`
	Title        string `gorm:"size:255;not null"`
	Description  string `gorm:"type:text;not null"`
	Kind         string `gorm:"size:16;not null"`
	ParentID     string `gorm:"size:36;index"`
	SubjectsJSON string `gorm:"type:text;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CollectionItem places a material in a collection at an explicit position.
type CollectionItem struct {
	ID           string `gorm:"primaryKey;size:36"`
	CollectionID string `gorm:"size:36;not null;uniqueIndex:uq_collection_material,priority:1;index"`
	MaterialID   string `gorm:"size:36;not null;uniqueIndex:uq_collection_material,priority:2;index"`
	Position     int    `gorm:"not null"`
	Role         string `gorm:"size:16;not null"`
	RelativePath string `gorm:"type:text;not null"`
	CreatedAt    time.Time
}

// Upload is a client-provided manifest of files to archive.
type Upload struct {
	ID             string `gorm:"primaryKey;size:36"`
	UserID         string `gorm:"size:36;not null;index"`
	Title          string `gorm:"size:255;not null"`
	CollectionKind string `gorm:"size:16;not null"`
	SubjectsJSON   string `gorm:"type:text;not null"`
	Grade          string `gorm:"size:16;not null"`
	AcademicYear   int    `gorm:"not null"`
	TagsJSON       string `gorm:"type:text;not null"`
	Status         string `gorm:"size:16;not null;index"`
	CollectionID   string `gorm:"size:36"`
	MaterialID     string `gorm:"size:36"`
	JobID          string `gorm:"size:36;index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UploadEntry is one file slot of an upload manifest.
type UploadEntry struct {
	ID           string `gorm:"primaryKey;size:36"`
	UploadID     string `gorm:"size:36;not null;index"`
	Position     int    `gorm:"not null"`
	RelativePath string `gorm:"type:text;not null"`
	SizeBytes    int64  `gorm:"not null"`
	SHA256       string `gorm:"size:64"`
	MediaType    string `gorm:"size:128"`
	Status       string `gorm:"size:16;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Job is a persisted unit of background work.
type Job struct {
	ID          string `gorm:"primaryKey;size:36"`
	Kind        string `gorm:"size:16;not null;index"`
	Status      string `gorm:"size:16;not null;index"`
	UploadID    string `gorm:"size:36;index"`
	ReplicaID   string `gorm:"size:36;index"`
	MaterialID  string `gorm:"size:36;index"`
	Attempts    int    `gorm:"not null"`
	Error       string `gorm:"type:text;not null"`
	LeaseOwner  string `gorm:"size:128;index"`
	LeaseUntil  *time.Time
	HeartbeatAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// JobLock serialises analyze and index work for one material across instances.
// Keys are stable resource names; the lease makes an abandoned claim recoverable.
type JobLock struct {
	Key        string    `gorm:"primaryKey;size:80"`
	JobID      string    `gorm:"size:36;not null;index"`
	LeaseUntil time.Time `gorm:"not null;index"`
	UpdatedAt  time.Time
}

// StorageProvider is one storage endpoint. Secrets are stored encrypted.
type StorageProvider struct {
	ID           string `gorm:"primaryKey;size:36"`
	Name         string `gorm:"size:128;not null"`
	Kind         string `gorm:"size:16;not null"`
	Role         string `gorm:"size:16;not null"`
	Enabled      bool   `gorm:"not null;index"`
	Priority     int    `gorm:"not null;index"`
	Weight       int    `gorm:"not null"`
	ConfigJSON   string `gorm:"type:text;not null"`
	SecretCipher string `gorm:"type:text"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Replica is one blob stored on one provider. (BlobID, ProviderID) is unique.
type Replica struct {
	ID             string `gorm:"primaryKey;size:36"`
	BlobID         string `gorm:"size:36;not null;uniqueIndex:uq_replica_blob_provider,priority:1;index"`
	ProviderID     string `gorm:"size:36;not null;uniqueIndex:uq_replica_blob_provider,priority:2;index"`
	Status         string `gorm:"size:16;not null;index"`
	SizeBytes      int64  `gorm:"not null"`
	Verification   string `gorm:"size:16"`
	Error          string `gorm:"type:text;not null"`
	ShareStatus    string `gorm:"size:16;not null"`
	ShareURL       string `gorm:"type:text;not null"`
	SharePassword  string `gorm:"type:text;not null"`
	ShareExpiresAt *time.Time
	RemoteID       string `gorm:"type:text;not null"`
	RemoteKey      string `gorm:"type:text;not null"`
	RemoteName     string `gorm:"type:text;not null"`
	PlanMode       string `gorm:"size:16;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Policy is the singleton storage policy (ID is always 1).
type Policy struct {
	ID              int    `gorm:"primaryKey"`
	Mode            string `gorm:"size:16;not null"`
	MinimumReplicas int    `gorm:"not null"`
	UpdatedAt       time.Time
}

// TableName pins the singleton policy table name.
func (Policy) TableName() string { return "storage_policies" }

// TableName keeps audit rows in a conventional plural table.
func (Audit) TableName() string { return "audits" }

// Analysis statuses.
const (
	AnalysisPending   = "pending"
	AnalysisRunning   = "running"
	AnalysisSucceeded = "succeeded"
	AnalysisFailed    = "failed"
)

// OCR provider kinds.
const (
	OCRKindMinerU    = "mineru"
	OCRKindPaddleOCR = "paddleocr"
)

// OCRProvider is one hosted document parser. The token is stored encrypted and never returned.
type OCRProvider struct {
	ID          string `gorm:"primaryKey;size:36"`
	Name        string `gorm:"size:128;not null"`
	Kind        string `gorm:"size:16;not null;index"`
	BaseURL     string `gorm:"type:text;not null"`
	TokenCipher string `gorm:"type:text"`
	Model       string `gorm:"size:200;not null"`
	Enabled     bool   `gorm:"not null;index"`
	Priority    int    `gorm:"not null;index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AIProvider is one OpenAI-compatible endpoint. The API key is stored encrypted and never returned.
type AIProvider struct {
	ID           string `gorm:"primaryKey;size:36"`
	Name         string `gorm:"size:128;not null"`
	BaseURL      string `gorm:"type:text;not null"`
	APIKeyCipher string `gorm:"type:text"`
	ModelsJSON   string `gorm:"type:text;not null"`
	DefaultModel string `gorm:"size:200;not null"`
	VisionModel  string `gorm:"size:200;not null"`
	Enabled      bool   `gorm:"not null;index"`
	Priority     int    `gorm:"not null;index"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AISettings is the singleton document-analysis configuration (ID is always 1).
type AISettings struct {
	ID                int    `gorm:"primaryKey"`
	AnalysisEnabled   bool   `gorm:"not null"`
	OCRProviderID     string `gorm:"size:36;index"`
	MinerUBaseURL     string `gorm:"column:mineru_base_url;type:text;not null"`
	MinerUTokenCipher string `gorm:"column:mineru_token_cipher;type:text"`
	MaxPages          int    `gorm:"not null"`
	MaxImages         int    `gorm:"not null"`
	UpdatedAt         time.Time
}

// TableName pins the singleton settings table name.
func (AISettings) TableName() string { return "ai_settings" }

// MaterialAnalysis is the latest AI result for one material. Suggestions are never auto-applied.
type MaterialAnalysis struct {
	ID              string  `gorm:"primaryKey;size:36"`
	MaterialID      string  `gorm:"size:36;not null;uniqueIndex"`
	Status          string  `gorm:"size:16;not null;index"`
	ProviderID      string  `gorm:"size:36"`
	JobID           string  `gorm:"size:36"`
	Model           string  `gorm:"size:200"`
	MarkdownPath    string  `gorm:"type:text"`
	ImagesJSON      string  `gorm:"type:text;not null"`
	PageCount       int     `gorm:"not null"`
	ImageCount      int     `gorm:"not null"`
	SuggestionsJSON string  `gorm:"type:text;not null"`
	Confidence      float64 `gorm:"not null"`
	Error           string  `gorm:"type:text;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SearchSettings is the singleton question-index configuration (ID is always 1).
type SearchSettings struct {
	ID         int    `gorm:"primaryKey"`
	Enabled    bool   `gorm:"not null"`
	MaxPages   int    `gorm:"not null"`
	BatchPages int    `gorm:"not null"`
	BatchChars int    `gorm:"not null"`
	ProviderID string `gorm:"size:36"`
	Model      string `gorm:"size:200"`
	UpdatedAt  time.Time
}

// TableName pins the singleton search settings table.
func (SearchSettings) TableName() string { return "search_settings" }

// MaterialText caches OCR Markdown and per-page text for analysis and indexing.
type MaterialText struct {
	MaterialID string `gorm:"primaryKey;size:36"`
	SourceHash string `gorm:"size:64;not null"`
	ParseKey   string `gorm:"size:64"`
	PageCount  int    `gorm:"not null"`
	PagesJSON  string `gorm:"type:text;not null"`
	Markdown   string `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// QuestionBlock is one LLM-extracted question with its starting page.
type QuestionBlock struct {
	ID            string  `gorm:"primaryKey;size:36"`
	MaterialID    string  `gorm:"size:36;not null;index"`
	PageNo        int     `gorm:"not null;index"`
	Seq           int     `gorm:"not null"`
	QuestionNo    string  `gorm:"size:32"`
	Text          string  `gorm:"type:text;not null"`
	Normalized    string  `gorm:"type:text;not null"`
	Confidence    float64 `gorm:"not null"`
	Model         string  `gorm:"size:200"`
	PromptVersion string  `gorm:"size:32"`
	CreatedAt     time.Time
}

// QuestionTerm is a portable inverted-index posting.
type QuestionTerm struct {
	Term       string `gorm:"primaryKey;size:64;index"`
	QuestionID string `gorm:"primaryKey;size:36;index"`
	TF         int    `gorm:"not null"`
}

// MaterialIndexState tracks indexing progress for one material.
type MaterialIndexState struct {
	MaterialID       string `gorm:"primaryKey;size:36"`
	Status           string `gorm:"size:16;not null;index"`
	JobID            string `gorm:"size:36;index"`
	PageCount        int    `gorm:"not null"`
	QuestionCount    int    `gorm:"not null"`
	LLMCalls         int    `gorm:"not null"`
	LLMFailedBatches int    `gorm:"not null"`
	Error            string `gorm:"type:text;not null"`
	IndexedAt        *time.Time
	UpdatedAt        time.Time
}

// Models lists every model for AutoMigrate, in dependency-friendly order.
func Models() []any {
	return []any{
		&User{}, &APIKey{}, &Session{}, &Audit{},
		&Blob{}, &Material{}, &MaterialSubject{},
		&Collection{}, &CollectionItem{},
		&Upload{}, &UploadEntry{}, &Job{}, &JobLock{},
		&StorageProvider{}, &Replica{}, &Policy{},
		&AIProvider{}, &OCRProvider{}, &AISettings{}, &MaterialAnalysis{},
		&SearchSettings{}, &MaterialText{}, &QuestionBlock{}, &QuestionTerm{}, &MaterialIndexState{},
	}
}

// EncodeStrings stores a string list as JSON text; nil becomes "[]".
func EncodeStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// DecodeStrings reads a JSON text list. Invalid or empty text yields an empty, non-nil slice.
func DecodeStrings(text string) []string {
	out := []string{}
	if text == "" {
		return out
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

// EncodeMap stores a string map as JSON text; nil becomes "{}".
func EncodeMap(values map[string]string) string {
	if values == nil {
		values = map[string]string{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// DecodeMap reads a JSON text map. Invalid or empty text yields an empty, non-nil map.
func DecodeMap(text string) map[string]string {
	out := map[string]string{}
	if text == "" {
		return out
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}
