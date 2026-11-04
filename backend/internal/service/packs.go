package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// PackFilter holds optional library filters. Empty fields are ignored.
type PackFilter struct {
	Q            string
	Subject      string
	Grade        string
	DocType      string
	Term         string
	Cohort       int
	Region       string
	Organization string
	Tag          string
	Stage        string
	Missing      string // "answers": exam packs without any answer file
	CollectionID string
	MaterialID   string
	Content      bool // also match parsed body text
	Trashed      bool
	Sort         string // "updated" (default), "cohort", "title"
}

// PackMember is the slim file summary shown inside a pack.
type PackMember struct {
	ID              string
	Title           string
	FileName        string
	Part            string
	AnswerStartPage int
	Stage           string
	Status          string
	MediaType       string
	SizeBytes       int64
	Position        int
}

// PackView is a pack with its decoded fields, members and completeness.
type PackView struct {
	Pack         model.Pack
	Subjects     []string
	Tags         []string
	Members      []PackMember
	Stage        string
	HasQuestions bool
	HasAnswers   bool
	HasSheet     bool
	IsExam       bool
	Collections  []model.Collection
}

// PackPatch is a partial catalogue update. Nil means "leave unchanged". Any
// manual edit locks the catalogue so later analysis only fills empty fields.
type PackPatch struct {
	Title        *string // "" unlocks and returns to the canonical name
	DocType      *string
	SubjectCodes *[]string
	Grade        *string
	Term         *string
	Cohort       *int
	Region       *string
	Organization *string
	ExamDate     *string
	ExamName     *string
	Chapter      *string
	Tags         *[]string
	Description  *string
}

func (pt PackPatch) empty() bool {
	return pt.Title == nil && pt.DocType == nil && pt.SubjectCodes == nil && pt.Grade == nil &&
		pt.Term == nil && pt.Cohort == nil && pt.Region == nil && pt.Organization == nil &&
		pt.ExamDate == nil && pt.ExamName == nil && pt.Chapter == nil && pt.Tags == nil && pt.Description == nil
}

func examDocTypeList() []string {
	out := make([]string, 0, len(examDocTypes))
	for k := range examDocTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Service) packQuery(db *gorm.DB, f PackFilter) *gorm.DB {
	q := db.Model(&model.Pack{})
	if f.Trashed {
		q = db.Unscoped().Model(&model.Pack{}).Where("deleted_at IS NOT NULL")
	}
	if f.Q != "" {
		pat := likePattern(f.Q)
		cond := "(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(exam_name) LIKE ? ESCAPE '!' OR LOWER(organization) LIKE ? ESCAPE '!'" +
			" OR LOWER(region) LIKE ? ESCAPE '!' OR LOWER(chapter) LIKE ? ESCAPE '!' OR LOWER(tags_json) LIKE ? ESCAPE '!'" +
			" OR id IN (SELECT pack_id FROM materials WHERE LOWER(original_name) LIKE ? ESCAPE '!')"
		args := []any{pat, pat, pat, pat, pat, pat, pat}
		if f.Content {
			cond += " OR id IN (SELECT m.pack_id FROM materials m JOIN material_texts mt ON mt.material_id = m.id WHERE LOWER(mt.markdown) LIKE ? ESCAPE '!')"
			args = append(args, pat)
		}
		q = q.Where(cond+")", args...)
	}
	if f.Subject != "" {
		q = q.Where("id IN (?)", db.Model(&model.PackSubject{}).Select("pack_id").Where("code = ?", f.Subject))
	}
	if f.Grade != "" {
		q = q.Where("grade = ?", f.Grade)
	}
	if f.DocType != "" {
		q = q.Where("doc_type = ?", f.DocType)
	}
	if f.Term != "" {
		q = q.Where("term = ?", f.Term)
	}
	if f.Cohort != 0 {
		q = q.Where("cohort = ?", f.Cohort)
	}
	if f.Region != "" {
		q = q.Where("region = ?", f.Region)
	}
	if f.Organization != "" {
		q = q.Where("organization = ?", f.Organization)
	}
	if f.Tag != "" {
		q = q.Where("tags_json LIKE ? ESCAPE '!'", "%"+likeEscape(`"`+f.Tag+`"`)+"%")
	}
	members := db.Unscoped().Model(&model.Material{}).Select("pack_id")
	if f.Stage != "" {
		q = q.Where("id IN (?)", members.Where("stage = ?", f.Stage))
	}
	if f.Missing == "answers" {
		withAnswers := db.Unscoped().Model(&model.Material{}).Select("pack_id").
			Where("part IN ?", []string{model.PartAnswers, model.PartFull, model.PartTeacher})
		// An untyped pack counts as an exam once it holds a question paper.
		withQuestions := db.Unscoped().Model(&model.Material{}).Select("pack_id").Where("part = ?", model.PartQuestions)
		q = q.Where("(doc_type IN ? OR (doc_type = '' AND id IN (?)))", examDocTypeList(), withQuestions).Where("id NOT IN (?)", withAnswers)
	}
	if f.CollectionID != "" {
		q = q.Where("id IN (?)", db.Model(&model.CollectionItem{}).Select("pack_id").Where("collection_id = ?", f.CollectionID))
	}
	if f.MaterialID != "" {
		q = q.Where("id IN (?)", db.Unscoped().Model(&model.Material{}).Select("pack_id").Where("id = ?", f.MaterialID))
	}
	return q
}

