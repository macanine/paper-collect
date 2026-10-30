package service

import (
	"context"
	"sort"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
	"gorm.io/gorm"
)

// CollectionView is a collection with its items in position order.
type CollectionView struct {
	Collection model.Collection
	Items      []CollectionItemView
}

// CollectionItemView is one placement with the material summary (no replicas).
type CollectionItemView struct {
	Item     model.CollectionItem
	Material *MaterialView
}

// CollectionInput is the body of a create request.
type CollectionInput struct {
	Title        string
	Description  string
	Kind         string
	ParentID     string
	SubjectCodes []string
}

// CollectionPatch is a partial update. Nil means "leave unchanged".
type CollectionPatch struct {
	Title        *string
	Description  *string
	Kind         *string
	ParentID     *string
	SubjectCodes *[]string
}

// ItemInput is one entry of a whole-set replacement. Nil optional fields take defaults:
// position = index in the request, role = the material's role, relative path = "".
type ItemInput struct {
	MaterialID   string
	Position     *int
	Role         *string
	RelativePath *string
}

const maxCycleDepth = 10000

// CollectionFilter narrows a collection list. ParentID and RootsOnly are
// mutually exclusive; RootsOnly lists top-level collections only.
type CollectionFilter struct {
	Q          string
	ParentID   string
	RootsOnly  bool
	MaterialID string
}

// ListCollections returns paginated collections, newest first.
func (s *Service) ListCollections(ctx context.Context, f CollectionFilter, pg PageQuery) (Page[model.Collection], error) {
	db := s.db.WithContext(ctx)
	build := func() *gorm.DB {
		tx := db.Model(&model.Collection{})
		if f.Q != "" {
			pat := likePattern(f.Q)
			tx = tx.Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(description) LIKE ? ESCAPE '!')", pat, pat)
		}
		if f.ParentID != "" {
			tx = tx.Where("parent_id = ?", f.ParentID)
		} else if f.RootsOnly {
			tx = tx.Where("parent_id = ?", "")
		}
		if f.MaterialID != "" {
			itemIDs := db.Model(&model.CollectionItem{}).Select("collection_id").Where("material_id = ?", f.MaterialID)
			tx = tx.Where("id IN (?)", itemIDs)
		}
		return tx
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return Page[model.Collection]{}, wrap("count collections", err)
	}
	items := []model.Collection{}
	if err := build().Order("created_at DESC").Order("id DESC").
		Limit(pg.PageSize).Offset(pg.offset()).Find(&items).Error; err != nil {
		return Page[model.Collection]{}, wrap("list collections", err)
	}
	return newPage(items, total, pg), nil
}

// GetCollection returns a collection with its items and material summaries.
func (s *Service) GetCollection(ctx context.Context, id string) (CollectionView, error) {
	db := s.db.WithContext(ctx)
	var c model.Collection
	if err := db.Where("id = ?", id).First(&c).Error; err != nil {
		return CollectionView{}, apperr.NotFound("合集不存在")
	}
	var items []model.CollectionItem
	if err := db.Where("collection_id = ?", id).Order("position ASC").Order("id ASC").Find(&items).Error; err != nil {
		return CollectionView{}, wrap("load items", err)
	}
	materials, err := s.materialSummaries(db, items)
	if err != nil {
		return CollectionView{}, err
	}
	views := make([]CollectionItemView, 0, len(items))
	for _, it := range items {
		item := it
		// The material is the single source of truth for the role.
		if mv := materials[item.MaterialID]; mv != nil {
			item.Role = mv.Material.Role
		}
		views = append(views, CollectionItemView{Item: item, Material: materials[item.MaterialID]})
	}
	return CollectionView{Collection: c, Items: views}, nil
}

// materialSummaries loads referenced materials (no replicas) keyed by id.
func (s *Service) materialSummaries(db *gorm.DB, items []model.CollectionItem) (map[string]*MaterialView, error) {
	out := map[string]*MaterialView{}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.MaterialID)
	}
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		var rows []model.Material
		if err := db.Where("id IN ?", ids[start:end]).Find(&rows).Error; err != nil {
			return nil, wrap("load materials", err)
		}
		chunk := make([]string, 0, len(rows))
		for _, m := range rows {
			chunk = append(chunk, m.ID)
		}
		subjects, err := s.subjectsByMaterial(db, chunk)
		if err != nil {
			return nil, err
		}
		for _, m := range rows {
			mv := MaterialView{Material: m, Subjects: subjects[m.ID]}
			out[m.ID] = &mv
		}
	}
	return out, nil
}

// CreateCollection adds a collection. A non-empty parent must exist.
func (s *Service) CreateCollection(ctx context.Context, p auth.Principal, in CollectionInput) (model.Collection, error) {
	title, err := validateTitle(in.Title)
	if err != nil {
		return model.Collection{}, err
	}
	desc, err := validateDescription(in.Description)
	if err != nil {
		return model.Collection{}, err
	}
	if err := validateCollectionKind(in.Kind); err != nil {
		return model.Collection{}, err
	}
	subjects, err := validateOptionalSubjects(in.SubjectCodes)
	if err != nil {
		return model.Collection{}, err
	}
	db := s.db.WithContext(ctx)
	if in.ParentID != "" {
		var parent model.Collection
		if err := db.Where("id = ?", in.ParentID).First(&parent).Error; err != nil {
			return model.Collection{}, apperr.Validation("父合集不存在")
		}
	}
	now := s.now()
	c := model.Collection{
		ID:           newID(),
		Title:        title,
		Description:  desc,
		Kind:         in.Kind,
		ParentID:     in.ParentID,
		SubjectsJSON: model.EncodeStrings(subjects),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return wrap("create collection", err)
		}
		return s.audit(tx, p, "collection.create", c.ID)
	})
	if err != nil {
		return model.Collection{}, err
	}
	return c, nil
}

