package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// UploadInput is the manifest submitted by a client. Hints are optional
// priors for analysis and the initial pack fields; nothing is required.
type UploadInput struct {
	Title   string
	Hints   UploadHints
	Entries []UploadEntryInput
}

// UploadHints describe a batch ("these are all 2025届 高三 数学").
type UploadHints struct {
	SubjectCodes []string `json:"subject_codes"`
	Grade        string   `json:"grade"`
	DocType      string   `json:"doc_type"`
	Cohort       int      `json:"cohort"`
	Region       string   `json:"region"`
	Organization string   `json:"organization"`
	Tags         []string `json:"tags"`
}

// UploadEntryInput declares one file. SHA256 may be empty and is then computed on receipt.
type UploadEntryInput struct {
	RelativePath string
	SizeBytes    int64
	SHA256       string
}

// UploadView is an upload with its entries in manifest order and, once
// processed, the files it produced.
type UploadView struct {
	Upload  model.Upload
	Hints   UploadHints
	Skipped []string
	Entries []model.UploadEntry
	Results []UploadResult
}

// UploadResult is one file an upload produced or matched.
type UploadResult struct {
	Source   model.MaterialSource
	Material model.Material
	PackID   string
}

func validateHints(in UploadHints) (UploadHints, error) {
	subjects, err := validateOptionalSubjects(in.SubjectCodes)
	if err != nil {
		return UploadHints{}, err
	}
	if err := validateGrade(in.Grade); err != nil {
		return UploadHints{}, err
	}
	if err := validateDocType(in.DocType); err != nil {
		return UploadHints{}, err
	}
	if err := validateCohort(in.Cohort); err != nil {
		return UploadHints{}, err
	}
	region, err := validateShortText("地区", in.Region, 64)
	if err != nil {
		return UploadHints{}, err
	}
	org, err := validateShortText("来源机构", in.Organization, 128)
	if err != nil {
		return UploadHints{}, err
	}
	tags, err := validateTags(in.Tags)
	if err != nil {
		return UploadHints{}, err
	}
	return UploadHints{SubjectCodes: subjects, Grade: in.Grade, DocType: in.DocType, Cohort: in.Cohort,
		Region: region, Organization: org, Tags: tags}, nil
}

func decodeHints(raw string) UploadHints {
	var h UploadHints
	_ = json.Unmarshal([]byte(raw), &h)
	if h.SubjectCodes == nil {
		h.SubjectCodes = []string{}
	}
	if h.Tags == nil {
		h.Tags = []string{}
	}
	return h
}

// CreateUpload records a manifest. Entries whose declared SHA-256 is already
// archived are marked existing and need no transfer ("秒传").
func (s *Service) CreateUpload(ctx context.Context, p auth.Principal, in UploadInput) (UploadView, error) {
	hints, err := validateHints(in.Hints)
	if err != nil {
		return UploadView{}, err
	}
	if n := len(in.Entries); n < 1 || n > maxUploadEntries {
		return UploadView{}, apperr.Validation("清单文件数必须为 1 到 2000 个")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = defaultUploadTitle(in.Entries)
	}
	title, err = validateTitle(truncateRunes(title, maxTitleRunes))
	if err != nil {
		return UploadView{}, err
	}

	seen := map[string]bool{}
	var total int64
	relPaths := make([]string, 0, len(in.Entries))
	sizes := map[string]UploadEntryInput{}
	for _, e := range in.Entries {
		if err := validateRelativePath(e.RelativePath); err != nil {
			return UploadView{}, err
		}
		_, ok := blob.ExtensionKind(e.RelativePath)
		if !ok {
			return UploadView{}, apperr.UnsupportedMedia("不支持该文件格式：" + path.Base(e.RelativePath))
		}
		if e.SizeBytes < 1 || e.SizeBytes > maxUploadBytes {
			return UploadView{}, apperr.Validation("单文件大小必须为 1 字节到 512 MiB")
		}
		sum, err := normalizeSHA(e.SHA256)
		if err != nil {
			return UploadView{}, err
		}
		if seen[e.RelativePath] {
			return UploadView{}, apperr.Conflict("清单中存在重复路径")
		}
		seen[e.RelativePath] = true
		total += e.SizeBytes
		if total > maxBatchBytes {
			return UploadView{}, apperr.Validation("清单总大小不能超过 2 GiB")
		}
		relPaths = append(relPaths, e.RelativePath)
		e.SHA256 = sum
		sizes[e.RelativePath] = e
	}

	db := s.db.WithContext(ctx)
	known, err := s.knownContent(db, sizes)
	if err != nil {
		return UploadView{}, err
	}
	hintsJSON, _ := json.Marshal(hints)
	now := s.now()
	up := model.Upload{
		ID: newID(), UserID: p.UserID, Title: title, HintsJSON: string(hintsJSON),
		Status: model.UploadPending, SkippedJSON: "[]", CreatedAt: now, UpdatedAt: now,
	}
	entries := make([]model.UploadEntry, 0, len(relPaths))
	for i, rel := range sortedByNatural(relPaths) {
		src := sizes[rel]
		entry := model.UploadEntry{
			ID: newID(), UploadID: up.ID, Position: i, RelativePath: rel, SizeBytes: src.SizeBytes,
			SHA256: src.SHA256, Status: model.EntryPending, CreatedAt: now, UpdatedAt: now,
		}
		if mediaType, ok := known[src.SHA256]; ok && src.SHA256 != "" {
			entry.Status = model.EntryExisting
			entry.MediaType = mediaType
		}
		entries = append(entries, entry)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&up).Error; err != nil {
			return wrap("create upload", err)
		}
		if err := tx.CreateInBatches(&entries, 250).Error; err != nil {
			return wrap("create upload entries", err)
		}
		return s.audit(tx, p, "upload.create", up.ID)
	})
	if err != nil {
		return UploadView{}, err
	}
	return UploadView{Upload: up, Hints: hints, Skipped: []string{}, Entries: entries, Results: []UploadResult{}}, nil
}

