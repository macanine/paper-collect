// Package model holds GORM table definitions and the fixed domain enumerations.
// List-valued fields are stored as JSON text so the schema stays portable across SQLite and PostgreSQL.
package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
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
	ScopeNodesManage      = "nodes:manage"
)

// AllScopes lists every known scope.
var AllScopes = []string{
	ScopeMaterialsRead, ScopeMaterialsWrite, ScopeCollectionsRead, ScopeCollectionsWrite,
	ScopeUploadsWrite, ScopeJobsRead, ScopeJobsWrite, ScopeStorageRead, ScopeStorageWrite,
	ScopeKeysManage, ScopeUsersManage, ScopeAuditRead,
	ScopeAIRead, ScopeAIWrite, ScopeSearchRead, ScopeSearchWrite, ScopeNodesManage,
}

// Material parts describe what one file is inside its pack.
const (
	// PartQuestions is the question paper without answers.
	PartQuestions = "questions"
	// PartAnswers holds only answers or worked solutions.
	PartAnswers = "answers"
	// PartFull is questions followed by a separate answer section in one file.
	PartFull = "full"
	// PartTeacher is a teacher copy where each answer follows its question.
	PartTeacher = "teacher"
	// PartAnswerSheet is the blank answer sheet.
	PartAnswerSheet = "answer_sheet"
	// PartListening is listening audio or its transcript.
	PartListening = "listening"
	// PartMain is the body of a single-document pack (lecture notes, slides, guides).
	PartMain = "main"
	// PartOther is used only when the file cannot be classified.
	PartOther = "other"
)

// MaterialParts lists the accepted material parts.
var MaterialParts = []string{PartQuestions, PartAnswers, PartFull, PartTeacher, PartAnswerSheet, PartListening, PartMain, PartOther}

// Material sync statuses: how far a ready file's drive copies have got. The
// original is always safe in the local CAS; this only reports the drives.
const (
	SyncWaitingCatalog = "waiting_catalog" // catalogue not confirmed yet, rules cannot run
	SyncNotRequired    = "not_required"    // the matched rule uploads nowhere
	SyncSyncing        = "syncing"         // some required target is pending, uploading or moving
	SyncSynced         = "synced"          // every required target is ready
	SyncAttention      = "attention"       // a required target failed, or a path drifted under on_path_change=keep
)

// SyncStatuses lists material sync statuses.
var SyncStatuses = []string{SyncWaitingCatalog, SyncNotRequired, SyncSyncing, SyncSynced, SyncAttention}

// Material curation stages: the one status a person needs to read. The AI
// decides every catalogue; people only step in for "attention".
const (
	// StageProcessing: the AI pipeline (read → group → decide) is working.
	StageProcessing = "processing"
	// StageWaitingAI: no usable model or OCR service; work resumes on its own
	// once the AI configuration is available again.
	StageWaitingAI = "waiting_ai"
	// StageAttention: the AI could not reach a verified decision.
	StageAttention = "attention"
	// StageReady: catalogued by a verified AI decision or by a person.
	StageReady = "ready"
)

// MaterialStages lists curation stages.
var MaterialStages = []string{StageProcessing, StageWaitingAI, StageAttention, StageReady}

// AI pipeline steps of a material that is not ready yet.
const (
	StepRead   = "read"
	StepGroup  = "group"
	StepDecide = "decide"
)

// Attention reasons that are not check codes.
const (
	AttentionAIFailed        = "ai_failed"        // the model kept failing after every retry
	AttentionGroupUnverified = "group_unverified" // grouping fell back to single-file sets
	AttentionMetadataOnly    = "metadata_only"    // no content could be read; judged by name only
)

// Pack document types: what kind of material a pack is.
var DocTypes = []string{
	"gaokao_real", "gaokao_mock", "zhongkao_real", "zhongkao_mock",
	"joint", "monthly", "midterm", "final", "opening", "unit", "weekly",
	"topic", "lecture", "study_guide", "courseware", "homework", "other",
}

