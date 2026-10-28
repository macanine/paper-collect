package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/archive"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
	"gorm.io/gorm"
)

// stagedBlob is a content-addressed file that is already in the CAS.
type stagedBlob struct {
	Size      int64
	MediaType string
	FileName  string
}

// plannedMaterial is a material to be inserted when the ingest transaction commits.
type plannedMaterial struct {
	ID         string
	Title      string
	FileName   string
	MediaType  string
	SHA        string
	Size       int64
	SourcePath string
	Role       string
	ParentID   string
	RelPath    string
}

// zipGroup is one uploaded package with the members extracted from it.
type zipGroup struct {
	Container    *plannedMaterial
	Members      []*plannedMaterial
	CollectionID string
}

type expandedMember struct {
	Path      string
	SHA       string
	Size      int64
	MediaType string
}

// RunIngest executes one ingest job. Expansion writes only content-addressed files;
// all catalogue changes happen in a single transaction, so a failed job publishes nothing.
func (s *Service) RunIngest(ctx context.Context, jobID string) {
	db := s.db.WithContext(ctx)
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil || job.Kind != model.JobIngest {
		return
	}
	err := s.ingest(ctx, job)
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		if !errors.Is(context.Cause(ctx), errLeaseLost) {
			s.requeueJob(job.ID, job.LeaseOwner)
		}
		return
	}
	s.logf("ingest job failed: %v", err)
	s.failJob(job.ID, job.LeaseOwner, publicMessage(err), func(tx *gorm.DB) error {
		if job.UploadID == "" {
			return nil
		}
		return tx.Model(&model.Upload{}).Where("id = ? AND job_id = ?", job.UploadID, job.ID).
			Updates(map[string]any{"status": model.UploadFailed, "updated_at": s.now()}).Error
	})
}

func (s *Service) ingest(ctx context.Context, job model.Job) error {
	db := s.db.WithContext(ctx)
	var up model.Upload
	if err := db.Where("id = ?", job.UploadID).First(&up).Error; err != nil {
		return fmt.Errorf("load upload: %w", err)
	}
	var entries []model.UploadEntry
	if err := db.Where("upload_id = ?", up.ID).Order("position ASC").Find(&entries).Error; err != nil {
		return fmt.Errorf("load entries: %w", err)
	}
	if len(entries) == 0 {
		return &archive.Rejected{Reason: "上传清单为空"}
	}

	staged := map[string]stagedBlob{}
	var top []*plannedMaterial
	var groups []*zipGroup
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.Status != model.EntryReceived {
			return &archive.Rejected{Reason: "上传文件尚未全部接收"}
		}
		container := &plannedMaterial{
			ID:         newID(),
			Title:      titleFromName(e.RelativePath),
			FileName:   baseName(e.RelativePath),
			MediaType:  e.MediaType,
			SHA:        e.SHA256,
			Size:       e.SizeBytes,
			SourcePath: e.RelativePath,
			RelPath:    e.RelativePath,
		}
		container.Role = suggestRole(container.FileName)
		addStaged(staged, container.SHA, stagedBlob{Size: container.Size, MediaType: container.MediaType, FileName: container.FileName})
		top = append(top, container)

		if e.MediaType != blob.MediaTypes[blob.KindZIP] {
			continue
		}
		members, err := s.expandPackage(e)
		if err != nil {
			return err
		}
		group := &zipGroup{Container: container, CollectionID: newID()}
		for _, m := range members {
			pm := &plannedMaterial{
				ID:         newID(),
				Title:      titleFromName(m.Path),
				FileName:   baseName(m.Path),
				MediaType:  m.MediaType,
				SHA:        m.SHA,
				Size:       m.Size,
				SourcePath: e.RelativePath + "/" + m.Path,
				ParentID:   container.ID,
				RelPath:    m.Path,
			}
			pm.Role = suggestRole(pm.FileName)
			addStaged(staged, pm.SHA, stagedBlob{Size: pm.Size, MediaType: pm.MediaType, FileName: pm.FileName})
			group.Members = append(group.Members, pm)
		}
		groups = append(groups, group)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		return s.publishIngest(tx, job, up, top, groups, staged)
	})
}

