package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
	"gorm.io/gorm"
)

// The AI pipeline. Every file goes through three model steps and nothing else
// decides its catalogue:
//
//	READ   (analyze job, per file, may run on worker nodes) → a verified card
//	GROUP  (group job, per batch, master only, one at a time) → sets
//	DECIDE (decide job, per set, master only) → the set's catalogue
//
// Files that arrive together share a BatchID. Grouping of a batch starts once
// none of its files is still being read. A set is re-decided whenever its
// members change; a decision made for an older member list is discarded.

// enqueueReadTx queues READ for one file and moves it back into processing.
func (s *Service) enqueueReadTx(tx *gorm.DB, materialID string, now time.Time) error {
	var open int64
	if err := tx.Model(&model.Job{}).Where("kind = ? AND material_id = ? AND status IN ?",
		model.JobAnalyze, materialID, []string{model.JobPending, model.JobRunning}).Count(&open).Error; err != nil {
		return wrap("count read jobs", err)
	}
	if err := tx.Model(&model.Material{}).Where("id = ?", materialID).Updates(map[string]any{
		"stage": model.StageProcessing, "ai_step": model.StepRead, "attention_json": "[]", "updated_at": now,
	}).Error; err != nil {
		return wrap("mark reading", err)
	}
	if open > 0 {
		return nil
	}
	jobID := newID()
	card := model.FileCard{MaterialID: materialID, Status: model.AnalysisPending, JobID: jobID,
		CardJSON: "{}", ProblemsJSON: "[]", CreatedAt: now, UpdatedAt: now}
	if err := tx.Save(&card).Error; err != nil {
		return wrap("reset card", err)
	}
	return wrap("create read job", tx.Create(&model.Job{ID: jobID, Kind: model.JobAnalyze, Status: model.JobPending,
		MaterialID: materialID, CreatedAt: now, UpdatedAt: now}).Error)
}

// cardMetaTx collects the non-content evidence for reading one file: its
// name, where it arrived from, the uploader's hints and the names of the
// files that arrived in the same folder.
func (s *Service) cardMetaTx(db *gorm.DB, m model.Material) jobexec.CardMeta {
	meta := jobexec.CardMeta{FileName: m.OriginalName, MediaType: m.MediaType}
	var sources []model.MaterialSource
	_ = db.Where("material_id = ?", m.ID).Order("created_at ASC").Limit(5).Find(&sources).Error
	for _, src := range sources {
		meta.Paths = append(meta.Paths, src.Path)
	}
	if len(sources) == 0 || sources[0].UploadID == "" {
		return meta
	}
	var up model.Upload
	if err := db.Where("id = ?", sources[0].UploadID).First(&up).Error; err == nil {
		meta.Hints = hintsText(decodeHints(up.HintsJSON))
	}
	dir := path.Dir(sources[0].Path)
	var siblings []model.MaterialSource
	_ = db.Where("upload_id = ? AND material_id <> ?", sources[0].UploadID, m.ID).Order("path ASC").Limit(500).Find(&siblings).Error
	for _, sib := range siblings {
		if path.Dir(sib.Path) == dir && len(meta.Siblings) < 15 {
			meta.Siblings = append(meta.Siblings, path.Base(sib.Path))
		}
	}
	return meta
}

// hintsText renders upload hints as evidence for the model. Hints never
// become catalogue values by themselves.
func hintsText(h UploadHints) string {
	parts := []string{}
	if h.DocType != "" {
		parts = append(parts, "类型="+catalogai.DocTypeLabels[h.DocType])
	}
	if g := catalogai.GradeLabels[h.Grade]; g != "" {
		parts = append(parts, "年级="+g)
	}
	for _, code := range h.SubjectCodes {
		parts = append(parts, "学科="+subjectLabel(code))
	}
	if h.Cohort > 0 {
		parts = append(parts, fmt.Sprintf("%d届", h.Cohort))
	}
	if h.Region != "" {
		parts = append(parts, "地区="+h.Region)
	}
	if h.Organization != "" {
		parts = append(parts, "学校/机构="+h.Organization)
	}
	for _, t := range h.Tags {
		parts = append(parts, "标签="+t)
	}
	return strings.Join(parts, "，")
}

func configError(err error) error {
	return &jobexec.ClassError{Class: jobexec.ClassConfig, Err: err}
}