// ListPacks is the library: filtered, paginated packs with their members.
func (s *Service) ListPacks(ctx context.Context, f PackFilter, pg PageQuery) (Page[PackView], error) {
	db := s.db.WithContext(ctx)
	var total int64
	if err := s.packQuery(db, f).Count(&total).Error; err != nil {
		return Page[PackView]{}, wrap("count packs", err)
	}
	q := s.packQuery(db, f)
	switch f.Sort {
	case "cohort":
		q = q.Order("cohort DESC").Order("exam_date DESC").Order("updated_at DESC")
	case "title":
		q = q.Order("title ASC")
	default:
		if f.Trashed {
			q = q.Order("deleted_at DESC")
		} else {
			q = q.Order("updated_at DESC")
		}
	}
	rows := []model.Pack{}
	if err := q.Order("id ASC").Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[PackView]{}, wrap("list packs", err)
	}
	ids := make([]string, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.ID)
	}
	views, err := s.packViewsByID(db, ids, f.Trashed)
	if err != nil {
		return Page[PackView]{}, err
	}
	items := make([]PackView, 0, len(rows))
	for _, p := range rows {
		items = append(items, views[p.ID])
	}
	return newPage(items, total, pg), nil
}

// packViewsByID loads packs with subjects and member summaries in bulk.
func (s *Service) packViewsByID(db *gorm.DB, ids []string, trashed bool) (map[string]PackView, error) {
	out := map[string]PackView{}
	if len(ids) == 0 {
		return out, nil
	}
	// Build a fresh chain per query: an Unscoped instance keeps conditions.
	base := func() *gorm.DB {
		if trashed {
			return db.Unscoped()
		}
		return db
	}
	for start := 0; start < len(ids); start += 400 {
		end := min(start+400, len(ids))
		chunk := ids[start:end]
		var packs []model.Pack
		if err := base().Where("id IN ?", chunk).Find(&packs).Error; err != nil {
			return nil, wrap("load packs", err)
		}
		var members []model.Material
		if err := base().Where("pack_id IN ?", chunk).Order("position ASC").Order("created_at ASC").Find(&members).Error; err != nil {
			return nil, wrap("load pack members", err)
		}
		byPack := map[string][]model.Material{}
		for _, m := range members {
			byPack[m.PackID] = append(byPack[m.PackID], m)
		}
		for _, p := range packs {
			out[p.ID] = buildPackView(p, byPack[p.ID])
		}
	}
	return out, nil
}

func buildPackView(p model.Pack, members []model.Material) PackView {
	view := PackView{
		Pack: p, Subjects: sortSubjects(model.DecodeStrings(p.SubjectsJSON)), Tags: model.DecodeStrings(p.TagsJSON),
		Members: make([]PackMember, 0, len(members)), Stage: model.StageReady, IsExam: examDocTypes[p.DocType],
	}
	parts := make([]string, 0, len(members))
	rank := map[string]int{model.StageReady: 0, model.StageProcessing: 1, model.StageReview: 2, model.StageFailed: 3}
	for _, m := range members {
		view.Members = append(view.Members, PackMember{
			ID: m.ID, Title: m.Title, FileName: m.FileName, Part: m.Part, AnswerStartPage: m.AnswerStartPage,
			Stage: m.Stage, Status: m.Status, MediaType: m.MediaType, SizeBytes: m.SizeBytes, Position: m.Position,
		})
		parts = append(parts, m.Part)
		if rank[m.Stage] > rank[view.Stage] {
			view.Stage = m.Stage
		}
	}
	view.HasQuestions, view.HasAnswers, view.HasSheet = packCompleteness(parts)
	if p.DocType == "" {
		for _, part := range parts {
			if part == model.PartQuestions || part == model.PartAnswers || part == model.PartFull || part == model.PartAnswerSheet {
				view.IsExam = true
			}
		}
	}
	return view
}