// publishIngest writes blobs, materials, collections, replica plans and job state in one transaction.
func (s *Service) publishIngest(tx *gorm.DB, job model.Job, up model.Upload, top []*plannedMaterial, groups []*zipGroup, staged map[string]stagedBlob) error {
	now := s.now()
	if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
		return fmt.Errorf("claim ingest job: %w", err)
	}
	blobIDs := make(map[string]string, len(staged))
	for sha, st := range staged {
		var existing model.Blob
		err := tx.Where("sha256 = ?", sha).First(&existing).Error
		if err == nil {
			blobIDs[sha] = existing.ID
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find blob: %w", err)
		}
		b := model.Blob{
			ID:        newID(),
			SHA256:    sha,
			SizeBytes: st.Size,
			MediaType: st.MediaType,
			FileName:  st.FileName,
			CreatedAt: now,
		}
		if err := tx.Create(&b).Error; err != nil {
			return fmt.Errorf("create blob: %w", err)
		}
		blobIDs[sha] = b.ID
	}

	subjects := model.DecodeStrings(up.SubjectsJSON)
	standalone := up.CollectionKind == "custom" && len(top) == 1 && len(groups) == 0 && !strings.Contains(top[0].RelPath, "/")
	flatZIP := up.CollectionKind == model.KindZip && len(top) == 1 && len(groups) == 1
	manifest := model.Collection{}
	if !standalone {
		manifest = model.Collection{
			ID: newID(), Title: up.Title, Description: "", Kind: up.CollectionKind,
			SubjectsJSON: up.SubjectsJSON, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&manifest).Error; err != nil {
			return fmt.Errorf("create manifest collection: %w", err)
		}
	}

	insertMaterial := func(pm *plannedMaterial) error {
		m := model.Material{
			ID:               pm.ID,
			Title:            pm.Title,
			Description:      "",
			Grade:            up.Grade,
			AcademicYear:     up.AcademicYear,
			TagsJSON:         up.TagsJSON,
			Role:             pm.Role,
			Status:           model.StatusPendingStorage,
			BlobID:           blobIDs[pm.SHA],
			FileName:         pm.FileName,
			MediaType:        pm.MediaType,
			SizeBytes:        pm.Size,
			SHA256:           pm.SHA,
			SourcePath:       pm.SourcePath,
			ParentMaterialID: pm.ParentID,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(&m).Error; err != nil {
			return fmt.Errorf("create material: %w", err)
		}
		return insertMaterialSubjects(tx, m.ID, subjects)
	}

	for _, pm := range top {
		if err := insertMaterial(pm); err != nil {
			return err
		}
	}
	for _, g := range groups {
		for _, pm := range g.Members {
			if err := insertMaterial(pm); err != nil {
				return err
			}
		}
		if flatZIP {
			continue
		}
		child := model.Collection{
			ID:           g.CollectionID,
			Title:        titleFromName(g.Container.FileName),
			Description:  "",
			Kind:         model.KindZip,
			ParentID:     manifest.ID,
			SubjectsJSON: up.SubjectsJSON,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := tx.Create(&child).Error; err != nil {
			return fmt.Errorf("create zip collection: %w", err)
		}
		members := append([]*plannedMaterial{}, g.Members...)
		sortPlanned(members)
		items := make([]model.CollectionItem, 0, len(members))
		for i, pm := range members {
			items = append(items, model.CollectionItem{
				ID:           newID(),
				CollectionID: child.ID,
				MaterialID:   pm.ID,
				Position:     i,
				Role:         pm.Role,
				RelativePath: pm.RelPath,
				CreatedAt:    now,
			})
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return fmt.Errorf("create zip items: %w", err)
			}
		}
	}

	if !standalone {
		ordered := append([]*plannedMaterial{}, top...)
		if flatZIP {
			ordered = append(ordered, groups[0].Members...)
		}
		sortPlanned(ordered)
		manifestItems := make([]model.CollectionItem, 0, len(ordered))
		for i, pm := range ordered {
			manifestItems = append(manifestItems, model.CollectionItem{
				ID: newID(), CollectionID: manifest.ID, MaterialID: pm.ID,
				Position: i, Role: pm.Role, RelativePath: pm.RelPath, CreatedAt: now,
			})
		}
		if len(manifestItems) > 0 {
			if err := tx.Create(&manifestItems).Error; err != nil {
				return fmt.Errorf("create manifest items: %w", err)
			}
		}
	}

	pol, err := s.loadPolicy(tx)
	if err != nil {
		return err
	}
	providers, err := loadProviders(tx)
	if err != nil {
		return err
	}
	for _, id := range blobIDs {
		var b model.Blob
		if err := tx.Where("id = ?", id).First(&b).Error; err != nil {
			return fmt.Errorf("load blob: %w", err)
		}
		if _, err := s.planBlobWork(tx, b, pol, providers); err != nil {
			return err
		}
	}

	if err := s.autoEnqueueAnalysesTx(tx, top, groups, now); err != nil {
		return err
	}
	indexCandidates := make([]*plannedMaterial, 0, len(top))
	indexCandidates = append(indexCandidates, top...)
	for _, group := range groups {
		indexCandidates = append(indexCandidates, group.Members...)
	}
	if err := s.autoEnqueueIndexTx(tx, indexCandidates, now); err != nil {
		return err
	}
	materialID := ""
	if standalone {
		materialID = top[0].ID
	}
	if err := tx.Model(&model.Upload{}).Where("id = ?", up.ID).
		Updates(map[string]any{"status": model.UploadCompleted, "collection_id": manifest.ID, "material_id": materialID, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("complete upload: %w", err)
	}
	if err := s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, ""); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return s.auditSystem(tx, "ingest.complete", up.ID)
}

