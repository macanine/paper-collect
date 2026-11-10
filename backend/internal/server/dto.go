package server

import (
	"encoding/json"
	"time"

	"github.com/nic/paper-collect/backend/internal/catalogai"
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
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	TargetID  string    `json:"target_id"`
	CreatedAt time.Time `json:"created_at"`
}

type libraryStatsDTO struct {
	Packs          int64 `json:"packs"`
	Files          int64 `json:"files"`
	Processing     int64 `json:"processing"`
	WaitingAI      int64 `json:"waiting_ai"`
	NeedsAttention int64 `json:"needs_attention"`
	Ready          int64 `json:"ready"`
	MissingAnswers int64 `json:"missing_answers"`
	Trashed        int64 `json:"trashed"`
	WaitingCatalog int64 `json:"waiting_catalog"`
	Syncing        int64 `json:"syncing"`
	Synced         int64 `json:"synced"`
	Attention      int64 `json:"attention"`
	NotRequired    int64 `json:"not_required"`
	Archived       int64 `json:"archived"`
	Missing        int64 `json:"missing"`
}

type jobStatsDTO struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Running   int64 `json:"running"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

type materialDTO struct {
	ID              string        `json:"id"`
	PackID          string        `json:"pack_id"`
	Position        int           `json:"position"`
	Part            string        `json:"part"`
	AnswerStartPage int           `json:"answer_start_page"`
	Title           string        `json:"title"`
	FileName        string        `json:"file_name"`
	OriginalName    string        `json:"original_name"`
	Stage           string        `json:"stage"`
	AIStep          string        `json:"ai_step"`
	Attention       []string      `json:"attention_reasons"`
	PartLocked      bool          `json:"part_locked"`
	Status          string        `json:"status"`
	SyncStatus      string        `json:"sync_status"`
	SyncPlannedAt   *time.Time    `json:"sync_planned_at"`
	MediaType       string        `json:"media_type"`
	SizeBytes       int64         `json:"size_bytes"`
	SHA256          string        `json:"sha256"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Pack            *packDTO      `json:"pack,omitempty"`
}

type materialSourceDTO struct {
	ID        string    `json:"id"`
	UploadID  string    `json:"upload_id"`
	Path      string    `json:"path"`
	Duplicate bool      `json:"duplicate"`
	CreatedAt time.Time `json:"created_at"`
}

type materialDetailDTO struct {
	materialDTO
	Sources  []materialSourceDTO `json:"sources"`
	Replicas []replicaDTO        `json:"replicas"`
}

type reviewPackItemDTO struct {
	ID        string   `json:"id"`
	Stage     string   `json:"stage"`
	Attention []string `json:"attention_reasons"`
}

type reviewPackDTO struct {
	Pack  packDTO             `json:"pack"`
	Items []reviewPackItemDTO `json:"items"`
}

type packBriefDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type packMemberDTO struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	FileName        string `json:"file_name"`
	Part            string `json:"part"`
	AnswerStartPage int    `json:"answer_start_page"`
	Stage           string `json:"stage"`
	Status          string `json:"status"`
	SyncStatus      string `json:"sync_status"`
	MediaType       string `json:"media_type"`
	SizeBytes       int64  `json:"size_bytes"`
	Position        int    `json:"position"`
}

