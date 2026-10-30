package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	groupCandidateLimit = 1000
	groupAutoScore      = 85
	groupAutoMargin     = 10
	groupMinTitleRunes  = 4
)

var groupRoleWords = []string{
	"参考答案", "答案及解析", "答案解析", "试卷答案",
	"教师版", "老师版", "学生版", "解析版", "原卷版", "word版", "pdf版",
	"答题卡", "含答案", "试题", "试卷", "答案", "解析", "详解",
	"teacher edition", "student edition", "answer key", "answer",
}

var groupFileExtensions = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".ppt": true, ".pptx": true,
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true,
	".tif": true, ".tiff": true, ".zip": true,
}

type examCollectionCandidate struct {
	Collection model.Collection
	Score      int
	TitleMatch string
}

// autoEnqueueExamAssignmentTx queues grouping only for a completed single-file
// upload whose analyzed material is an exam and is not yet in a collection.
func (s *Service) autoEnqueueExamAssignmentTx(tx *gorm.DB, materialID string) error {
	var material model.Material
	if err := tx.Where("id = ?", materialID).First(&material).Error; err != nil {
		return fmt.Errorf("load material for grouping: %w", err)
	}
	if material.Role != model.RoleExam {
		return nil
	}
	var collectionItems int64
	if err := tx.Model(&model.CollectionItem{}).Where("material_id = ?", materialID).
		Count(&collectionItems).Error; err != nil {
		return fmt.Errorf("count material collections: %w", err)
	}
	if collectionItems > 0 {
		return nil
	}
	upload, ok, err := s.singleFileUploadTx(tx, materialID)
	if err != nil || !ok {
		return err
	}

	var existing model.Job
	err = tx.Where("material_id = ? AND kind = ?", materialID, model.JobGroup).
		Order("created_at DESC").First(&existing).Error
	if err == nil {
		if existing.Status != model.JobFailed && existing.Status != model.JobSucceeded {
			return nil
		}
		now := s.now()
		return tx.Model(&model.Job{}).Where("id = ?", existing.ID).
			Updates(map[string]any{
				"status": model.JobPending, "error": "", "progress_message": "重新排队归类",
				"progress_current": 0, "progress_total": 0,
				"started_at": nil, "finished_at": nil, "duration_ms": 0, "updated_at": now,
				"log_json": appendJobLog(existing.LogJSON, model.JobLog{
					Time: now, Level: "info", Message: "重新排队归类",
				}),
			}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load existing group job: %w", err)
	}
	now := s.now()
	groupJob := model.Job{
		ID: newID(), Kind: model.JobGroup, Status: model.JobPending,
		UploadID: upload.ID, MaterialID: materialID, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&groupJob).Error; err != nil {
		return fmt.Errorf("create group job: %w", err)
	}
	return nil
}

// RunGroup assigns one standalone, AI-classified exam to an existing
// series/exam collection. It never creates or restructures a collection.
func (s *Service) RunGroup(ctx context.Context, jobID string) {
	db := s.db.WithContext(ctx)
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil || job.Kind != model.JobGroup {
		return
	}
	err := s.assignSingleExam(ctx, job)
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		if !errors.Is(context.Cause(ctx), errLeaseLost) {
			s.requeueJob(job.ID, job.LeaseOwner)
		}
		return
	}
	s.logf("group job failed: %v", err)
	s.failJob(job.ID, job.LeaseOwner, publicMessage(err))
}

