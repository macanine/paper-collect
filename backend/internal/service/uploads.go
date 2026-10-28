package service

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// UploadInput is the manifest submitted by a client. Empty collection kind means "custom".
type UploadInput struct {
	Title          string
	CollectionKind string
	SubjectCodes   []string
	Grade          string
	AcademicYear   int
	Tags           []string
	Entries        []UploadEntryInput
}

// UploadEntryInput declares one file. SHA256 may be empty and is then computed on receipt.
type UploadEntryInput struct {
	RelativePath string
	SizeBytes    int64
	SHA256       string
}

// UploadView is an upload with its entries in manifest order.
type UploadView struct {
	Upload  model.Upload
	Entries []model.UploadEntry
}

// CreateUpload records a manifest. It creates no materials; that happens when the upload is finalized.
func (s *Service) CreateUpload(ctx context.Context, p auth.Principal, in UploadInput) (UploadView, error) {
	title, err := validateTitle(in.Title)
	if err != nil {
		return UploadView{}, err
	}
	kind := in.CollectionKind
	if kind == "" {
		kind = "custom"
	}
	if err := validateCollectionKind(kind); err != nil {
		return UploadView{}, err
	}
	subjects, err := validateOptionalSubjects(in.SubjectCodes)
	if err != nil {
		return UploadView{}, err
	}
	if err := validateGrade(in.Grade); err != nil {
		return UploadView{}, err
	}
	if err := validateYear(in.AcademicYear); err != nil {
		return UploadView{}, err
	}
	tags, err := validateTags(in.Tags)
	if err != nil {
		return UploadView{}, err
	}
	if n := len(in.Entries); n < 1 || n > maxUploadEntries {
		return UploadView{}, apperr.Validation("清单文件数必须为 1 到 2000 个")
	}

	seen := map[string]bool{}
	var total int64
	relPaths := make([]string, 0, len(in.Entries))
	sizes := map[string]UploadEntryInput{}
	for _, e := range in.Entries {
		if err := validateRelativePath(e.RelativePath); err != nil {
			return UploadView{}, err
		}
		if _, ok := blob.ExtensionKind(e.RelativePath); !ok {
			return UploadView{}, apperr.UnsupportedMedia("仅支持 .zip、.pptx、.pdf、.docx、.doc 文件")
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

	now := s.now()
	up := model.Upload{
		ID:             newID(),
		UserID:         p.UserID,
		Title:          title,
		CollectionKind: kind,
		SubjectsJSON:   model.EncodeStrings(subjects),
		Grade:          in.Grade,
		AcademicYear:   in.AcademicYear,
		TagsJSON:       model.EncodeStrings(tags),
		Status:         model.UploadPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	entries := make([]model.UploadEntry, 0, len(relPaths))
	for i, rel := range sortedByNatural(relPaths) {
		src := sizes[rel]
		entry := model.UploadEntry{
			ID:           newID(),
			UploadID:     up.ID,
			Position:     i,
			RelativePath: rel,
			SizeBytes:    src.SizeBytes,
			SHA256:       src.SHA256,
			Status:       model.EntryPending,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		entries = append(entries, entry)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&up).Error; err != nil {
			return wrap("create upload", err)
		}
		if err := tx.Create(&entries).Error; err != nil {
			return wrap("create upload entries", err)
		}
		return s.audit(tx, p, "upload.create", up.ID)
	})
	if err != nil {
		return UploadView{}, err
	}
	return UploadView{Upload: up, Entries: entries}, nil
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
	return UploadView{Upload: up, Entries: entries}, nil
}

// PutEntry streams one file to a staging file, checks size, real format and SHA-256,
// then publishes it into the CAS and marks the entry received.
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
	if !ok || kind != expected {
		return model.UploadEntry{}, apperr.UnsupportedMedia("文件真实格式与扩展名不一致")
	}
	if err := s.blobs.Publish(tmpPath, sum); err != nil {
		return model.UploadEntry{}, apperr.Internal(err)
	}
	published = true

	res := db.Model(&model.UploadEntry{}).
		Where("id = ? AND status = ?", entry.ID, model.EntryPending).
		Updates(map[string]any{
			"status":     model.EntryReceived,
			"sha256":     sum,
			"media_type": blob.MediaTypes[kind],
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
		return reloaded, nil
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
		Where("upload_id = ? AND status <> ?", uploadID, model.EntryReceived).Count(&missing).Error; err != nil {
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
