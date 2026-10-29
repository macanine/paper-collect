package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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
	ArchivePath  string
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
	s.jobProgress(ctx, job, "读取上传清单并检查文件", 0, 0)
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
	skipped := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.Status != model.EntryReceived {
			return &archive.Rejected{Reason: "上传文件尚未全部接收"}
		}
		if e.MediaType == blob.MediaTypes[blob.KindZIP] {
			s.jobProgress(ctx, job, fmt.Sprintf("正在检查并展开压缩包（第 %d / %d 个文件）", i+1, len(entries)), i, len(entries))
			members, skippedMembers, err := s.expandPackage(ctx, job, e)
			if err != nil {
				return err
			}
			skipped += skippedMembers
			group := &zipGroup{ArchivePath: e.RelativePath, CollectionID: newID()}
			for _, member := range members {
				pm := &plannedMaterial{
					ID: newID(), Title: titleFromName(member.Path), FileName: baseName(member.Path),
					MediaType: member.MediaType, SHA: member.SHA, Size: member.Size,
					SourcePath: e.RelativePath + "/" + member.Path, RelPath: member.Path,
				}
				pm.Role = suggestRole(pm.FileName)
				addStaged(staged, pm.SHA, stagedBlob{Size: pm.Size, MediaType: pm.MediaType, FileName: pm.FileName})
				group.Members = append(group.Members, pm)
			}
			groups = append(groups, group)
			continue
		}
		pm := &plannedMaterial{
			ID: newID(), Title: titleFromName(e.RelativePath), FileName: baseName(e.RelativePath),
			MediaType: e.MediaType, SHA: e.SHA256, Size: e.SizeBytes,
			SourcePath: e.RelativePath, RelPath: e.RelativePath,
		}
		pm.Role = suggestRole(pm.FileName)
		addStaged(staged, pm.SHA, stagedBlob{Size: pm.Size, MediaType: pm.MediaType, FileName: pm.FileName})
		top = append(top, pm)
		if (i+1)%25 == 0 || i+1 == len(entries) {
			s.jobProgress(ctx, job, fmt.Sprintf("已检查 %d / %d 个上传文件", i+1, len(entries)), i+1, len(entries))
		}
	}
	if skipped > 0 {
		s.jobProgress(ctx, job, fmt.Sprintf("压缩包中共有 %d 个不支持的文件，已跳过，未归档", skipped), len(entries), len(entries))
	}
	s.jobProgress(ctx, job, fmt.Sprintf("文件检查完成，正在保存资料并安排存储（%d 个文件，%d 个压缩包）", len(entries), len(groups)), len(entries), len(entries))

	err := db.Transaction(func(tx *gorm.DB) error {
		return s.publishIngest(tx, job, up, top, groups, staged)
	})
	if err != nil {
		return err
	}
	// Once the catalogue transaction has committed, new ZIP uploads have no
	// remaining consumers. Old uploads in the CAS are never removed here.
	for _, e := range entries {
		if e.MediaType == blob.MediaTypes[blob.KindZIP] {
			if err := os.Remove(s.archiveUploadPath(e.ID, e.SHA256)); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.logf("remove temporary archive: %v", err)
			}
		}
	}
	return nil
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
	flatZIP := up.CollectionKind == model.KindZip && len(top) == 0 && len(groups) == 1
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
			Title:        titleFromName(g.ArchivePath),
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
			if err := tx.CreateInBatches(&items, 250).Error; err != nil {
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
			if err := tx.CreateInBatches(&manifestItems, 250).Error; err != nil {
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

	candidates := append([]*plannedMaterial(nil), top...)
	for _, group := range groups {
		candidates = append(candidates, group.Members...)
	}
	if err := s.autoEnqueueAnalysesTx(tx, candidates, now); err != nil {
		return err
	}
	if err := s.autoEnqueueIndexTx(tx, candidates, now); err != nil {
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
func (s *Service) expandPackage(ctx context.Context, job model.Job, e model.UploadEntry) ([]expandedMember, int, error) {
	zipPath := s.archiveUploadPath(e.ID, e.SHA256)
	if _, err := os.Stat(zipPath); errors.Is(err, os.ErrNotExist) {
		// Uploads received before this change live in the CAS. They may also
		// be referenced by historical materials, so leave them untouched.
		zipPath = s.blobs.Path(e.SHA256)
	} else if err != nil {
		return nil, 0, err
	}
	members, skipped, err := archive.InspectWithSkipped(zipPath, s.limits)
	if err != nil {
		return nil, 0, err
	}
	if len(members) == 0 {
		return nil, skipped, &archive.Rejected{Reason: "压缩包中没有支持归档的文件"}
	}
	s.jobProgress(ctx, job, fmt.Sprintf("压缩包检查通过，准备展开 %d 个受支持的文件", len(members)), 0, len(members))
	var out []expandedMember
	err = archive.Extract(zipPath, members, func(m archive.Member, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		tmp, err := s.blobs.NewTemp()
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		n, sum, copyErr := blob.CopyHashed(tmp, ingestContextReader{ctx: ctx, reader: r})
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
		if len(out)%25 == 0 || len(out) == len(members) {
			s.jobProgress(ctx, job, fmt.Sprintf("压缩包展开进度：%d / %d 个文件", len(out), len(members)), len(out), len(members))
		}
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	return out, skipped, nil
}

type ingestContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r ingestContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
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
	sort.SliceStable(items, func(i, j int) bool { return paths.NaturalLess(items[i].RelPath, items[j].RelPath) })
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