// knownContent returns media types of declared hashes that are already
// archived with matching size and whose original is still in the local CAS.
// ZIP containers are never archived as files, so they always transfer.
func (s *Service) knownContent(db *gorm.DB, entries map[string]UploadEntryInput) (map[string]string, error) {
	out := map[string]string{}
	sizes := map[string]int64{}
	hashes := []string{}
	for _, e := range entries {
		if e.SHA256 != "" {
			sizes[e.SHA256] = e.SizeBytes
			hashes = append(hashes, e.SHA256)
		}
	}
	for start := 0; start < len(hashes); start += 400 {
		end := min(start+400, len(hashes))
		var rows []model.Material
		if err := db.Unscoped().Select("sha256", "size_bytes", "media_type").
			Where("sha256 IN ?", hashes[start:end]).Find(&rows).Error; err != nil {
			return nil, wrap("find known content", err)
		}
		for _, m := range rows {
			if sizes[m.SHA256] == m.SizeBytes && s.blobs.Exists(m.SHA256) {
				out[m.SHA256] = m.MediaType
			}
		}
	}
	return out, nil
}

func defaultUploadTitle(entries []UploadEntryInput) string {
	if len(entries) == 0 {
		return "上传"
	}
	first := entries[0].RelativePath
	if i := strings.Index(first, "/"); i > 0 {
		return first[:i]
	}
	name := strings.TrimSuffix(path.Base(first), path.Ext(first))
	if len(entries) > 1 {
		return fmt.Sprintf("%s 等 %d 个文件", name, len(entries))
	}
	return name
}

// ListUploads returns the caller's upload history (all uploads for admins).
func (s *Service) ListUploads(ctx context.Context, p auth.Principal, pg PageQuery) (Page[UploadView], error) {
	q := s.db.WithContext(ctx).Model(&model.Upload{})
	if !p.IsAdmin() {
		q = q.Where("user_id = ?", p.UserID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return Page[UploadView]{}, wrap("count uploads", err)
	}
	var rows []model.Upload
	if err := q.Order("created_at DESC").Order("id DESC").Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[UploadView]{}, wrap("list uploads", err)
	}
	items := make([]UploadView, 0, len(rows))
	for _, up := range rows {
		items = append(items, UploadView{Upload: up, Hints: decodeHints(up.HintsJSON), Skipped: model.DecodeStrings(up.SkippedJSON)})
	}
	return newPage(items, total, pg), nil
}