func (s *Service) assignSingleExam(ctx context.Context, job model.Job) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return fmt.Errorf("claim group job: %w", err)
		}

		var material model.Material
		if err := tx.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
			return fmt.Errorf("load material: %w", err)
		}
		if material.Role != model.RoleExam {
			return s.finishGroupTx(tx, job, "资料角色不是试卷，保持独立", nil)
		}

		var existingItems int64
		if err := tx.Model(&model.CollectionItem{}).Where("material_id = ?", material.ID).
			Count(&existingItems).Error; err != nil {
			return fmt.Errorf("count material collections: %w", err)
		}
		if existingItems > 0 {
			return s.finishGroupTx(tx, job, "资料已在合集中，无需重复归类", nil)
		}

		upload, ok, err := s.singleFileUploadTx(tx, material.ID)
		if err != nil {
			return err
		}
		if !ok {
			return s.finishGroupTx(tx, job, "仅自动归类单文件上传的试卷", nil)
		}

		subjects, err := s.subjectsByMaterial(tx, []string{material.ID})
		if err != nil {
			return err
		}
		candidates, err := s.examCollectionCandidatesTx(tx, material, subjects[material.ID])
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return s.finishGroupTx(tx, job, "没有找到匹配的系列或试卷合集，保持独立", nil)
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Score != candidates[j].Score {
				return candidates[i].Score > candidates[j].Score
			}
			return candidates[i].Collection.UpdatedAt.After(candidates[j].Collection.UpdatedAt)
		})
		best := candidates[0]
		if best.Score < groupAutoScore ||
			(len(candidates) > 1 && best.Score-candidates[1].Score < groupAutoMargin) {
			return s.finishGroupTx(tx, job, "存在多个接近候选或匹配置信度不足，未自动归类", map[string]any{
				"candidate_count": len(candidates),
				"score":           best.Score,
			})
		}

		var target model.Collection
		query := tx.Where("id = ?", best.Collection.ID)
		if tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&target).Error; err != nil {
			return fmt.Errorf("load target collection: %w", err)
		}
		if target.Kind != model.KindSeries && target.Kind != model.KindExam {
			return s.finishGroupTx(tx, job, "目标合集类型不允许自动追加", nil)
		}
		members, memberSubjects, err := s.collectionMembersTx(tx, target.ID)
		if err != nil {
			return err
		}
		lockedScore, titleMatch, valid := scoreExamCollection(
			material, subjects[material.ID], target, members, memberSubjects,
		)
		if !valid || lockedScore < groupAutoScore {
			return s.finishGroupTx(tx, job, "目标合集在归类期间发生变化，未自动归类", nil)
		}

		var duplicate int64
		if err := tx.Model(&model.CollectionItem{}).
			Where("collection_id = ? AND material_id = ?", target.ID, material.ID).
			Count(&duplicate).Error; err != nil {
			return fmt.Errorf("check target item: %w", err)
		}
		if duplicate > 0 {
			return s.finishGroupTx(tx, job, "资料已在目标合集中，无需重复归类", nil)
		}

		var maxPosition int
		if err := tx.Model(&model.CollectionItem{}).Select("COALESCE(MAX(position), -1)").
			Where("collection_id = ?", target.ID).Scan(&maxPosition).Error; err != nil {
			return fmt.Errorf("load collection position: %w", err)
		}
		now := s.now()
		item := model.CollectionItem{
			ID:           newID(),
			CollectionID: target.ID,
			MaterialID:   material.ID,
			Position:     maxPosition + 1,
			Role:         material.Role,
			RelativePath: material.FileName,
			CreatedAt:    now,
		}
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
		if inserted.Error != nil {
			return fmt.Errorf("append collection item: %w", inserted.Error)
		}
		if inserted.RowsAffected == 1 {
			if err := tx.Model(&model.Collection{}).Where("id = ?", target.ID).
				Update("updated_at", now).Error; err != nil {
				return fmt.Errorf("touch target collection: %w", err)
			}
			if err := tx.Model(&model.Upload{}).
				Where("id = ? AND collection_id = ?", upload.ID, "").
				Update("collection_id", target.ID).Error; err != nil {
				return fmt.Errorf("link upload collection: %w", err)
			}
			if err := s.auditSystem(tx, "collection.auto_assign", target.ID); err != nil {
				return err
			}
		}

		message := fmt.Sprintf("已将试卷归入合集「%s」", target.Title)
		return s.finishGroupTx(tx, job, message, map[string]any{
			"candidate_count": len(candidates),
			"score":           lockedScore,
			"collection_id":   target.ID,
			"title_match":     titleMatch,
		})
	})
}

func (s *Service) finishGroupTx(tx *gorm.DB, job model.Job, message string, metrics map[string]any) error {
	return s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded, message, metrics)
}