// prepareAnalyze builds the READ input with decrypted model and OCR settings.
func (s *Service) prepareAnalyze(ctx context.Context, job model.Job, ocrProviderID string) (*jobexec.AnalyzeInput, error) {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	if material.MediaType == blob.MediaTypes[blob.KindZIP] {
		return nil, apperr.Validation("不支持对 ZIP 容器本身做识别")
	}
	settings, err := s.ensureAISettings(ctx)
	if err != nil {
		return nil, err
	}
	_ = db.Model(&model.FileCard{}).Where("material_id = ?", material.ID).
		Updates(map[string]any{"status": model.AnalysisRunning, "job_id": job.ID, "error": "", "updated_at": s.now()}).Error
	meta := s.cardMetaTx(db, material)
	in := &jobexec.AnalyzeInput{
		Blob:     jobexec.Blob{SHA256: material.SHA256, Size: material.SizeBytes, FileName: material.OriginalName, MediaType: material.MediaType},
		Readable: analyzable(material.MediaType), Meta: meta, MaxPages: settings.MaxPages, MaxImages: settings.MaxImages,
	}
	if in.Models, err = s.aiModels(ctx); err != nil {
		return nil, err
	}
	if len(in.Models) == 0 {
		return nil, configError(errors.New("没有启用的 AI 模型端点"))
	}
	if in.Readable {
		ocr, key, err := s.ocrConfigForProvider(ctx, settings, ocrProviderID, material.MediaType)
		if err != nil {
			return nil, configError(err)
		}
		in.OCR, in.ParseKey = ocr, key
		vision := false
		for _, m := range in.Models {
			vision = vision || m.VisionModel != ""
		}
		// Visual reading needs page images, which are deliberately not cached.
		if !(vision && settings.MaxImages > 0) {
			if in.Cached = s.cachedText(ctx, material, key); in.Cached != nil && in.Cached.Markdown == "" {
				in.Cached = nil
			}
		}
	}
	payload, _ := json.Marshal(meta)
	if err := db.Model(&model.Job{}).Where("id = ? AND lease_owner = ?", job.ID, job.LeaseOwner).
		Update("payload_json", string(payload)).Error; err != nil {
		return nil, err
	}
	return in, nil
}

// commitAnalyze re-checks the card on the master, stores it and moves the
// file on: to grouping when it arrived in a batch, otherwise straight to a
// new decision for the set it already belongs to.
func (s *Service) commitAnalyze(ctx context.Context, job model.Job, out *jobexec.AnalyzeResult) error {
	if out == nil {
		return errors.New("阅读结果为空")
	}
	db := s.db.WithContext(ctx)
	var latest model.Job
	if err := db.Select("id", "payload_json").Where("id = ?", job.ID).First(&latest).Error; err != nil {
		return err
	}
	var meta jobexec.CardMeta
	_ = json.Unmarshal([]byte(latest.PayloadJSON), &meta)
	cardIn := meta.CardInput(out.Markdown, out.PageCount, out.MetadataOnly)
	if out.Document != "" {
		cardIn = meta.CardInputFromDocument(out.Document, out.PageCount, out.MetadataOnly)
	}
	card, problems, err := catalogai.CheckCard(out.Content, cardIn)
	if err != nil {
		card, problems = catalogai.Card{}, []catalogai.Problem{catalogai.NotJSON()}
	}
	card.MetadataOnly = out.MetadataOnly
	if card.Role == "" {
		// Unknown role: the set decision assigns it with all files in view.
		card.Role = model.PartOther
	}
	if out.Parsed != nil {
		s.saveParsedText(ctx, job.MaterialID, out.Parsed)
	}
	cardJSON, _ := json.Marshal(card)
	problemsJSON, _ := json.Marshal(problems)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		now := s.now()
		if err := tx.Save(&model.FileCard{
			MaterialID: job.MaterialID, Status: model.AnalysisSucceeded, JobID: job.ID, ProviderID: out.ProviderID,
			Model: out.Model, CardJSON: string(cardJSON), ProblemsJSON: string(problemsJSON),
			MetadataOnly: out.MetadataOnly, PageCount: out.PageCount, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		var m model.Material
		if err := tx.Where("id = ?", job.MaterialID).First(&m).Error; err != nil {
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "文件已删除")
		}
		carry := []string{}
		if out.MetadataOnly && analyzable(m.MediaType) {
			carry = append(carry, model.AttentionMetadataOnly)
		}
		next := model.StepDecide
		if m.BatchID != "" || m.PackID == "" {
			next = model.StepGroup
		}
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"stage": model.StageProcessing, "ai_step": next, "attention_json": model.EncodeStrings(carry), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if next == model.StepGroup {
			batch := m.BatchID
			if batch == "" {
				batch = newID()
				if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Update("batch_id", batch).Error; err != nil {
					return err
				}
			}
			if err := s.maybeEnqueueGroupTx(tx, batch, now); err != nil {
				return err
			}
		} else if err := s.enqueueDecideTx(tx, m.PackID, now); err != nil {
			return err
		}
		message := fmt.Sprintf("已阅读：%s", catalogai.RoleLabels[card.Role])
		if out.MetadataOnly {
			message += "（未能读取正文，仅依据文件名）"
		}
		return s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded, message,
			map[string]any{"problems": len(problems), "page_count": out.PageCount})
	})
	if err != nil {
		return err
	}
	s.notify()
	return nil
}