// GetUpload returns a manifest with entry status, so an interrupted client can resume.
func (s *Service) GetUpload(ctx context.Context, p auth.Principal, id string) (UploadView, error) {
	db := s.db.WithContext(ctx)
	var up model.Upload
	if err := db.Where("id = ?", id).First(&up).Error; err != nil {
		return UploadView{}, apperr.NotFound("上传不存在")
	}
	if err := checkUploadAccess(p, up); err != nil {
		return UploadView{}, err
	}
	var entries []model.UploadEntry
	if err := db.Where("upload_id = ?", id).Order("position ASC").Find(&entries).Error; err != nil {
		return UploadView{}, wrap("load entries", err)
	}
	view := UploadView{Upload: up, Hints: decodeHints(up.HintsJSON), Skipped: model.DecodeStrings(up.SkippedJSON), Entries: entries, Results: []UploadResult{}}
	var sources []model.MaterialSource
	if err := db.Where("upload_id = ?", id).Order("created_at ASC").Order("path ASC").Find(&sources).Error; err != nil {
		return UploadView{}, wrap("load upload results", err)
	}
	ids := make([]string, 0, len(sources))
	for _, src := range sources {
		ids = append(ids, src.MaterialID)
	}
	materials := map[string]model.Material{}
	if len(ids) > 0 {
		var rows []model.Material
		if err := db.Unscoped().Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return UploadView{}, wrap("load upload materials", err)
		}
		for _, m := range rows {
			materials[m.ID] = m
		}
	}
	for _, src := range sources {
		m := materials[src.MaterialID]
		view.Results = append(view.Results, UploadResult{Source: src, Material: m, PackID: m.PackID})
	}
	return view, nil
}