func (s *Service) singleFileUploadTx(tx *gorm.DB, materialID string) (model.Upload, bool, error) {
	var upload model.Upload
	err := tx.Where("material_id = ? AND status = ?", materialID, model.UploadCompleted).
		Order("created_at DESC").First(&upload).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Upload{}, false, nil
	}
	if err != nil {
		return model.Upload{}, false, fmt.Errorf("load upload: %w", err)
	}
	if upload.CollectionID != "" {
		return model.Upload{}, false, nil
	}
	if upload.CollectionKind != model.KindCustom {
		return model.Upload{}, false, nil
	}
	var entries []model.UploadEntry
	if err := tx.Where("upload_id = ?", upload.ID).Order("position ASC").Find(&entries).Error; err != nil {
		return model.Upload{}, false, fmt.Errorf("load upload entries: %w", err)
	}
	if len(entries) != 1 {
		return model.Upload{}, false, nil
	}
	entry := entries[0]
	if entry.MediaType == blob.MediaTypes[blob.KindZIP] ||
		strings.HasSuffix(strings.ToLower(entry.RelativePath), ".zip") ||
		strings.Contains(entry.RelativePath, "/") {
		return model.Upload{}, false, nil
	}
	return upload, true, nil
}

func (s *Service) examCollectionCandidatesTx(tx *gorm.DB, material model.Material, subjects []string) ([]examCollectionCandidate, error) {
	var collections []model.Collection
	if err := tx.Where("kind IN ?", []string{model.KindSeries, model.KindExam}).
		Order("updated_at DESC").Limit(groupCandidateLimit).Find(&collections).Error; err != nil {
		return nil, fmt.Errorf("load candidate collections: %w", err)
	}
	if len(collections) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(collections))
	for _, collection := range collections {
		ids = append(ids, collection.ID)
	}
	var items []model.CollectionItem
	if err := tx.Where("collection_id IN ?", ids).
		Order("collection_id ASC").Order("position ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("load collection items: %w", err)
	}
	materialIDs := make([]string, 0, len(items))
	seenMaterials := map[string]bool{}
	for _, item := range items {
		if !seenMaterials[item.MaterialID] {
			seenMaterials[item.MaterialID] = true
			materialIDs = append(materialIDs, item.MaterialID)
		}
	}
	materialByID := map[string]model.Material{}
	if len(materialIDs) > 0 {
		var members []model.Material
		if err := tx.Where("id IN ?", materialIDs).Find(&members).Error; err != nil {
			return nil, fmt.Errorf("load collection materials: %w", err)
		}
		for _, member := range members {
			materialByID[member.ID] = member
		}
	}
	memberSubjects, err := s.subjectsByMaterial(tx, materialIDs)
	if err != nil {
		return nil, err
	}
	membersByCollection := map[string][]model.Material{}
	for _, item := range items {
		if member, ok := materialByID[item.MaterialID]; ok {
			membersByCollection[item.CollectionID] = append(membersByCollection[item.CollectionID], member)
		}
	}

	candidates := make([]examCollectionCandidate, 0, len(collections))
	for _, collection := range collections {
		score, titleMatch, ok := scoreExamCollection(
			material, subjects, collection, membersByCollection[collection.ID], memberSubjects,
		)
		if ok {
			candidates = append(candidates, examCollectionCandidate{
				Collection: collection, Score: score, TitleMatch: titleMatch,
			})
		}
	}
	return candidates, nil
}

func (s *Service) collectionMembersTx(tx *gorm.DB, collectionID string) ([]model.Material, map[string][]string, error) {
	var items []model.CollectionItem
	if err := tx.Where("collection_id = ?", collectionID).
		Order("position ASC").Find(&items).Error; err != nil {
		return nil, nil, fmt.Errorf("load collection items: %w", err)
	}
	materialIDs := make([]string, 0, len(items))
	for _, item := range items {
		materialIDs = append(materialIDs, item.MaterialID)
	}
	members := []model.Material{}
	if len(materialIDs) > 0 {
		if err := tx.Where("id IN ?", materialIDs).Find(&members).Error; err != nil {
			return nil, nil, fmt.Errorf("load collection materials: %w", err)
		}
	}
	memberSubjects, err := s.subjectsByMaterial(tx, materialIDs)
	if err != nil {
		return nil, nil, err
	}
	return members, memberSubjects, nil
}