// batchOrder sorts files by the path they arrived with so a folder's files
// stay next to each other when a batch is split into chunks.
func (s *Service) batchOrder(db *gorm.DB, rows []model.Material) ([]model.Material, map[string]string) {
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.ID)
	}
	paths := sourcePaths(db, ids)
	for _, m := range rows {
		if paths[m.ID] == "" {
			paths[m.ID] = m.OriginalName
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return pathsLess(paths[rows[i].ID], paths[rows[j].ID]) })
	return rows, paths
}

// sourcePaths returns the first arrival path of each file.
func sourcePaths(db *gorm.DB, ids []string) map[string]string {
	paths := map[string]string{}
	if len(ids) == 0 {
		return paths
	}
	var sources []model.MaterialSource
	_ = db.Where("material_id IN ?", ids).Order("created_at ASC").Find(&sources).Error
	for _, src := range sources {
		if _, ok := paths[src.MaterialID]; !ok {
			paths[src.MaterialID] = src.Path
		}
	}
	return paths
}

// folderOf is the arrival folder of a path; top-level files have none, because
// sharing the root says nothing about belonging to the same set.
func folderOf(p string) string {
	d := path.Dir(p)
	if d == "." || d == "/" || d == "" {
		return ""
	}
	return d
}

func pathsLess(a, b string) bool { return paths.NaturalLess(a, b) }

// loadCards returns the verified cards of the given files.
func loadCards(db *gorm.DB, ids []string) map[string]catalogai.Card {
	out := map[string]catalogai.Card{}
	if len(ids) == 0 {
		return out
	}
	var rows []model.FileCard
	_ = db.Where("material_id IN ? AND status = ?", ids, model.AnalysisSucceeded).Find(&rows).Error
	for _, row := range rows {
		var card catalogai.Card
		if json.Unmarshal([]byte(row.CardJSON), &card) == nil {
			out[row.MaterialID] = card
		}
	}
	return out
}

// RereadMaterial reads one file again and then re-decides its set.
func (s *Service) RereadMaterial(ctx context.Context, p auth.Principal, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Material
		if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
			return apperr.NotFound("文件不存在")
		}
		if m.PackID != "" {
			// Already placed: after reading, only its set is decided again.
			if err := tx.Model(&model.Material{}).Where("id = ?", id).Update("batch_id", "").Error; err != nil {
				return err
			}
		}
		if err := s.enqueueReadTx(tx, id, s.now()); err != nil {
			return err
		}
		return s.audit(tx, p, "material.ai_reread", id)
	})
	if err != nil {
		return err
	}
	s.notify()
	return nil
}

// MaterialCardView is the AI reading of one file.
type MaterialCardView struct {
	MaterialID      string
	Status          string
	ProviderName    string
	Model           string
	PageCount       int
	MetadataOnly    bool
	Card            catalogai.Card
	Problems        []catalogai.Problem
	MarkdownExcerpt string
	Error           string
	UpdatedAt       *time.Time
}

// GetMaterialCard returns the card of one file and an excerpt of its text.
func (s *Service) GetMaterialCard(ctx context.Context, id string) (MaterialCardView, error) {
	db := s.db.WithContext(ctx)
	var m model.Material
	if err := db.Select("id").Where("id = ?", id).First(&m).Error; err != nil {
		return MaterialCardView{}, apperr.NotFound("文件不存在")
	}
	view := MaterialCardView{MaterialID: id, Status: "none"}
	var row model.FileCard
	if err := db.Where("material_id = ?", id).First(&row).Error; err == nil {
		view.Status, view.Model, view.PageCount, view.MetadataOnly, view.Error = row.Status, row.Model, row.PageCount, row.MetadataOnly, row.Error
		view.ProviderName = s.providerName(ctx, row.ProviderID)
		_ = json.Unmarshal([]byte(row.CardJSON), &view.Card)
		_ = json.Unmarshal([]byte(row.ProblemsJSON), &view.Problems)
		updated := row.UpdatedAt
		view.UpdatedAt = &updated
	}
	var text model.MaterialText
	if err := db.Select("markdown").Where("material_id = ?", id).First(&text).Error; err == nil {
		view.MarkdownExcerpt = excerptRunes(text.Markdown, 4000)
	}
	return view, nil
}