// UpdateCollection applies a partial update and rejects parent cycles.
func (s *Service) UpdateCollection(ctx context.Context, p auth.Principal, id string, pt CollectionPatch) (model.Collection, error) {
	if pt.Title == nil && pt.Description == nil && pt.Kind == nil && pt.ParentID == nil && pt.SubjectCodes == nil {
		return model.Collection{}, apperr.Validation("至少需要提供一个可修改字段")
	}
	updates := map[string]any{"updated_at": s.now()}
	if pt.Title != nil {
		t, err := validateTitle(*pt.Title)
		if err != nil {
			return model.Collection{}, err
		}
		updates["title"] = t
	}
	if pt.Description != nil {
		d, err := validateDescription(*pt.Description)
		if err != nil {
			return model.Collection{}, err
		}
		updates["description"] = d
	}
	if pt.Kind != nil {
		if err := validateCollectionKind(*pt.Kind); err != nil {
			return model.Collection{}, err
		}
		updates["kind"] = *pt.Kind
	}
	if pt.SubjectCodes != nil {
		subjects, err := validateOptionalSubjects(*pt.SubjectCodes)
		if err != nil {
			return model.Collection{}, err
		}
		updates["subjects_json"] = model.EncodeStrings(subjects)
	}

	db := s.db.WithContext(ctx)
	var result model.Collection
	err := db.Transaction(func(tx *gorm.DB) error {
		var c model.Collection
		if err := tx.Where("id = ?", id).First(&c).Error; err != nil {
			return apperr.NotFound("合集不存在")
		}
		if pt.ParentID != nil {
			newParent := *pt.ParentID
			if newParent != "" {
				if newParent == id {
					return apperr.Conflict("父合集不能是合集自身")
				}
				cyc, err := wouldCycle(tx, id, newParent)
				if err != nil {
					return err
				}
				if cyc {
					return apperr.Conflict("父合集会形成循环")
				}
			}
			updates["parent_id"] = newParent
		}
		if err := tx.Model(&model.Collection{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return wrap("update collection", err)
		}
		if err := tx.Where("id = ?", id).First(&result).Error; err != nil {
			return wrap("reload collection", err)
		}
		return s.audit(tx, p, "collection.update", id)
	})
	if err != nil {
		return model.Collection{}, err
	}
	return result, nil
}

// wouldCycle reports whether making parent the parent of collection id creates a loop.
func wouldCycle(tx *gorm.DB, id, parent string) (bool, error) {
	cur := parent
	for depth := 0; cur != "" && depth < maxCycleDepth; depth++ {
		if cur == id {
			return true, nil
		}
		var c model.Collection
		err := tx.Select("id", "parent_id").Where("id = ?", cur).First(&c).Error
		if err != nil {
			return false, nil
		}
		cur = c.ParentID
	}
	return false, nil
}

// ReplaceItems replaces the whole item set in one transaction. An empty list clears the
// collection without deleting materials or replicas.
func (s *Service) ReplaceItems(ctx context.Context, p auth.Principal, id string, in []ItemInput) (CollectionView, error) {
	if len(in) > maxCollectionItems {
		return CollectionView{}, apperr.Validation("合集条目最多 5000 条")
	}
	db := s.db.WithContext(ctx)

	ids := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, it := range in {
		if it.MaterialID == "" {
			return CollectionView{}, apperr.Validation("material_id 不能为空")
		}
		if seen[it.MaterialID] {
			return CollectionView{}, apperr.Validation("同一合集中资料不能重复")
		}
		seen[it.MaterialID] = true
		ids = append(ids, it.MaterialID)
		if it.Position != nil && *it.Position < 0 {
			return CollectionView{}, apperr.Validation("position 必须大于等于 0")
		}
		if it.RelativePath != nil && *it.RelativePath != "" {
			if err := validateRelativePath(*it.RelativePath); err != nil {
				return CollectionView{}, err
			}
		}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		var c model.Collection
		if err := tx.Where("id = ?", id).First(&c).Error; err != nil {
			return apperr.NotFound("合集不存在")
		}
		materials := map[string]model.Material{}
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			var rows []model.Material
			if err := tx.Where("id IN ?", ids[start:end]).Find(&rows).Error; err != nil {
				return wrap("load materials", err)
			}
			for _, m := range rows {
				materials[m.ID] = m
			}
		}
		if len(materials) != len(ids) {
			return apperr.Validation("存在不存在的资料")
		}

		if err := tx.Where("collection_id = ?", id).Delete(&model.CollectionItem{}).Error; err != nil {
			return wrap("clear items", err)
		}
		now := s.now()
		rows := make([]model.CollectionItem, 0, len(in))
		for i, it := range in {
			pos := i
			if it.Position != nil {
				pos = *it.Position
			}
			// Role is owned by the material; callers cannot override it here.
			role := materials[it.MaterialID].Role
			rel := ""
			if it.RelativePath != nil {
				rel = *it.RelativePath
			}
			rows = append(rows, model.CollectionItem{
				ID:           newID(),
				CollectionID: id,
				MaterialID:   it.MaterialID,
				Position:     pos,
				Role:         role,
				RelativePath: rel,
				CreatedAt:    now,
			})
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return wrap("insert items", err)
			}
		}
		return s.audit(tx, p, "collection.items", id)
	})
	if err != nil {
		return CollectionView{}, err
	}
	return s.GetCollection(ctx, id)
}

// sortedByNatural orders relative paths naturally; used when building manifest positions.
func sortedByNatural(values []string) []string {
	out := append([]string{}, values...)
	sort.SliceStable(out, func(i, j int) bool { return paths.NaturalLess(out[i], out[j]) })
	return out
}
