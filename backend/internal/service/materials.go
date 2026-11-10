package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// MaterialFilter holds optional file-list filters. Empty fields are ignored.
type MaterialFilter struct {
	Q          string
	Stage      string
	Part       string
	PackID     string
	Status     string
	SyncStatus string
	SHA256     string
	Reason     string
}

// MaterialView is one file with its pack and, for detail views, sources and replicas.
type MaterialView struct {
	Material  model.Material
	Pack      *PackView
	Reasons   []string
	Sources   []model.MaterialSource
	Replicas  []model.Replica
	Suggested *model.Pack
}

// ReviewPackItem is the small per-file portion needed by the suite review UI.
// The full file summary lives in ReviewPackView.Pack.Members so one pack is
// serialized once instead of once per member.
type ReviewPackItem struct {
	ID        string
	Stage     string
	Reasons   []string
	Suggested *model.Pack
}

// ReviewPackView is one paginated suite in the review inbox. Items contains
// the files matching the requested stage/reason filter; Pack.Members contains
// every file so confirming the suite can finish all review members at once.
type ReviewPackView struct {
	Pack  PackView
	Items []ReviewPackItem
}

// MaterialPatch is a partial file update. Nil means "leave unchanged". PackID
// moves the file into another pack; NewPack splits it into a pack of its own.
type MaterialPatch struct {
	Part            *string
	AnswerStartPage *int
	PackID          *string
	NewPack         bool
}

func (pt MaterialPatch) empty() bool {
	return pt.Part == nil && pt.AnswerStartPage == nil && pt.PackID == nil && !pt.NewPack
}

func (s *Service) materialQuery(db *gorm.DB, f MaterialFilter) *gorm.DB {
	q := db.Model(&model.Material{})
	if f.Q != "" {
		pat := likePattern(f.Q)
		q = q.Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(original_name) LIKE ? ESCAPE '!')", pat, pat)
	}
	if f.Stage != "" {
		q = q.Where("stage = ?", f.Stage)
	}
	if f.Part != "" {
		q = q.Where("part = ?", f.Part)
	}
	if f.PackID != "" {
		q = q.Where("pack_id = ?", f.PackID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.SyncStatus != "" {
		q = q.Where("sync_status = ?", f.SyncStatus)
	}
	if f.SHA256 != "" {
		q = q.Where("sha256 = ?", strings.ToLower(f.SHA256))
	}
	if f.Reason != "" {
		q = q.Where("review_reasons_json LIKE ? ESCAPE '!'", "%"+likeEscape(`"`+f.Reason+`"`)+"%")
	}
	return q
}

// ListMaterials returns a filtered, paginated file list with each file's pack.
// The review inbox is this list with stage=review, oldest first.
func (s *Service) ListMaterials(ctx context.Context, f MaterialFilter, pg PageQuery) (Page[MaterialView], error) {
	db := s.db.WithContext(ctx)
	var total int64
	if err := s.materialQuery(db, f).Count(&total).Error; err != nil {
		return Page[MaterialView]{}, wrap("count materials", err)
	}
	order := "created_at DESC"
	if f.Stage == model.StageReview {
		order = "created_at ASC"
	}
	rows := []model.Material{}
	if err := s.materialQuery(db, f).Order(order).Order("id ASC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[MaterialView]{}, wrap("list materials", err)
	}
	items, err := s.materialViews(db, rows)
	if err != nil {
		return Page[MaterialView]{}, err
	}
	return newPage(items, total, pg), nil
}

// ListReviewPacks returns the review inbox grouped and paginated by suite.
// This keeps large suites together and avoids serializing the same suite once
// for every matching file.
func (s *Service) ListReviewPacks(ctx context.Context, stage, reason string, pg PageQuery) (Page[ReviewPackView], error) {
	db := s.db.WithContext(ctx)
	apply := func(q *gorm.DB) *gorm.DB {
		q = q.Model(&model.Material{})
		if stage != "" {
			q = q.Where("stage = ?", stage)
		}
		if reason != "" {
			q = q.Where("review_reasons_json LIKE ? ESCAPE '!'", "%"+likeEscape(`"`+reason+`"`)+"%")
		}
		return q
	}
	var total int64
	if err := apply(db).Distinct("pack_id").Count(&total).Error; err != nil {
		return Page[ReviewPackView]{}, wrap("count review packs", err)
	}
	type packRow struct {
		PackID string
	}
	var rows []packRow
	if err := apply(db).Select("pack_id").
		Group("pack_id").Order("MIN(created_at) ASC").Order("pack_id ASC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[ReviewPackView]{}, wrap("list review packs", err)
	}
	if len(rows) == 0 {
		return newPage([]ReviewPackView{}, total, pg), nil
	}
	packIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		packIDs = append(packIDs, row.PackID)
	}
	packViews, err := s.packViewsByID(db, packIDs, false)
	if err != nil {
		return Page[ReviewPackView]{}, err
	}
	var materials []model.Material
	if err := apply(db).Where("pack_id IN ?", packIDs).Order("created_at ASC").Order("id ASC").Find(&materials).Error; err != nil {
		return Page[ReviewPackView]{}, wrap("load review pack items", err)
	}
	suggestedIDs := make([]string, 0)
	seenSuggested := map[string]bool{}
	for _, material := range materials {
		if material.SuggestedPackID != "" && !seenSuggested[material.SuggestedPackID] {
			seenSuggested[material.SuggestedPackID] = true
			suggestedIDs = append(suggestedIDs, material.SuggestedPackID)
		}
	}
	suggestedViews, err := s.packViewsByID(db, suggestedIDs, false)
	if err != nil {
		return Page[ReviewPackView]{}, err
	}
	itemsByPack := make(map[string][]ReviewPackItem, len(packIDs))
	for _, material := range materials {
		var suggested *model.Pack
		if view, ok := suggestedViews[material.SuggestedPackID]; ok && material.SuggestedPackID != "" {
			copy := view.Pack
			suggested = &copy
		}
		itemsByPack[material.PackID] = append(itemsByPack[material.PackID], ReviewPackItem{
			ID: material.ID, Stage: material.Stage, Reasons: model.DecodeStrings(material.ReviewReasonsJSON), Suggested: suggested,
		})
	}
	items := make([]ReviewPackView, 0, len(packIDs))
	for _, packID := range packIDs {
		if pack, ok := packViews[packID]; ok {
			items = append(items, ReviewPackView{Pack: pack, Items: itemsByPack[packID]})
		}
	}
	return newPage(items, total, pg), nil
}