// expandPackage validates a ZIP completely, then extracts its supported members into the CAS.
// Each member's real format must match its extension, otherwise the whole package is rejected.
func (s *Service) expandPackage(e model.UploadEntry) ([]expandedMember, error) {
	zipPath := s.blobs.Path(e.SHA256)
	members, err := archive.Inspect(zipPath, s.limits)
	if err != nil {
		return nil, err
	}
	var out []expandedMember
	err = archive.Extract(zipPath, members, func(m archive.Member, r io.Reader) error {
		tmp, err := s.blobs.NewTemp()
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		n, sum, copyErr := blob.CopyHashed(tmp, r)
		closeErr := tmp.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			_ = os.Remove(tmpPath)
			return copyErr
		}
		kind, sniffErr := blob.Sniff(tmpPath)
		expected, _ := blob.ExtensionKind(m.Path)
		if sniffErr != nil || kind != expected {
			_ = os.Remove(tmpPath)
			return &archive.Rejected{Reason: "压缩包成员格式与扩展名不一致，已拒绝整个压缩包"}
		}
		if err := s.blobs.Publish(tmpPath, sum); err != nil {
			return err
		}
		out = append(out, expandedMember{Path: m.Path, SHA: sum, Size: n, MediaType: blob.MediaTypes[kind]})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// failJob marks a job failed and optionally applies related projection writes
// in the same transaction, guarded by the job lease.
func (s *Service) failJob(jobID, owner, message string, projections ...func(*gorm.DB) error) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.finishJobTx(tx, jobID, owner, model.JobFailed, message); err != nil {
			return err
		}
		for _, projection := range projections {
			if err := projection(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, errLeaseLost) {
			s.logf("fail job: %v", err)
		}
		return false
	}
	return true
}

// requeueJob returns an interrupted job to pending and applies related
// projection resets atomically.
func (s *Service) requeueJob(jobID, owner string, projections ...func(*gorm.DB) error) bool {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.finishJobTx(tx, jobID, owner, model.JobPending, ""); err != nil {
			return err
		}
		for _, projection := range projections {
			if err := projection(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.notify()
		return true
	}
	return false
}

func addStaged(m map[string]stagedBlob, sha string, st stagedBlob) {
	if _, ok := m[sha]; !ok {
		m[sha] = st
	}
}

func sortPlanned(items []*plannedMaterial) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && paths.NaturalLess(items[j].RelPath, items[j-1].RelPath); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// titleFromName derives a display title from a path: base name without its extension.
func titleFromName(p string) string {
	base := baseName(p)
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "未命名资料"
	}
	if utf8.RuneCountInString(base) > maxTitleRunes {
		base = string([]rune(base)[:maxTitleRunes])
	}
	return base
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// suggestRole proposes a role from the file name. The result is only a suggestion; default is other.
func suggestRole(name string) string {
	lower := strings.ToLower(name)
	rules := []struct {
		role     string
		keywords []string
	}{
		{model.RoleTeacherEdition, []string{"教师版", "老师版", "教师用", "teacher edition", "teacher_edition"}},
		{model.RoleLecture, []string{"讲义", "教程", "课件", "教案", "学案", "lecture", "handout"}},
		{model.RoleQuestionsAnswers, []string{"题目+答案", "试卷+答案", "试卷答案", "含答案", "答案解析", "解析", "详解", "solution"}},
		{model.RoleAnswer, []string{"答案", "答题卡", "answer", "key"}},
		{model.RoleExam, []string{"试卷", "学生", "练习", "习题", "exam", "student"}},
	}
	for _, rule := range rules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return rule.role
			}
		}
	}
	return model.RoleOther
}