func scoreExamCollection(
	material model.Material,
	subjects []string,
	collection model.Collection,
	members []model.Material,
	memberSubjects map[string][]string,
) (int, string, bool) {
	materialKey := normalizeGroupTitle(material.Title)
	if fileKey := normalizeGroupTitle(material.FileName); utf8.RuneCountInString(fileKey) > utf8.RuneCountInString(materialKey) {
		materialKey = fileKey
	}
	if utf8.RuneCountInString(materialKey) < groupMinTitleRunes {
		return 0, "", false
	}
	hasExamLike := false
	for _, member := range members {
		if member.SHA256 == material.SHA256 {
			return 0, "", false
		}
		if isExamLikeRole(member.Role) {
			hasExamLike = true
		}
		if member.Role == model.RoleExam && normalizeGroupTitle(member.Title) == materialKey {
			return 0, "", false
		}
	}
	if collection.Kind == model.KindExam && hasExamLike {
		return 0, "", false
	}

	score := 0
	titleMatch := ""
	if key := normalizeGroupTitle(collection.Title); key != "" {
		score = titleMatchScore(key, materialKey, 80, 60)
	}
	if score > 0 {
		titleMatch = "collection_title"
	}
	for _, member := range members {
		key := normalizeGroupTitle(member.Title)
		if memberScore := titleMatchScore(key, materialKey, 75, 55); memberScore > score {
			score = memberScore
			titleMatch = "material_title"
		}
	}
	if score == 0 {
		return 0, "", false
	}

	collectionSubjects := model.DecodeStrings(collection.SubjectsJSON)
	if len(collectionSubjects) == 0 {
		for _, member := range members {
			collectionSubjects = append(collectionSubjects, memberSubjects[member.ID]...)
		}
	}
	if len(subjects) > 0 && len(collectionSubjects) > 0 {
		if !intersects(subjects, collectionSubjects) {
			return 0, "", false
		}
		score += 15
	}

	if material.Grade != "" {
		gradeMatch := false
		hasGrade := false
		for _, member := range members {
			if member.Grade == "" {
				continue
			}
			hasGrade = true
			if member.Grade == material.Grade {
				gradeMatch = true
			}
		}
		if hasGrade && !gradeMatch {
			return 0, "", false
		}
		if gradeMatch {
			score += 4
		}
	}

	if material.AcademicYear != 0 {
		yearExact, yearNear, hasYear := false, false, false
		for _, member := range members {
			if member.AcademicYear == 0 {
				continue
			}
			hasYear = true
			if member.AcademicYear == material.AcademicYear {
				yearExact = true
			}
			if absInt(member.AcademicYear-material.AcademicYear) <= 1 {
				yearNear = true
			}
		}
		if hasYear && !yearNear {
			return 0, "", false
		}
		if yearExact {
			score += 5
		} else if yearNear {
			score += 3
		}
	}

	for _, member := range members {
		if member.Role == model.RoleAnswer || member.Role == model.RoleQuestionsAnswers || member.Role == model.RoleTeacherEdition {
			score += 3
			break
		}
	}
	return score, titleMatch, true
}

func titleMatchScore(candidate, target string, exact, contains int) int {
	if candidate == "" || utf8.RuneCountInString(candidate) < groupMinTitleRunes {
		return 0
	}
	if candidate == target {
		return exact
	}
	if utf8.RuneCountInString(target) >= 6 &&
		(strings.Contains(candidate, target) || strings.Contains(target, candidate)) {
		return contains
	}
	return 0
}

func normalizeGroupTitle(raw string) string {
	name := strings.TrimSpace(baseName(raw))
	if ext := strings.ToLower(path.Ext(name)); groupFileExtensions[ext] {
		name = name[:len(name)-len(ext)]
	}
	name = strings.ToLower(name)
	for _, word := range groupRoleWords {
		name = strings.ReplaceAll(name, word, "")
	}
	var out strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func isExamLikeRole(role string) bool {
	return role == model.RoleExam || role == model.RoleQuestionsAnswers || role == model.RoleTeacherEdition
}

func intersects(left, right []string) bool {
	set := make(map[string]bool, len(left))
	for _, value := range left {
		set[value] = true
	}
	for _, value := range right {
		if set[value] {
			return true
		}
	}
	return false
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