func (s *Service) materialViews(db *gorm.DB, rows []model.Material) ([]MaterialView, error) {
	packIDs := []string{}
	seen := map[string]bool{}
	for _, m := range rows {
		for _, id := range []string{m.PackID, m.SuggestedPackID} {
			if id != "" && !seen[id] {
				seen[id] = true
				packIDs = append(packIDs, id)
			}
		}
	}
	packs, err := s.packViewsByID(db, packIDs, false)
	if err != nil {
		return nil, err
	}
	items := make([]MaterialView, 0, len(rows))
	for _, m := range rows {
		view := MaterialView{Material: m, Reasons: model.DecodeStrings(m.ReviewReasonsJSON)}
		if p, ok := packs[m.PackID]; ok {
			view.Pack = &p
		}
		if p, ok := packs[m.SuggestedPackID]; ok && m.SuggestedPackID != "" {
			pack := p.Pack
			view.Suggested = &pack
		}
		items = append(items, view)
	}
	return items, nil
}

// GetMaterial returns one file with its pack, arrival sources and replicas.
// Share URLs are included; the caller must hold materials:read.
func (s *Service) GetMaterial(ctx context.Context, id string) (MaterialView, error) {
	db := s.db.WithContext(ctx)
	var m model.Material
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		return MaterialView{}, apperr.NotFound("文件不存在")
	}
	views, err := s.materialViews(db, []model.Material{m})
	if err != nil {
		return MaterialView{}, err
	}
	view := views[0]
	if err := db.Where("blob_id = ?", m.BlobID).Order("created_at ASC").Order("id ASC").Find(&view.Replicas).Error; err != nil {
		return MaterialView{}, wrap("load replicas", err)
	}
	if err := db.Where("material_id = ?", m.ID).Order("created_at ASC").Find(&view.Sources).Error; err != nil {
		return MaterialView{}, wrap("load sources", err)
	}
	return view, nil
}