// GetPack returns one pack with members and the topics it is placed in.
func (s *Service) GetPack(ctx context.Context, id string) (PackView, error) {
	db := s.db.WithContext(ctx)
	views, err := s.packViewsByID(db, []string{id}, true)
	if err != nil {
		return PackView{}, err
	}
	view, ok := views[id]
	if !ok {
		return PackView{}, apperr.NotFound("套卷不存在")
	}
	var collectionIDs []string
	if err := db.Model(&model.CollectionItem{}).Where("pack_id = ?", id).Pluck("collection_id", &collectionIDs).Error; err != nil {
		return PackView{}, wrap("load pack topics", err)
	}
	view.Collections = []model.Collection{}
	if len(collectionIDs) > 0 {
		if err := db.Where("id IN ?", collectionIDs).Order("title ASC").Find(&view.Collections).Error; err != nil {
			return PackView{}, wrap("load topics", err)
		}
	}
	return view, nil
}

// UpdatePack applies a manual catalogue edit and renames member files.
func (s *Service) UpdatePack(ctx context.Context, p auth.Principal, id string, pt PackPatch) (PackView, error) {
	if pt.empty() {
		return PackView{}, apperr.Validation("至少需要提供一个可修改字段")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		renamed, err = s.applyPackPatchTx(tx, id, pt, true, s.now())
		if err != nil {
			return err
		}
		return s.audit(tx, p, "pack.update", id)
	})
	if err != nil {
		return PackView{}, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return s.GetPack(ctx, id)
}

// BulkUpdatePacks applies the same catalogue edit to many packs.
func (s *Service) BulkUpdatePacks(ctx context.Context, p auth.Principal, ids []string, pt PackPatch) (int, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, apperr.Validation("一次需要修改 1 到 500 个套卷")
	}
	if pt.empty() {
		return 0, apperr.Validation("至少需要提供一个可修改字段")
	}
	if pt.Title != nil {
		return 0, apperr.Validation("批量修改不能设置标题")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now()
		for _, id := range ids {
			ids, err := s.applyPackPatchTx(tx, id, pt, true, now)
			if err != nil {
				return err
			}
			renamed = append(renamed, ids...)
		}
		return s.audit(tx, p, "pack.bulk_update", strings.Join(ids, ","))
	})
	if err != nil {
		return 0, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return len(ids), nil
}