type packDTO struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	LockedFields  []string        `json:"locked_fields"`
	DocType       string          `json:"doc_type"`
	SubjectCodes  []string        `json:"subject_codes"`
	Grade         string          `json:"grade"`
	Term          string          `json:"term"`
	Cohort        int             `json:"cohort"`
	Region        string          `json:"region"`
	Organization  string          `json:"organization"`
	OrgID         string          `json:"organization_id"`
	ExamDate      string          `json:"exam_date"`
	ExamName      string          `json:"exam_name"`
	Chapter       string          `json:"chapter"`
	Tags          []string        `json:"tags"`
	Description   string          `json:"description"`
	Stage         string          `json:"stage"`
	IsExam        bool            `json:"is_exam"`
	HasQuestions  bool            `json:"has_questions"`
	HasAnswers    bool            `json:"has_answers"`
	HasSheet      bool            `json:"has_answer_sheet"`
	Members       []packMemberDTO `json:"members"`
	Collections   []collectionDTO `json:"collections,omitempty"`
	DeletedAt     *time.Time      `json:"deleted_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type facetValueDTO struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type packFacetsDTO struct {
	Subjects      []facetValueDTO `json:"subjects"`
	Grades        []facetValueDTO `json:"grades"`
	DocTypes      []facetValueDTO `json:"doc_types"`
	Cohorts       []facetValueDTO `json:"cohorts"`
	Regions       []facetValueDTO `json:"regions"`
	Organizations []facetValueDTO `json:"organizations"`
	Tags          []facetValueDTO `json:"tags"`
}

type pullCacheDTO struct {
	DownloadURL string    `json:"download_url"`
	FileName    string    `json:"file_name"`
	MediaType   string    `json:"media_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	ExpiresAt   time.Time `json:"expires_at"`
	ExpiresIn   int64     `json:"expires_in"`
}

type replicaDTO struct {
	ID             string     `json:"id"`
	BlobID         string     `json:"blob_id"`
	DriveID        string     `json:"drive_id"`
	RuleID         string     `json:"rule_id"`
	DesiredPath    string     `json:"desired_path"`
	RemotePath     string     `json:"remote_path"`
	SkipReason     string     `json:"skip_reason"`
	Share          bool       `json:"share"`
	Required       bool       `json:"required"`
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
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	PackCount   int64     `json:"pack_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type collectionDetailDTO struct {
	collectionDTO
	Packs []packDTO `json:"packs"`
}

type uploadEntryDTO struct {
	ID           string `json:"id"`
	RelativePath string `json:"relative_path"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	Status       string `json:"status"`
}

type uploadHintsDTO struct {
	SubjectCodes []string `json:"subject_codes"`
	Grade        string   `json:"grade"`
	DocType      string   `json:"doc_type"`
	Cohort       int      `json:"cohort"`
	Region       string   `json:"region"`
	Organization string   `json:"organization"`
	Tags         []string `json:"tags"`
}

type uploadResultDTO struct {
	MaterialID string `json:"material_id"`
	PackID     string `json:"pack_id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Duplicate  bool   `json:"duplicate"`
	Stage      string `json:"stage"`
	Part       string `json:"part"`
}

type uploadDTO struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Hints          uploadHintsDTO    `json:"hints"`
	Status         string            `json:"status"`
	JobID          string            `json:"job_id"`
	NewCount       int               `json:"new_count"`
	DuplicateCount int               `json:"duplicate_count"`
	Skipped        []string          `json:"skipped"`
	CreatedAt      time.Time         `json:"created_at"`
	Entries        []uploadEntryDTO  `json:"entries"`
	Results        []uploadResultDTO `json:"results"`
}

type jobDTO struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	UploadID        string         `json:"upload_id"`
	ReplicaID       string         `json:"replica_id"`
	MaterialID      string         `json:"material_id"`
	NodeID          string         `json:"node_id"`
	NodeName        string         `json:"node_name"`
	Attempts        int            `json:"attempts"`
	Error           string         `json:"error"`
	ProgressMessage string         `json:"progress_message"`
	ProgressCurrent int            `json:"progress_current"`
	ProgressTotal   int            `json:"progress_total"`
	Logs            []model.JobLog `json:"logs,omitempty"`
	StartedAt       *time.Time     `json:"started_at"`
	FinishedAt      *time.Time     `json:"finished_at"`
	DurationMS      int64          `json:"duration_ms"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type driveDTO struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Kind             string               `json:"kind"`
	Enabled          bool                 `json:"enabled"`
	RootPath         string               `json:"root_path"`
	MaxFileBytes     int64                `json:"max_file_bytes"`
	AllowedExts      []string             `json:"allowed_exts"`
	ReadOrder        int                  `json:"read_order"`
	OnPathChange     string               `json:"on_path_change"`
	RouteMode        string               `json:"route_mode"`
	Routes           []driveRouteDTO      `json:"routes"`
	Config           map[string]string    `json:"config"`
	SecretConfigured bool                 `json:"secret_configured"`
	Capabilities     storage.Capabilities `json:"capabilities"`
	Stats            map[string]int64     `json:"stats"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type driveRouteDTO struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	IsMaster bool   `json:"is_master"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
}