// PutEntry streams and validates a file, then stores documents in the CAS or
// keeps ZIPs in a separate staging area until their ingest job succeeds.
// A repeated PUT of identical content returns the original result; different content is a conflict.
func (s *Service) PutEntry(ctx context.Context, p auth.Principal, uploadID, entryID string, body io.Reader, declared int64) (model.UploadEntry, error) {
	db := s.db.WithContext(ctx)
	var up model.Upload
	if err := db.Where("id = ?", uploadID).First(&up).Error; err != nil {
		return model.UploadEntry{}, apperr.NotFound("上传不存在")
	}
	if err := checkUploadAccess(p, up); err != nil {
		return model.UploadEntry{}, err
	}
	var entry model.UploadEntry
	if err := db.Where("id = ? AND upload_id = ?", entryID, uploadID).First(&entry).Error; err != nil {
		return model.UploadEntry{}, apperr.NotFound("文件条目不存在")
	}
	if entry.Status == model.EntryExisting {
		return entry, nil
	}
	if up.Status != model.UploadPending {
		return model.UploadEntry{}, apperr.Conflict("上传会话已完成，不能再上传文件")
	}
	if declared > entry.SizeBytes {
		return model.UploadEntry{}, apperr.TooLarge("请求体超过声明的文件大小")
	}

	tmp, err := s.blobs.NewTemp()
	if err != nil {
		return model.UploadEntry{}, apperr.Internal(err)
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()
	n, sum, copyErr := blob.CopyHashed(tmp, io.LimitReader(body, entry.SizeBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return model.UploadEntry{}, apperr.BadRequest("读取上传内容失败")
	}
	if closeErr != nil {
		return model.UploadEntry{}, apperr.Internal(closeErr)
	}
	if n > entry.SizeBytes {
		return model.UploadEntry{}, apperr.TooLarge("文件超过声明的大小")
	}
	if n != entry.SizeBytes {
		return model.UploadEntry{}, apperr.Unprocessable("字节数与声明不符")
	}

	if entry.Status == model.EntryReceived {
		if sum == entry.SHA256 {
			return entry, nil
		}
		return model.UploadEntry{}, apperr.Conflict("已接收的文件不能替换为不同内容")
	}
	if entry.SHA256 != "" && sum != entry.SHA256 {
		return model.UploadEntry{}, apperr.Unprocessable("SHA-256 与声明不符")
	}
	kind, err := blob.Sniff(tmpPath)
	if err != nil {
		return model.UploadEntry{}, apperr.UnsupportedMedia("文件内容无法识别，或压缩包结构无效")
	}
	expected, ok := blob.ExtensionKind(entry.RelativePath)
	if !ok || !blob.Compatible(kind, expected) {
		return model.UploadEntry{}, apperr.UnsupportedMedia("文件真实格式与扩展名不一致")
	}
	archivePath := ""
	if kind == blob.KindZIP {
		if err := s.stageArchive(tmpPath, entry.ID, sum); err != nil {
			return model.UploadEntry{}, apperr.Internal(err)
		}
		archivePath = s.archiveUploadPath(entry.ID, sum)
	} else if err := s.blobs.Publish(tmpPath, sum); err != nil {
		return model.UploadEntry{}, apperr.Internal(err)
	}
	published = true

	res := db.Model(&model.UploadEntry{}).
		Where("id = ? AND status = ?", entry.ID, model.EntryPending).
		Updates(map[string]any{
			"status":     model.EntryReceived,
			"sha256":     sum,
			"media_type": blob.MediaTypes[expected],
			"updated_at": s.now(),
		})
	if res.Error != nil {
		return model.UploadEntry{}, apperr.Internal(res.Error)
	}
	var reloaded model.UploadEntry
	if err := db.Where("id = ?", entry.ID).First(&reloaded).Error; err != nil {
		return model.UploadEntry{}, apperr.Internal(err)
	}
	if reloaded.Status == model.EntryReceived && reloaded.SHA256 == sum {
		if archivePath != "" && res.RowsAffected == 0 {
			// A duplicate writer may finish after the ingest worker has cleaned
			// up. Remove its staged file only if the upload is already complete.
			var current model.Upload
			if err := db.Select("status").Where("id = ?", uploadID).First(&current).Error; err == nil && current.Status == model.UploadCompleted {
				_ = os.Remove(archivePath)
			}
		}
		return reloaded, nil
	}
	if archivePath != "" && reloaded.Status == model.EntryReceived {
		// A concurrent request won with different content; its staging file has
		// a different name and must remain available to the ingest job.
		_ = os.Remove(archivePath)
	}
	return model.UploadEntry{}, apperr.Conflict("已接收的文件不能替换为不同内容")
}

// Finalize queues ingest exactly once per upload. Repeated calls return the same job id.
func (s *Service) Finalize(ctx context.Context, p auth.Principal, uploadID string) (string, error) {
	db := s.db.WithContext(ctx)
	var up model.Upload
	if err := db.Where("id = ?", uploadID).First(&up).Error; err != nil {
		return "", apperr.NotFound("上传不存在")
	}
	if err := checkUploadAccess(p, up); err != nil {
		return "", err
	}
	if up.JobID != "" {
		return up.JobID, nil
	}
	var missing int64
	if err := db.Model(&model.UploadEntry{}).
		Where("upload_id = ? AND status NOT IN ?", uploadID, []string{model.EntryReceived, model.EntryExisting}).Count(&missing).Error; err != nil {
		return "", wrap("count missing", err)
	}
	if missing > 0 {
		return "", apperr.Conflict("仍有文件未接收完成，不能完成上传")
	}

	jobID := newID()
	now := s.now()
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Upload{}).Where("id = ? AND job_id = ?", uploadID, "").
			Updates(map[string]any{"status": model.UploadProcessing, "job_id": jobID, "updated_at": now})
		if res.Error != nil {
			return wrap("mark upload", res.Error)
		}
		if res.RowsAffected == 0 {
			return errAlreadyFinalized
		}
		job := model.Job{
			ID:        jobID,
			Kind:      model.JobIngest,
			Status:    model.JobPending,
			UploadID:  uploadID,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&job).Error; err != nil {
			return wrap("create ingest job", err)
		}
		return s.audit(tx, p, "upload.finalize", uploadID)
	})
	if errors.Is(err, errAlreadyFinalized) {
		var again model.Upload
		if err := db.Where("id = ?", uploadID).First(&again).Error; err != nil {
			return "", apperr.Internal(err)
		}
		return again.JobID, nil
	}
	if err != nil {
		return "", apperr.Internal(err)
	}
	s.notify()
	return jobID, nil
}

var errAlreadyFinalized = errors.New("upload already finalized")

func checkUploadAccess(p auth.Principal, up model.Upload) error {
	if up.UserID == p.UserID || p.IsAdmin() {
		return nil
	}
	return apperr.Forbidden("无权访问该上传")
}

// normalizeSHA accepts an empty value or 64 hex digits in any case and returns lowercase.
func normalizeSHA(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	v := strings.ToLower(raw)
	if len(v) != 64 {
		return "", apperr.Validation("sha256 必须是 64 位十六进制字符串")
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", apperr.Validation("sha256 必须是 64 位十六进制字符串")
	}
	return v, nil
}