// applyPackPatchTx validates and writes pack fields. manual marks the
// catalogue as human-owned; automatic callers pass false.
func (s *Service) applyPackPatchTx(tx *gorm.DB, id string, pt PackPatch, manual bool, now time.Time) ([]string, error) {
	var pack model.Pack
	if err := tx.Where("id = ?", id).First(&pack).Error; err != nil {
		return nil, apperr.NotFound("套卷不存在")
	}
	updates := map[string]any{"updated_at": now}
	if manual {
		updates["catalog_locked"] = true
	}
	if pt.Title != nil {
		t := strings.TrimSpace(*pt.Title)
		if t == "" {
			updates["title_locked"] = false
		} else {
			v, err := validateTitle(t)
			if err != nil {
				return nil, err
			}
			updates["title"], updates["title_locked"] = v, true
		}
	}
	if pt.DocType != nil {
		if err := validateDocType(*pt.DocType); err != nil {
			return nil, err
		}
		updates["doc_type"] = *pt.DocType
	}
	var subjects []string
	if pt.SubjectCodes != nil {
		codes, err := validateOptionalSubjects(*pt.SubjectCodes)
		if err != nil {
			return nil, err
		}
		subjects = codes
		updates["subjects_json"] = model.EncodeStrings(codes)
	}
	if pt.Grade != nil {
		if err := validateGrade(*pt.Grade); err != nil {
			return nil, err
		}
		updates["grade"] = *pt.Grade
	}
	if pt.Term != nil {
		if err := validateTerm(*pt.Term); err != nil {
			return nil, err
		}
		updates["term"] = *pt.Term
	}
	if pt.Cohort != nil {
		if err := validateCohort(*pt.Cohort); err != nil {
			return nil, err
		}
		updates["cohort"] = *pt.Cohort
	}
	texts := []struct {
		value  *string
		column string
		label  string
		limit  int
	}{
		{pt.Region, "region", "地区", 64}, {pt.Organization, "organization", "来源机构", 128},
		{pt.ExamName, "exam_name", "考试名称", 255}, {pt.Chapter, "chapter", "章节", 255},
	}
	for _, t := range texts {
		if t.value == nil {
			continue
		}
		v, err := validateShortText(t.label, *t.value, t.limit)
		if err != nil {
			return nil, err
		}
		updates[t.column] = v
	}
	if pt.ExamDate != nil {
		if err := validateExamDate(*pt.ExamDate); err != nil {
			return nil, err
		}
		updates["exam_date"] = *pt.ExamDate
	}
	if pt.Tags != nil {
		tags, err := validateTags(*pt.Tags)
		if err != nil {
			return nil, err
		}
		updates["tags_json"] = model.EncodeStrings(tags)
	}
	if pt.Description != nil {
		d, err := validateDescription(*pt.Description)
		if err != nil {
			return nil, err
		}
		updates["description"] = d
	}
	if err := tx.Model(&model.Pack{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, wrap("update pack", err)
	}
	if subjects != nil {
		if err := tx.Where("pack_id = ?", id).Delete(&model.PackSubject{}).Error; err != nil {
			return nil, wrap("clear subjects", err)
		}
		if err := insertPackSubjects(tx, id, subjects); err != nil {
			return nil, err
		}
	}
	return s.refreshPackNamesTx(tx, id)
}

// MergePacks moves every file of the source packs into the target pack. Empty
// catalogue fields of the target are filled from the sources.
func (s *Service) MergePacks(ctx context.Context, p auth.Principal, targetID string, sourceIDs []string) (PackView, error) {
	if len(sourceIDs) == 0 || len(sourceIDs) > 100 {
		return PackView{}, apperr.Validation("一次需要合并 1 到 100 个套卷")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		renamed, err = s.mergePacksTx(tx, targetID, sourceIDs, s.now())
		if err != nil {
			return err
		}
		return s.audit(tx, p, "pack.merge", targetID)
	})
	if err != nil {
		return PackView{}, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return s.GetPack(ctx, targetID)
}

func (s *Service) mergePacksTx(tx *gorm.DB, targetID string, sourceIDs []string, now time.Time) ([]string, error) {
	var target model.Pack
	if err := tx.Where("id = ?", targetID).First(&target).Error; err != nil {
		return nil, apperr.NotFound("目标套卷不存在")
	}
	fill := PackPatch{}
	for _, sourceID := range sourceIDs {
		if sourceID == targetID {
			continue
		}
		var source model.Pack
		if err := tx.Where("id = ?", sourceID).First(&source).Error; err != nil {
			return nil, apperr.NotFound("待合并的套卷不存在")
		}
		fillEmptyPackFields(&fill, target, source)
		// Topic placements must follow the files before moving the last member
		// removes the now-empty source pack.
		var placements []model.CollectionItem
		if err := tx.Where("pack_id = ?", sourceID).Find(&placements).Error; err != nil {
			return nil, wrap("load source topic placements", err)
		}
		for _, item := range placements {
			var exists int64
			if err := tx.Model(&model.CollectionItem{}).
				Where("collection_id = ? AND pack_id = ?", item.CollectionID, targetID).
				Count(&exists).Error; err != nil {
				return nil, wrap("check target topic placement", err)
			}
			if exists == 0 {
				if err := tx.Create(&model.CollectionItem{
					ID: newID(), CollectionID: item.CollectionID, PackID: targetID,
					Position: item.Position, CreatedAt: now,
				}).Error; err != nil {
					return nil, wrap("move topic placement", err)
				}
			}
		}
		var members []model.Material
		if err := tx.Where("pack_id = ?", sourceID).Order("position ASC").Find(&members).Error; err != nil {
			return nil, wrap("load members", err)
		}
		for _, m := range members {
			if err := s.moveMaterialTx(tx, m, targetID, now); err != nil {
				return nil, err
			}
		}
		if err := s.dropPackIfEmptyTx(tx, sourceID); err != nil {
			return nil, err
		}
	}
	if !fill.empty() {
		return s.applyPackPatchTx(tx, targetID, fill, false, now)
	}
	return s.refreshPackNamesTx(tx, targetID)
}

// fillEmptyPackFields copies fields the target lacks from source into patch.
func fillEmptyPackFields(patch *PackPatch, target, source model.Pack) {
	setStr := func(dst **string, have, from string) {
		if have == "" && from != "" && *dst == nil {
			v := from
			*dst = &v
		}
	}
	setStr(&patch.DocType, target.DocType, source.DocType)
	setStr(&patch.Grade, target.Grade, source.Grade)
	setStr(&patch.Term, target.Term, source.Term)
	setStr(&patch.Region, target.Region, source.Region)
	setStr(&patch.Organization, target.Organization, source.Organization)
	setStr(&patch.ExamDate, target.ExamDate, source.ExamDate)
	setStr(&patch.ExamName, target.ExamName, source.ExamName)
	setStr(&patch.Chapter, target.Chapter, source.Chapter)
	if target.Cohort == 0 && source.Cohort != 0 && patch.Cohort == nil {
		v := source.Cohort
		patch.Cohort = &v
	}
	if len(model.DecodeStrings(target.SubjectsJSON)) == 0 && patch.SubjectCodes == nil {
		if codes := model.DecodeStrings(source.SubjectsJSON); len(codes) > 0 {
			patch.SubjectCodes = &codes
		}
	}
}

// ReorderPack sets the order of files inside a pack.
func (s *Service) ReorderPack(ctx context.Context, p auth.Principal, id string, materialIDs []string) (PackView, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current []string
		if err := tx.Model(&model.Material{}).Where("pack_id = ?", id).Pluck("id", &current).Error; err != nil {
			return wrap("load members", err)
		}
		if len(current) != len(materialIDs) {
			return apperr.Validation("排序必须包含套卷中的全部文件")
		}
		members := map[string]bool{}
		for _, m := range current {
			members[m] = true
		}
		for i, mid := range materialIDs {
			if !members[mid] {
				return apperr.Validation("排序中包含不属于该套卷的文件")
			}
			delete(members, mid)
			if err := tx.Model(&model.Material{}).Where("id = ?", mid).Update("position", i).Error; err != nil {
				return wrap("reorder", err)
			}
		}
		if len(members) > 0 {
			return apperr.Validation("排序中有重复文件")
		}
		return s.audit(tx, p, "pack.reorder", id)
	})
	if err != nil {
		return PackView{}, err
	}
	return s.GetPack(ctx, id)
}