type loginDTO struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    int     `json:"expires_in"`
	User         userDTO `json:"user"`
}

type createKeyDTO struct {
	Key    apiKeyDTO `json:"key"`
	Secret string    `json:"secret"`
}

type driveTestDTO struct {
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
	return auditDTO{ID: a.ID, UserID: a.UserID, Username: a.Username, Action: a.Action, TargetID: a.TargetID, CreatedAt: a.CreatedAt}
}

func toMaterial(v service.MaterialView) materialDTO {
	m := v.Material
	reasons := v.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	out := materialDTO{
		ID: m.ID, PackID: m.PackID, Position: m.Position, Part: m.Part, AnswerStartPage: m.AnswerStartPage,
		Title: m.Title, FileName: m.FileName, OriginalName: m.OriginalName, Stage: m.Stage, AIStep: m.AIStep,
		Attention: reasons, PartLocked: m.PartLocked, Status: m.Status, SyncStatus: m.SyncStatus, SyncPlannedAt: m.SyncPlannedAt, MediaType: m.MediaType, SizeBytes: m.SizeBytes, SHA256: m.SHA256,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if v.Pack != nil {
		p := toPack(*v.Pack)
		out.Pack = &p
	}
	return out
}

func toMaterialDetail(v service.MaterialView) materialDetailDTO {
	replicas := make([]replicaDTO, 0, len(v.Replicas))
	for _, r := range v.Replicas {
		replicas = append(replicas, toReplica(r))
	}
	sources := make([]materialSourceDTO, 0, len(v.Sources))
	for _, src := range v.Sources {
		sources = append(sources, materialSourceDTO{ID: src.ID, UploadID: src.UploadID, Path: src.Path, Duplicate: src.Duplicate, CreatedAt: src.CreatedAt})
	}
	return materialDetailDTO{materialDTO: toMaterial(v), Sources: sources, Replicas: replicas}
}

func toReviewPack(v service.ReviewPackView) reviewPackDTO {
	items := make([]reviewPackItemDTO, 0, len(v.Items))
	for _, item := range v.Items {
		reasons := item.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		items = append(items, reviewPackItemDTO{ID: item.ID, Stage: item.Stage, Attention: reasons})
	}
	return reviewPackDTO{Pack: toPack(v.Pack), Items: items}
}

func toPack(v service.PackView) packDTO {
	p := v.Pack
	members := make([]packMemberDTO, 0, len(v.Members))
	for _, m := range v.Members {
		members = append(members, packMemberDTO{
			ID: m.ID, Title: m.Title, FileName: m.FileName, Part: m.Part, AnswerStartPage: m.AnswerStartPage,
			Stage: m.Stage, Status: m.Status, SyncStatus: m.SyncStatus, MediaType: m.MediaType, SizeBytes: m.SizeBytes, Position: m.Position,
		})
	}
	subjects, tags := v.Subjects, v.Tags
	if subjects == nil {
		subjects = []string{}
	}
	if tags == nil {
		tags = []string{}
	}
	var deleted *time.Time
	if p.DeletedAt.Valid {
		t := p.DeletedAt.Time
		deleted = &t
	}
	var collections []collectionDTO
	if v.Collections != nil {
		collections = make([]collectionDTO, 0, len(v.Collections))
		for _, c := range v.Collections {
			collections = append(collections, toCollection(service.CollectionSummary{Collection: c}))
		}
	}
	return packDTO{
		ID: p.ID, Title: p.Title, LockedFields: model.DecodeStrings(p.LockedJSON), DocType: p.DocType,
		SubjectCodes: subjects, Grade: p.Grade, Term: p.Term, Cohort: p.Cohort, Region: p.Region,
		Organization: p.Organization, OrgID: p.OrganizationID, ExamDate: p.ExamDate, ExamName: p.ExamName, Chapter: p.Chapter, Tags: tags,
		Description: p.Description, Stage: v.Stage, IsExam: v.IsExam, HasQuestions: v.HasQuestions,
		HasAnswers: v.HasAnswers, HasSheet: v.HasSheet, Members: members, Collections: collections,
		DeletedAt: deleted, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func toFacets(f service.PackFacets) packFacetsDTO {
	conv := func(in []service.FacetValue) []facetValueDTO {
		out := make([]facetValueDTO, 0, len(in))
		for _, v := range in {
			out = append(out, facetValueDTO{Value: v.Value, Count: v.Count})
		}
		return out
	}
	return packFacetsDTO{
		Subjects: conv(f.Subjects), Grades: conv(f.Grades), DocTypes: conv(f.DocTypes), Cohorts: conv(f.Cohorts),
		Regions: conv(f.Regions), Organizations: conv(f.Organizations), Tags: conv(f.Tags),
	}
}

func toReplica(r model.Replica) replicaDTO {
	return replicaDTO{
		ID:      r.ID,
		BlobID:  r.BlobID,
		DriveID: r.DriveID, RuleID: r.RuleID, DesiredPath: r.DesiredPath, RemotePath: r.RemotePath, SkipReason: r.SkipReason, Share: r.Share, Required: r.Required,
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

func toCollection(v service.CollectionSummary) collectionDTO {
	c := v.Collection
	return collectionDTO{
		ID: c.ID, Title: c.Title, Description: c.Description, PackCount: v.PackCount,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toCollectionDetail(v service.CollectionView) collectionDetailDTO {
	packs := make([]packDTO, 0, len(v.Packs))
	for _, p := range v.Packs {
		packs = append(packs, toPack(p))
	}
	return collectionDetailDTO{
		collectionDTO: toCollection(service.CollectionSummary{Collection: v.Collection, PackCount: int64(len(packs))}),
		Packs:         packs,
	}
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
	results := make([]uploadResultDTO, 0, len(v.Results))
	for _, r := range v.Results {
		results = append(results, uploadResultDTO{
			MaterialID: r.Source.MaterialID, PackID: r.PackID, Title: r.Material.Title, Path: r.Source.Path,
			Duplicate: r.Source.Duplicate, Stage: r.Material.Stage, Part: r.Material.Part,
		})
	}
	skipped := v.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	h := v.Hints
	if h.SubjectCodes == nil {
		h.SubjectCodes = []string{}
	}
	if h.Tags == nil {
		h.Tags = []string{}
	}
	return uploadDTO{
		ID: v.Upload.ID, Title: v.Upload.Title, Status: v.Upload.Status, JobID: v.Upload.JobID,
		Hints: uploadHintsDTO{SubjectCodes: h.SubjectCodes, Grade: h.Grade, DocType: h.DocType, Cohort: h.Cohort,
			Region: h.Region, Organization: h.Organization, Tags: h.Tags},
		NewCount: v.Upload.NewCount, DuplicateCount: v.Upload.DuplicateCount, Skipped: skipped,
		CreatedAt: v.Upload.CreatedAt, Entries: entries, Results: results,
	}
}

func toJob(j model.Job) jobDTO {
	var logs []model.JobLog
	if j.LogJSON != "" {
		_ = json.Unmarshal([]byte(j.LogJSON), &logs)
	}
	return jobDTO{
		ID:              j.ID,
		Kind:            j.Kind,
		Status:          j.Status,
		UploadID:        j.UploadID,
		ReplicaID:       j.ReplicaID,
		MaterialID:      j.MaterialID,
		NodeID:          j.NodeID,
		NodeName:        j.NodeName,
		Attempts:        j.Attempts,
		Error:           j.Error,
		ProgressMessage: j.ProgressMessage,
		ProgressCurrent: j.ProgressCurrent,
		ProgressTotal:   j.ProgressTotal,
		Logs:            logs,
		StartedAt:       j.StartedAt,
		FinishedAt:      j.FinishedAt,
		DurationMS:      j.DurationMS,
		CreatedAt:       j.CreatedAt,
		UpdatedAt:       j.UpdatedAt,
	}
}

func toDrive(p model.Drive) driveDTO {
	routeMode := p.RouteMode
	if routeMode == "" {
		routeMode = model.DriveRoutePool
	}
	return driveDTO{ID: p.ID, Name: p.Name, Kind: p.Kind, Enabled: p.Enabled, RootPath: p.RootPath, MaxFileBytes: p.MaxFileBytes, AllowedExts: model.DecodeStrings(p.AllowedExtsJSON), ReadOrder: p.ReadOrder, OnPathChange: p.OnPathChange, RouteMode: routeMode, Routes: []driveRouteDTO{}, Config: model.DecodeMap(p.ConfigJSON), SecretConfigured: p.SecretCipher != "", CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
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
	OCRProviderID string    `json:"ocr_provider_id"`
	MaxPages      int       `json:"max_pages"`
	MaxImages     int       `json:"max_images"`
	OCRConfigured bool      `json:"ocr_configured"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ocrProviderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	BaseURL         string    `json:"base_url"`
	Model           string    `json:"model"`
	Enabled         bool      `json:"enabled"`
	Priority        int       `json:"priority"`
	TokenConfigured bool      `json:"token_configured"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type materialCardDTO struct {
	MaterialID      string              `json:"material_id"`
	Status          string              `json:"status"`
	ProviderName    string              `json:"provider_name"`
	Model           string              `json:"model"`
	PageCount       int                 `json:"page_count"`
	MetadataOnly    bool                `json:"metadata_only"`
	Card            catalogai.Card      `json:"card"`
	Problems        []catalogai.Problem `json:"problems"`
	MarkdownExcerpt string              `json:"markdown_excerpt"`
	Error           string              `json:"error"`
	UpdatedAt       *time.Time          `json:"updated_at"`
}

type packAIDTO struct {
	Status       string                    `json:"status"`
	ProviderName string                    `json:"provider_name"`
	Model        string                    `json:"model"`
	Revision     int                       `json:"revision"`
	Decision     *catalogai.Decision       `json:"decision"`
	Problems     []catalogai.Problem       `json:"problems"`
	Error        string                    `json:"error"`
	DecidedAt    *time.Time                `json:"decided_at"`
	Cards        map[string]catalogai.Card `json:"cards"`
}

type entityDTO struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Region  string   `json:"region"`
	Source  string   `json:"source"`
	Locked  bool     `json:"locked"`
	Aliases []string `json:"aliases"`
	Usage   int64    `json:"usage"`
}

type rerunDTO struct {
	Materials      int64 `json:"materials"`
	Packs          int64 `json:"packs"`
	EstimatedCalls int64 `json:"estimated_calls"`
	Applied        bool  `json:"applied"`
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
		OCRProviderID: v.Settings.OCRProviderID,
		MaxPages:      v.Settings.MaxPages, MaxImages: v.Settings.MaxImages,
		OCRConfigured: v.OCRConfigured,
		UpdatedAt:     v.Settings.UpdatedAt,
	}
}

func toOCRProvider(v service.OCRProviderView) ocrProviderDTO {
	return ocrProviderDTO{
		ID: v.Provider.ID, Name: v.Provider.Name, Kind: v.Provider.Kind,
		BaseURL: v.Provider.BaseURL, Model: v.Provider.Model,
		Enabled: v.Provider.Enabled, Priority: v.Provider.Priority,
		TokenConfigured: v.Provider.TokenCipher != "",
		CreatedAt:       v.Provider.CreatedAt, UpdatedAt: v.Provider.UpdatedAt,
	}
}

func toMaterialCard(v service.MaterialCardView) materialCardDTO {
	problems := v.Problems
	if problems == nil {
		problems = []catalogai.Problem{}
	}
	return materialCardDTO{
		MaterialID: v.MaterialID, Status: v.Status, ProviderName: v.ProviderName, Model: v.Model,
		PageCount: v.PageCount, MetadataOnly: v.MetadataOnly, Card: v.Card, Problems: problems,
		MarkdownExcerpt: v.MarkdownExcerpt, Error: v.Error, UpdatedAt: v.UpdatedAt,
	}
}

func toPackAI(v service.PackAIView) packAIDTO {
	out := packAIDTO{Status: "none", Problems: v.Problems, Cards: v.Cards}
	if out.Problems == nil {
		out.Problems = []catalogai.Problem{}
	}
	if out.Cards == nil {
		out.Cards = map[string]catalogai.Card{}
	}
	if d := v.Decision; d != nil {
		out.Status, out.Model, out.Revision, out.Error, out.DecidedAt = d.Status, d.Model, d.Revision, d.Error, d.DecidedAt
		out.ProviderName = v.ProviderName
		if d.Status == model.AnalysisSucceeded {
			parsed := v.Parsed
			out.Decision = &parsed
		}
	}
	return out
}

func toEntity(v service.EntityView) entityDTO {
	aliases := v.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	e := v.Entity
	return entityDTO{ID: e.ID, Kind: e.Kind, Name: e.Name, Region: e.Region, Source: e.Source, Locked: e.Locked, Aliases: aliases, Usage: v.Usage}
}

type searchHitDTO struct {
	QuestionID string            `json:"question_id"`
	QuestionNo string            `json:"question_no"`
	Text       string            `json:"text"`
	Snippet    string            `json:"snippet"`
	Highlights [][2]int          `json:"highlights"`
	Score      float64           `json:"score"`
	PageNo     int               `json:"page_no"`
	Material   searchMaterialDTO `json:"material"`
	Pack       packBriefDTO      `json:"pack"`
	Subjects   []string          `json:"subject_codes"`
	AlsoIn     []packBriefDTO    `json:"also_in"`
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
	highlights := h.Highlights
	if highlights == nil {
		highlights = [][2]int{}
	}
	also := make([]packBriefDTO, 0, len(h.AlsoIn))
	for _, p := range h.AlsoIn {
		also = append(also, packBriefDTO{ID: p.ID, Title: p.Title})
	}
	if h.Subjects == nil {
		h.Subjects = []string{}
	}
	return searchHitDTO{
		QuestionID: h.Question.ID, QuestionNo: h.Question.QuestionNo, Text: h.Question.Text,
		Snippet: h.Snippet, Highlights: highlights, Score: h.Score, PageNo: h.Question.PageNo,
		Material: searchMaterialDTO{ID: h.Material.ID, Title: h.Material.Title, FileName: h.Material.FileName},
		Pack:     packBriefDTO{ID: h.Pack.ID, Title: h.Pack.Title}, Subjects: h.Subjects, AlsoIn: also,
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

type nodeDTO struct {
	ID                    string         `json:"id"`
	Name                  string         `json:"name"`
	IsMaster              bool           `json:"is_master"`
	Enabled               bool           `json:"enabled"`
	Draining              bool           `json:"draining"`
	Status                string         `json:"status"`
	Kinds                 []string       `json:"kinds"`
	Slots                 map[string]int `json:"slots"`
	ConfiguredSlots       map[string]int `json:"configured_slots"`
	CacheMaxBytes         int64          `json:"cache_max_bytes"`
	OCRProviderIDs        []string       `json:"ocr_provider_ids"`
	ConfigRevision        int64          `json:"config_revision"`
	AppliedConfigRevision int64          `json:"applied_config_revision"`
	Running               int            `json:"running"`
	RunningKinds          map[string]int `json:"running_kinds"`
	Succeeded             int            `json:"succeeded_1h"`
	Failed                int            `json:"failed_1h"`
	Version               string         `json:"version"`
	Protocol              int            `json:"protocol"`
	LastIP                string         `json:"last_ip"`
	LastSeenAt            *time.Time     `json:"last_seen_at"`
	StartedAt             *time.Time     `json:"started_at"`
	CPUPercent            float64        `json:"cpu_percent"`
	CPUCores              int            `json:"cpu_cores"`
	ProcCPU               float64        `json:"proc_cpu_percent"`
	MemUsed               int64          `json:"mem_used"`
	MemTotal              int64          `json:"mem_total"`
	ProcRSS               int64          `json:"proc_rss"`
	NetRxBps              int64          `json:"net_rx_bps"`
	NetTxBps              int64          `json:"net_tx_bps"`
	CacheUsed             int64          `json:"cache_used"`
	CacheMax              int64          `json:"cache_max"`
	CreatedAt             time.Time      `json:"created_at"`
}

// nodeCreatedDTO is the only response that contains a node token.
type nodeCreatedDTO struct {
	nodeDTO
	Token string `json:"token"`
}

func toNode(v service.NodeView) nodeDTO {
	n := v.Node
	return nodeDTO{
		ID: n.ID, Name: n.Name, IsMaster: n.IsMaster, Enabled: n.Enabled, Draining: n.Draining,
		Status: v.Status, Kinds: v.Kinds, Slots: v.Slots, ConfiguredSlots: v.ConfiguredSlots,
		CacheMaxBytes: n.ConfiguredCacheMax, OCRProviderIDs: v.OCRProviderIDs,
		ConfigRevision: n.ConfigRevision, AppliedConfigRevision: n.AppliedConfigRevision,
		Running: n.Running, RunningKinds: v.RunningKinds,
		Succeeded: v.Succeeded, Failed: v.Failed, Version: n.Version, Protocol: n.Protocol,
		LastIP: n.LastIP, LastSeenAt: n.LastSeenAt, StartedAt: n.StartedAt,
		CPUPercent: n.CPUPercent, CPUCores: n.CPUCores, ProcCPU: n.ProcCPU,
		MemUsed: n.MemUsed, MemTotal: n.MemTotal, ProcRSS: n.ProcRSS,
		NetRxBps: n.NetRxBps, NetTxBps: n.NetTxBps, CacheUsed: n.CacheUsed, CacheMax: n.CacheMax,
		CreatedAt: n.CreatedAt,
	}
}

type clusterSettingsDTO struct {
	MasterKinds                []string  `json:"master_kinds"`
	MasterIngestConcurrency    int       `json:"master_ingest_concurrency"`
	MasterReplicateConcurrency int       `json:"master_replicate_concurrency"`
	MasterAnalyzeConcurrency   int       `json:"master_analyze_concurrency"`
	MasterIndexConcurrency     int       `json:"master_index_concurrency"`
	LeaseSeconds               int       `json:"lease_seconds"`
	HeartbeatSeconds           int       `json:"heartbeat_seconds"`
	ClaimWaitSeconds           int       `json:"claim_wait_seconds"`
	PollSeconds                int       `json:"poll_seconds"`
	Revision                   int64     `json:"revision"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

func toClusterSettings(v service.ClusterSettingsView) clusterSettingsDTO {
	return clusterSettingsDTO{
		MasterKinds: v.Kinds, MasterIngestConcurrency: v.Settings.MasterIngestConcurrency,
		MasterReplicateConcurrency: v.Settings.MasterReplicateConcurrency,
		MasterAnalyzeConcurrency:   v.Settings.MasterAnalyzeConcurrency,
		MasterIndexConcurrency:     v.Settings.MasterIndexConcurrency,
		LeaseSeconds:               v.Settings.LeaseSeconds, HeartbeatSeconds: v.Settings.HeartbeatSeconds,
		ClaimWaitSeconds: v.Settings.ClaimWaitSeconds, PollSeconds: v.Settings.PollSeconds,
		Revision: v.Settings.Revision, UpdatedAt: v.Settings.UpdatedAt,
	}
}