// UpdateMaterial changes a file's part or moves it between packs. A manual
// edit is a human decision, so the file leaves the review inbox.
func (s *Service) UpdateMaterial(ctx context.Context, p auth.Principal, id string, pt MaterialPatch) (MaterialView, error) {
	if pt.empty() {
		return MaterialView{}, apperr.Validation("至少需要提供一个可修改字段")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		renamed, err = s.applyMaterialPatchTx(tx, id, pt, s.now())
		if err != nil {
			return err
		}
		if err := s.setStageTx(tx, id, model.StageReady, nil, ""); err != nil {
			return err
		}
		return s.audit(tx, p, "material.update", id)
	})
	if err != nil {
		return MaterialView{}, err
	}
	s.notify()
	s.renameMaterialFiles(ctx, renamed)
	return s.GetMaterial(ctx, id)
}

// applyMaterialPatchTx validates and writes file-level fields. It is shared by
// manual edits and validated AI suggestions.
func (s *Service) applyMaterialPatchTx(tx *gorm.DB, id string, pt MaterialPatch, now time.Time) ([]string, error) {
	var m model.Material
	if err := tx.Where("id = ?", id).First(&m).Error; err != nil {
		return nil, apperr.NotFound("文件不存在")
	}
	updates := map[string]any{"updated_at": now}
	if pt.Part != nil {
		if err := validatePart(*pt.Part); err != nil {
			return nil, err
		}
		updates["part"] = *pt.Part
	}
	if pt.AnswerStartPage != nil {
		if *pt.AnswerStartPage < 0 || *pt.AnswerStartPage > 10000 {
			return nil, apperr.Validation("答案起始页必须为 0 到 10000")
		}
		updates["answer_start_page"] = *pt.AnswerStartPage
	}
	if err := tx.Model(&model.Material{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, wrap("update material", err)
	}
	if pt.Part != nil && *pt.Part != m.Part {
		if err := s.syncMaterialIndexForPartTx(tx, id, *pt.Part, now); err != nil {
			return nil, wrap("sync material index state", err)
		}
	}
	touched := map[string]bool{m.PackID: true}
	switch {
	case pt.NewPack:
		target, err := s.splitIntoNewPackTx(tx, m, now)
		if err != nil {
			return nil, err
		}
		touched[target] = true
	case pt.PackID != nil && *pt.PackID != m.PackID:
		if err := s.moveMaterialTx(tx, m, *pt.PackID, now); err != nil {
			return nil, err
		}
		touched[*pt.PackID] = true
	}
	var renamed []string
	for packID := range touched {
		var exists int64
		if err := tx.Model(&model.Pack{}).Where("id = ?", packID).Count(&exists).Error; err != nil {
			return nil, wrap("check pack", err)
		}
		if exists == 0 {
			continue
		}
		ids, err := s.refreshPackNamesTx(tx, packID)
		if err != nil {
			return nil, err
		}
		renamed = append(renamed, ids...)
	}
	return renamed, nil
}

// moveMaterialTx puts a file at the end of another pack and removes the old
// pack when it becomes empty.
func (s *Service) moveMaterialTx(tx *gorm.DB, m model.Material, targetPackID string, now time.Time) error {
	var target model.Pack
	if err := tx.Where("id = ?", targetPackID).First(&target).Error; err != nil {
		return apperr.NotFound("目标套卷不存在")
	}
	var maxPos struct{ N int }
	if err := tx.Model(&model.Material{}).Select("COALESCE(MAX(position), -1) AS n").
		Where("pack_id = ?", target.ID).Scan(&maxPos).Error; err != nil {
		return wrap("pack position", err)
	}
	if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
		"pack_id": target.ID, "position": maxPos.N + 1, "suggested_pack_id": "", "updated_at": now,
	}).Error; err != nil {
		return wrap("move material", err)
	}
	if err := tx.Model(&model.Pack{}).Where("id = ?", target.ID).Update("updated_at", now).Error; err != nil {
		return wrap("touch pack", err)
	}
	return s.dropPackIfEmptyTx(tx, m.PackID)
}