// FacetValue is one distinct catalogue value with the number of packs using it.
type FacetValue struct {
	Value string
	Count int64
}

// PackFacets lists the values present in the library, for filter menus.
type PackFacets struct {
	Subjects      []FacetValue
	Grades        []FacetValue
	DocTypes      []FacetValue
	Cohorts       []FacetValue
	Regions       []FacetValue
	Organizations []FacetValue
	Tags          []FacetValue
}

// GetPackFacets counts packs by each filterable field.
func (s *Service) GetPackFacets(ctx context.Context) (PackFacets, error) {
	db := s.db.WithContext(ctx)
	count := func(column string) ([]FacetValue, error) {
		var rows []FacetValue
		err := db.Model(&model.Pack{}).Select(column + " AS value, COUNT(*) AS count").
			Where(column + " <> ''").Group(column).Order("count DESC").Limit(200).Scan(&rows).Error
		return rows, wrap("facet "+column, err)
	}
	var out PackFacets
	var err error
	if out.Grades, err = count("grade"); err != nil {
		return out, err
	}
	if out.DocTypes, err = count("doc_type"); err != nil {
		return out, err
	}
	if out.Regions, err = count("region"); err != nil {
		return out, err
	}
	if out.Organizations, err = count("organization"); err != nil {
		return out, err
	}
	var cohorts []struct {
		Cohort int
		Count  int64
	}
	if err := db.Model(&model.Pack{}).Select("cohort, COUNT(*) AS count").Where("cohort <> 0").
		Group("cohort").Order("cohort DESC").Scan(&cohorts).Error; err != nil {
		return out, wrap("facet cohort", err)
	}
	for _, c := range cohorts {
		out.Cohorts = append(out.Cohorts, FacetValue{Value: strconv.Itoa(c.Cohort), Count: c.Count})
	}
	if err := db.Model(&model.PackSubject{}).Select("code AS value, COUNT(*) AS count").
		Where("pack_id IN (?)", db.Model(&model.Pack{}).Select("id")).
		Group("code").Order("count DESC").Scan(&out.Subjects).Error; err != nil {
		return out, wrap("facet subject", err)
	}
	var tagRows []string
	if err := db.Model(&model.Pack{}).Where("tags_json <> '[]'").Limit(5000).Pluck("tags_json", &tagRows).Error; err != nil {
		return out, wrap("facet tags", err)
	}
	tagCounts := map[string]int64{}
	for _, raw := range tagRows {
		for _, t := range model.DecodeStrings(raw) {
			tagCounts[t]++
		}
	}
	for t, n := range tagCounts {
		out.Tags = append(out.Tags, FacetValue{Value: t, Count: n})
	}
	sort.Slice(out.Tags, func(i, j int) bool {
		if out.Tags[i].Count != out.Tags[j].Count {
			return out.Tags[i].Count > out.Tags[j].Count
		}
		return out.Tags[i].Value < out.Tags[j].Value
	})
	if len(out.Tags) > 100 {
		out.Tags = out.Tags[:100]
	}
	return out, nil
}