// Grade values: junior grades are reserved for middle-school material.
var Grades = []string{"", "grade7", "grade8", "grade9", "grade10", "grade11", "grade12"}

// Term values.
var Terms = []string{"", "first", "second"}

// Upload and entry statuses.
const (
	UploadPending    = "pending"
	UploadProcessing = "processing"
	UploadCompleted  = "completed"
	UploadFailed     = "failed"

	EntryPending  = "pending"
	EntryReceived = "received"
	EntryExisting = "existing"
)

// Job kinds and statuses.
const (
	JobIngest    = "ingest"
	JobReplicate = "replicate"
	JobAnalyze   = "analyze" // READ: parse one file and let the model write its card
	JobIndex     = "index"
	JobGroup     = "group"  // GROUP: the model groups a batch of cards into sets
	JobDecide    = "decide" // DECIDE: the model decides one set's catalogue
	JobEntity    = "entity" // the model checks a new dictionary entry for duplicates

	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// Storage drive kinds.
const (
	KindLocal    = "local"
	KindWebDAV   = "webdav"
	KindLanzou   = "lanzou"
	KindPan123   = "123pan"
	KindOneDrive = "onedrive"
	KindS3       = "s3"
)

// DefaultPathTemplate lays drive copies out as a browsable folder tree.
const DefaultPathTemplate = "{学科}/{届别}/{类型}/{套卷}/{文件名}"

// StorageKinds lists drive kinds.
var StorageKinds = []string{KindLocal, KindWebDAV, KindLanzou, KindPan123, KindOneDrive, KindS3}

// On-path-change behaviours for a drive: whether a desired-path change (rename,
// re-catalogue, template edit) moves the remote copy or leaves it where it is.
const (
	OnPathChangeMove = "move"
	OnPathChangeKeep = "keep"
)

// OnPathChangeModes lists the accepted values.
var OnPathChangeModes = []string{OnPathChangeMove, OnPathChangeKeep}

// Replica states.
const (
	ReplicaPending   = "pending" // queued, not yet attempted
	ReplicaUploading = "uploading"
	ReplicaReady     = "ready"
	ReplicaMoving    = "moving"
	ReplicaFailed    = "failed"
	ReplicaSkipped   = "skipped"  // desired, but the drive rejects it (size/ext/disabled)
	ReplicaOrphaned  = "orphaned" // no longer desired by any rule; kept until a person deletes it

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
	SecretHash string `gorm:"size:64;not null;index"`
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
	Username  string    `gorm:"-"`
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

// Material is one unique file (one SHA-256). Re-uploading the same content adds
// a MaterialSource instead of a new row; parse, analysis and index results hang
// off the material, so each content is processed and paid for once.
type Material struct {
	ID              string `gorm:"primaryKey;size:36"`
	PackID          string `gorm:"size:36;not null;index"`
	Position        int    `gorm:"not null"`
	Part            string `gorm:"size:32;not null;index"`
	AnswerStartPage int    `gorm:"not null"`
	Title           string `gorm:"size:255;not null"`
	FileName        string `gorm:"type:text;not null"`
	OriginalName    string `gorm:"type:text;not null"`
	Stage           string `gorm:"size:16;not null;index"`
	AIStep          string `gorm:"size:16;not null;default:'';index"`
	AttentionJSON   string `gorm:"type:text;not null;default:'[]'"`
	// BatchID groups files the model groups together: the upload they
	// arrived with, or a library re-run.
	BatchID string `gorm:"size:36;not null;default:'';index"`
	// PartLocked marks a role set by a person; decisions keep it.
	PartLocked bool `gorm:"not null;default:false"`
	// Status reports whether the original is safe: archived once it is in the
	// local CAS, missing when it can only be pulled back from a drive.
	Status        string `gorm:"size:16;not null;index"`
	SyncStatus    string `gorm:"size:16;not null;index"`
	SyncPlannedAt *time.Time
	BlobID        string `gorm:"size:36;not null;index"`
	MediaType     string `gorm:"size:128;not null"`
	SizeBytes     int64  `gorm:"not null"`
	SHA256        string `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// Material archival statuses.
const (
	StatusArchived = "archived"
	StatusMissing  = "missing"
)

// MaterialSource records each time and place a file arrived.
type MaterialSource struct {
	ID         string `gorm:"primaryKey;size:36"`
	MaterialID string `gorm:"size:36;not null;index"`
	UploadID   string `gorm:"size:36;index"`
	UserID     string `gorm:"size:36"`
	Path       string `gorm:"type:text;not null"`
	Duplicate  bool   `gorm:"not null"`
	CreatedAt  time.Time
}

// Pack is the unit people browse: one exam set (questions, answers, answer
// sheet...) or one single-document resource. Catalogue fields live here and
// every member file inherits them.
type Pack struct {
	ID    string `gorm:"primaryKey;size:36"`
	Title string `gorm:"size:255;not null"`
	// LockedJSON lists catalogue fields a person set; decisions keep them.
	LockedJSON   string `gorm:"type:text;not null;default:'[]'"`
	DocType      string `gorm:"size:32;not null;index"`
	SubjectsJSON string `gorm:"type:text;not null"`
	Grade        string `gorm:"size:16;not null;index"`
	Term         string `gorm:"size:16;not null"`
	Cohort       int    `gorm:"not null;index"`
	Region       string `gorm:"size:64;not null;index"`
	// Organization is the canonical name of OrganizationID, kept on the pack
	// for titles, filters and path templates.
	Organization   string `gorm:"size:128;not null"`
	OrganizationID string `gorm:"size:36;not null;default:'';index"`
	ExamDate       string `gorm:"size:7;not null"`
	ExamName       string `gorm:"size:255;not null"`
	Chapter        string `gorm:"size:255;not null"`
	TagsJSON       string `gorm:"type:text;not null"`
	Description    string `gorm:"type:text;not null"`
	// Revision changes whenever the members change; a decision made for an
	// older revision is discarded.
	Revision  int `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// Locked reports the locked catalogue fields.
func (p Pack) Locked() map[string]bool {
	out := map[string]bool{}
	for _, f := range DecodeStrings(p.LockedJSON) {
		out[f] = true
	}
	return out
}

// PackSubject links a pack to subject codes (filterable).
type PackSubject struct {
	PackID string `gorm:"primaryKey;size:36"`
	Code   string `gorm:"primaryKey;size:32;index"`
}

// Collection is a hand-curated topic ("专题") of packs.
type Collection struct {
	ID          string `gorm:"primaryKey;size:36"`
	Title       string `gorm:"size:255;not null"`
	Description string `gorm:"type:text;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CollectionItem places a pack in a topic at an explicit position.
type CollectionItem struct {
	ID           string `gorm:"primaryKey;size:36"`
	CollectionID string `gorm:"size:36;not null;uniqueIndex:uq_collection_pack,priority:1;index"`
	PackID       string `gorm:"size:36;not null;uniqueIndex:uq_collection_pack,priority:2;index"`
	Position     int    `gorm:"not null"`
	CreatedAt    time.Time
}

// Upload is one batch of dropped files. It never creates collections; hints
// are optional priors for analysis and the initial pack fields.
type Upload struct {
	ID             string `gorm:"primaryKey;size:36"`
	UserID         string `gorm:"size:36;not null;index"`
	Title          string `gorm:"size:255;not null"`
	HintsJSON      string `gorm:"type:text;not null"`
	Status         string `gorm:"size:16;not null;index"`
	JobID          string `gorm:"size:36;index"`
	NewCount       int    `gorm:"not null"`
	DuplicateCount int    `gorm:"not null"`
	SkippedJSON    string `gorm:"type:text;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UploadEntry is one file slot of an upload manifest. Status "existing" means
// the declared SHA-256 is already archived, so no bytes need to be sent.
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
	ID         string `gorm:"primaryKey;size:36"`
	Kind       string `gorm:"size:16;not null;index"`
	Status     string `gorm:"size:16;not null;index"`
	UploadID   string `gorm:"size:36;index"`
	ReplicaID  string `gorm:"size:36;index"`
	MaterialID string `gorm:"size:36;index"`
	PackID     string `gorm:"size:36;not null;default:'';index"`
	BatchID    string `gorm:"size:36;not null;default:'';index"`
	EntityID   string `gorm:"size:36;not null;default:'';index"`
	// PayloadJSON holds what a job was prepared with (the files of a group
	// chunk, the pack revision of a decision) so failures and commits refer
	// to exactly that work.
	PayloadJSON string `gorm:"type:text;not null;default:''"`
	// NextAttemptAt delays a retried job; nil means it may run now.
	NextAttemptAt   *time.Time `gorm:"index"`
	Attempts        int        `gorm:"not null"`
	Error           string     `gorm:"type:text;not null"`
	ProgressMessage string     `gorm:"type:text;not null;default:''"`
	ProgressCurrent int        `gorm:"not null;default:0"`
	ProgressTotal   int        `gorm:"not null;default:0"`
	LogJSON         string     `gorm:"type:text;not null;default:''"`
	LeaseOwner      string     `gorm:"size:128;index"` // node ID while running
	NodeID          string     `gorm:"size:36;index"`  // node of the latest execution
	NodeName        string     `gorm:"size:64"`
	LeaseUntil      *time.Time
	HeartbeatAt     *time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	DurationMS      int64 `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Drive route policies control which node may upload a replica. Pool uses the
// shared-pool behavior where any eligible node may claim the job. Preferred
// follows the configured priority list while falling back to the pool when no
// configured node is available. Exclusive only uses configured nodes.
const (
	DriveRoutePool      = "pool"
	DriveRoutePreferred = "preferred"
	DriveRouteExclusive = "exclusive"
)

var DriveRouteModes = []string{DriveRoutePool, DriveRoutePreferred, DriveRouteExclusive}

// DriveNodeRoute assigns one upload node to a drive. Lower priority numbers
// win when more than one configured node is available.
type DriveNodeRoute struct {
	ID        string `gorm:"primaryKey;size:36"`
	DriveID   string `gorm:"size:36;not null;uniqueIndex:uq_drive_node_route,priority:1;index"`
	NodeID    string `gorm:"size:36;not null;uniqueIndex:uq_drive_node_route,priority:2;index"`
	Priority  int    `gorm:"not null;index"`
	Enabled   bool   `gorm:"not null;default:true;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// JobLog contains only application-generated, non-secret progress information.
type JobLog struct {
	Time       time.Time      `json:"time"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	ElapsedMS  int64          `json:"elapsed_ms,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Metrics    map[string]any `json:"metrics,omitempty"`
}

// ClusterSettings is the singleton scheduling policy controlled by the master.
// Workers receive the relevant parts with every heartbeat.
type ClusterSettings struct {
	ID                         string `gorm:"primaryKey;size:36"`
	MasterKindsJSON            string `gorm:"type:text;not null;default:'[]'"`
	MasterIngestConcurrency    int    `gorm:"not null;default:1"`
	MasterReplicateConcurrency int    `gorm:"not null;default:2"`
	MasterAnalyzeConcurrency   int    `gorm:"not null;default:2"`
	MasterIndexConcurrency     int    `gorm:"not null;default:2"`
	LeaseSeconds               int    `gorm:"not null;default:120"`
	HeartbeatSeconds           int    `gorm:"not null;default:10"`
	ClaimWaitSeconds           int    `gorm:"not null;default:25"`
	PollSeconds                int    `gorm:"not null;default:2"`
	Revision                   int64  `gorm:"not null;default:1"`
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// TableName pins the singleton cluster policy table.
func (ClusterSettings) TableName() string { return "cluster_settings" }

// Node is one job executor. The master registers itself; workers authenticate
// with a token whose hash is stored here. Configured fields are desired state
// controlled from the master; the reported fields are the latest heartbeat.
type Node struct {
	ID                    string `gorm:"primaryKey;size:36"`
	Name                  string `gorm:"size:64;not null"`
	TokenHash             string `gorm:"size:64;uniqueIndex"`
	IsMaster              bool   `gorm:"not null;default:false"`
	Enabled               bool   `gorm:"not null;default:true"`
	Draining              bool   `gorm:"not null;default:false"`
	KindsJSON             string `gorm:"type:text;not null;default:'[]'"`
	SlotsJSON             string `gorm:"type:text;not null;default:'{}'"`
	ConfiguredSlotsJSON   string `gorm:"type:text;not null;default:'{}'"`
	ConfiguredCacheMax    int64  `gorm:"not null;default:0"`
	OCRProviderIDsJSON    string `gorm:"type:text;not null;default:'[]'"`
	ConfigRevision        int64  `gorm:"not null;default:1"`
	AppliedConfigRevision int64  `gorm:"not null;default:0"`
	Version               string `gorm:"size:64"`
	Protocol              int    `gorm:"not null;default:0"`
	LastIP                string `gorm:"size:64"`
	LastSeenAt            *time.Time
	StartedAt             *time.Time
	// The most recent heartbeat sample.
	CPUPercent float64
	CPUCores   int
	ProcCPU    float64
	MemUsed    int64
	MemTotal   int64
	ProcRSS    int64
	NetRxBps   int64
	NetTxBps   int64
	CacheUsed  int64
	CacheMax   int64
	Running    int `gorm:"not null;default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// JobLock serialises analyze and index work for one material across instances.
// Keys are stable resource names; the lease makes an abandoned claim recoverable.
type JobLock struct {
	Key        string    `gorm:"primaryKey;size:80"`
	JobID      string    `gorm:"size:36;not null;index"`
	LeaseUntil time.Time `gorm:"not null;index"`
	UpdatedAt  time.Time
}

// Drive is one network storage destination. It describes where and how to
// connect, plus which execution nodes may upload its replicas. Which files go
// there and under what path is decided entirely by Rules. Secrets are stored
// encrypted.
type Drive struct {
	ID              string `gorm:"primaryKey;size:36"`
	Name            string `gorm:"size:128;not null"`
	Kind            string `gorm:"size:16;not null"`
	Enabled         bool   `gorm:"not null;index"`
	RootPath        string `gorm:"type:text;not null"`
	MaxFileBytes    int64  `gorm:"not null"` // 0 means no system-imposed limit
	AllowedExtsJSON string `gorm:"type:text;not null"`
	ReadOrder       int    `gorm:"not null;index"`
	OnPathChange    string `gorm:"size:8;not null"`
	RouteMode       string `gorm:"size:16;not null;default:'pool';index"`
	ConfigJSON      string `gorm:"type:text;not null"`
	SecretCipher    string `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName names the drives table (kept plural per convention; "storage_drives"
// avoids colliding with a future generic "drives" concept).
func (Drive) TableName() string { return "storage_drives" }

// TableName names the per-drive upload routing table.
func (DriveNodeRoute) TableName() string { return "drive_node_routes" }

// Rule is one ordered sync rule. Rules are matched top to bottom; the first
// whose Conditions match a material governs it. A disabled rule is skipped as
// if absent. Exactly one row has IsDefault=true: the fixed "everything else"
// rule at the bottom of the list, which always matches and cannot be deleted.
type Rule struct {
	ID             string `gorm:"primaryKey;size:36"`
	Position       int    `gorm:"not null;index"`
	Name           string `gorm:"size:128;not null"`
	Enabled        bool   `gorm:"not null;index"`
	IsDefault      bool   `gorm:"not null"`
	ConditionsJSON string `gorm:"type:text;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName names the rules table.
func (Rule) TableName() string { return "sync_rules" }

// RuleConditions is the decoded form of Rule.ConditionsJSON. Fields are AND'd
// together; values within a field are OR'd. A zero value matches everything.
type RuleConditions struct {
	Subjects   []string `json:"subjects,omitempty"`
	DocTypes   []string `json:"doc_types,omitempty"`
	Grades     []string `json:"grades,omitempty"`
	Cohorts    []int    `json:"cohorts,omitempty"`
	Regions    []string `json:"regions,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Parts      []string `json:"parts,omitempty"`
	MediaTypes []string `json:"media_types,omitempty"`
	MinSize    int64    `json:"min_size,omitempty"`
	MaxSize    int64    `json:"max_size,omitempty"` // 0 means no upper bound
}

// RuleTarget is one drive a rule uploads matching files to.
type RuleTarget struct {
	ID           string `gorm:"primaryKey;size:36"`
	RuleID       string `gorm:"size:36;not null;uniqueIndex:uq_rule_target_drive,priority:1;index"`
	DriveID      string `gorm:"size:36;not null;uniqueIndex:uq_rule_target_drive,priority:2;index"`
	PathTemplate string `gorm:"type:text;not null"`
	Share        bool   `gorm:"not null"`
	Required     bool   `gorm:"not null"`
	Position     int    `gorm:"not null"`
}

// TableName names the rule targets table.
func (RuleTarget) TableName() string { return "sync_rule_targets" }

// Replica is one blob stored, or wanted, on one drive. (BlobID, DriveID) is
// unique. RuleID records which rule currently wants this copy; it is empty
// once the copy becomes orphaned (no rule wants it any more) but the row is
// kept so a person can review and delete it.
//
// DesiredPath is what the owning rule's template resolves to right now;
// RemotePath is where the bytes actually sit. They differ after a rename, a
// catalogue change or a template edit, until a move (or a person) reconciles
// them. A replica is readable once RemoteID is set, regardless of Status.
type Replica struct {
	ID             string `gorm:"primaryKey;size:36"`
	BlobID         string `gorm:"size:36;not null;uniqueIndex:uq_replica_blob_drive,priority:1;index"`
	DriveID        string `gorm:"size:36;not null;uniqueIndex:uq_replica_blob_drive,priority:2;index"`
	RuleID         string `gorm:"size:36;index"`
	Status         string `gorm:"size:16;not null;index"`
	SkipReason     string `gorm:"type:text;not null"`
	Error          string `gorm:"type:text;not null"`
	DesiredPath    string `gorm:"type:text;not null"`
	RemotePath     string `gorm:"type:text;not null"`
	RemoteID       string `gorm:"type:text;not null"`
	Share          bool   `gorm:"not null"`
	Required       bool   `gorm:"not null"`
	ShareStatus    string `gorm:"size:16;not null"`
	ShareURL       string `gorm:"type:text;not null"`
	SharePassword  string `gorm:"type:text;not null"`
	ShareExpiresAt *time.Time
	Verification   string `gorm:"size:16"`
	SizeBytes      int64  `gorm:"not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName names the replicas table.
func (Replica) TableName() string { return "replicas" }

// TableName keeps audit rows in a conventional plural table.
func (Audit) TableName() string { return "audits" }

// Analysis statuses.
const (
	AnalysisPending   = "pending"
	AnalysisRunning   = "running"
	AnalysisSucceeded = "succeeded"
	AnalysisFailed    = "failed"
)

const IndexStatusNotApplicable = "not_applicable"

// OCR provider kinds.
const (
	OCRKindMinerU    = "mineru"
	OCRKindPaddleOCR = "paddleocr"
	OCRKindRapidOCR  = "rapidocr"
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

// AISettings is the singleton AI configuration (ID is always 1). Cataloguing
// always runs through the AI; there is no switch to turn it off.
type AISettings struct {
	ID            int    `gorm:"primaryKey"`
	OCRProviderID string `gorm:"size:36;index"`
	MaxPages      int    `gorm:"not null"`
	MaxImages     int    `gorm:"not null"`
	UpdatedAt     time.Time
}

// TableName pins the singleton settings table name.
func (AISettings) TableName() string { return "ai_settings" }

// FileCard is the model's verified reading of one file (READ step).
type FileCard struct {
	MaterialID   string `gorm:"primaryKey;size:36"`
	Status       string `gorm:"size:16;not null;index"`
	JobID        string `gorm:"size:36"`
	ProviderID   string `gorm:"size:36"`
	Model        string `gorm:"size:200"`
	CardJSON     string `gorm:"type:text;not null"`
	ProblemsJSON string `gorm:"type:text;not null"`
	MetadataOnly bool   `gorm:"not null"`
	PageCount    int    `gorm:"not null"`
	Error        string `gorm:"type:text;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PackDecision is the latest verified AI catalogue of one pack (DECIDE step).
type PackDecision struct {
	PackID       string `gorm:"primaryKey;size:36"`
	Status       string `gorm:"size:16;not null;index"`
	JobID        string `gorm:"size:36"`
	Revision     int    `gorm:"not null"`
	ProviderID   string `gorm:"size:36"`
	Model        string `gorm:"size:200"`
	DecisionJSON string `gorm:"type:text;not null"`
	ProblemsJSON string `gorm:"type:text;not null"`
	Error        string `gorm:"type:text;not null"`
	DecidedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Entity kinds kept in the name dictionary.
const (
	EntityOrganization = "organization"
	EntityTag          = "tag"
)

// Entity sources: created by the AI or by a person.
const (
	EntitySourceAI    = "ai"
	EntitySourceHuman = "human"
)

// Entity is one canonical name (a school, a league, a tag). Every spelling
// the AI or a person met is an EntityAlias pointing here, so one school is
// always stored under one name.
type Entity struct {
	ID        string `gorm:"primaryKey;size:36"`
	Kind      string `gorm:"size:16;not null;index"`
	Name      string `gorm:"size:128;not null"`
	Region    string `gorm:"size:64;not null"`
	Source    string `gorm:"size:16;not null"`
	Locked    bool   `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EntityAlias maps one normalised spelling to its entity.
type EntityAlias struct {
	Kind      string `gorm:"primaryKey;size:16"`
	Norm      string `gorm:"primaryKey;size:128"`
	EntityID  string `gorm:"size:36;not null;index"`
	Text      string `gorm:"size:128;not null"`
	CreatedAt time.Time
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
	ID               string  `gorm:"primaryKey;size:36"`
	MaterialID       string  `gorm:"size:36;not null;index"`
	PageNo           int     `gorm:"not null;index"`
	Seq              int     `gorm:"not null"`
	QuestionNo       string  `gorm:"size:32"`
	Text             string  `gorm:"type:text;not null"`
	Normalized       string  `gorm:"type:text;not null"`
	MathIndexVersion int     `gorm:"not null;default:0;index"`
	Confidence       float64 `gorm:"not null"`
	Model            string  `gorm:"size:200"`
	PromptVersion    string  `gorm:"size:32"`
	CreatedAt        time.Time
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
		&Blob{}, &Material{}, &MaterialSource{}, &Pack{}, &PackSubject{},
		&Collection{}, &CollectionItem{},
		&Upload{}, &UploadEntry{}, &Job{}, &JobLock{},
		&Drive{}, &Rule{}, &RuleTarget{}, &Replica{},
		&DriveNodeRoute{},
		&AIProvider{}, &OCRProvider{}, &AISettings{}, &FileCard{}, &PackDecision{}, &Entity{}, &EntityAlias{},
		&SearchSettings{}, &MaterialText{}, &QuestionBlock{}, &QuestionTerm{}, &MaterialIndexState{},
		&ClusterSettings{}, &Node{},
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