// splitIntoNewPackTx moves a file into a fresh pack that copies the catalogue
// of its current pack, so a wrongly paired file keeps its subject and grade.
func (s *Service) splitIntoNewPackTx(tx *gorm.DB, m model.Material, now time.Time) (string, error) {
	var old model.Pack
	if err := tx.Where("id = ?", m.PackID).First(&old).Error; err != nil {
		return "", apperr.NotFound("套卷不存在")
	}
	fresh := old
	fresh.ID = newID()
	fresh.Title = ""
	fresh.TitleLocked = false
	fresh.ExamName = ""
	fresh.CreatedAt, fresh.UpdatedAt = now, now
	fresh.DeletedAt = gorm.DeletedAt{}
	if err := tx.Create(&fresh).Error; err != nil {
		return "", wrap("create pack", err)
	}
	if err := insertPackSubjects(tx, fresh.ID, model.DecodeStrings(fresh.SubjectsJSON)); err != nil {
		return "", err
	}
	if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
		"pack_id": fresh.ID, "position": 0, "suggested_pack_id": "", "updated_at": now,
	}).Error; err != nil {
		return "", wrap("move material", err)
	}
	return fresh.ID, s.dropPackIfEmptyTx(tx, old.ID)
}

// dropPackIfEmptyTx hard-deletes a pack that no longer has any file, together
// with its subjects and topic placements.
func (s *Service) dropPackIfEmptyTx(tx *gorm.DB, packID string) error {
	var n int64
	if err := tx.Unscoped().Model(&model.Material{}).Where("pack_id = ?", packID).Count(&n).Error; err != nil {
		return wrap("count pack members", err)
	}
	if n > 0 {
		return nil
	}
	if err := tx.Where("pack_id = ?", packID).Delete(&model.PackSubject{}).Error; err != nil {
		return wrap("delete pack subjects", err)
	}
	if err := tx.Where("pack_id = ?", packID).Delete(&model.CollectionItem{}).Error; err != nil {
		return wrap("delete pack placements", err)
	}
	if err := tx.Model(&model.Material{}).Where("suggested_pack_id = ?", packID).Update("suggested_pack_id", "").Error; err != nil {
		return wrap("clear pack suggestions", err)
	}
	return wrap("delete pack", tx.Unscoped().Where("id = ?", packID).Delete(&model.Pack{}).Error)
}

// setStageTx writes the curation stage. Ready and processing clear the inbox reasons.
func (s *Service) setStageTx(tx *gorm.DB, id, stage string, reasons []string, suggestedPackID string) error {
	if stage != model.StageReview && stage != model.StageFailed {
		reasons = nil
		suggestedPackID = ""
	}
	err := wrap("set stage", tx.Model(&model.Material{}).Where("id = ?", id).Updates(map[string]any{
		"stage": stage, "review_reasons_json": model.EncodeStrings(reasons), "suggested_pack_id": suggestedPackID,
	}).Error)
	if err != nil {
		return err
	}
	if stage == model.StageReady {
		_, err = s.planMaterialTx(tx, id, true)
	}
	return err
}

// AcceptMaterials confirms files in the review inbox as they are now.
// AcceptSuggestion additionally moves each file into its suggested pack.
func (s *Service) AcceptMaterials(ctx context.Context, p auth.Principal, ids []string, acceptSuggestion bool) (int, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, apperr.Validation("一次需要确认 1 到 500 个文件")
	}
	var renamed []string
	count := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.Material
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return wrap("load materials", err)
		}
		now := s.now()
		for _, m := range rows {
			if acceptSuggestion && m.SuggestedPackID != "" {
				var exists int64
				if err := tx.Model(&model.Pack{}).Where("id = ?", m.SuggestedPackID).Count(&exists).Error; err != nil {
					return wrap("check pack", err)
				}
				if exists > 0 {
					ids, err := s.applyMaterialPatchTx(tx, m.ID, MaterialPatch{PackID: &m.SuggestedPackID}, now)
					if err != nil {
						return err
					}
					renamed = append(renamed, ids...)
				}
			}
			if err := s.setStageTx(tx, m.ID, model.StageReady, nil, ""); err != nil {
				return err
			}
			count++
		}
		return s.audit(tx, p, "material.accept", strings.Join(ids, ","))
	})
	if err != nil {
		return 0, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return count, nil
}

func insertPackSubjects(tx *gorm.DB, packID string, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	rows := make([]model.PackSubject, 0, len(codes))
	for _, c := range codes {
		rows = append(rows, model.PackSubject{PackID: packID, Code: c})
	}
	return wrap("insert subjects", tx.Create(&rows).Error)
}

// sortSubjects orders subject codes by the fixed dictionary.
func sortSubjects(codes []string) []string {
	order := map[string]int{}
	for i, sub := range model.Subjects {
		order[sub.Code] = i
	}
	out := append([]string{}, codes...)
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}
