package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/paths"
	"gorm.io/gorm"
)

// CollectionFilter holds optional topic-list filters.
type CollectionFilter struct {
	Q      string
	PackID string
}

// CollectionSummary is a topic with its pack count.
type CollectionSummary struct {
	Collection model.Collection
	PackCount  int64
}

// CollectionView is a topic with its packs in order.
type CollectionView struct {
	Collection model.Collection
	Packs      []PackView
}

// CollectionInput creates a topic.
type CollectionInput struct {
	Title       string
	Description string
	PackIDs     []string
}

// CollectionPatch is a partial topic update.
type CollectionPatch struct {
	Title       *string
	Description *string
}

// ListCollections lists topics, newest first.
func (s *Service) ListCollections(ctx context.Context, f CollectionFilter, pg PageQuery) (Page[CollectionSummary], error) {
	db := s.db.WithContext(ctx)
	q := db.Model(&model.Collection{})
	if f.Q != "" {
		pat := likePattern(f.Q)
		q = q.Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(description) LIKE ? ESCAPE '!')", pat, pat)
	}
	if f.PackID != "" {
		q = q.Where("id IN (?)", db.Model(&model.CollectionItem{}).Select("collection_id").Where("pack_id = ?", f.PackID))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return Page[CollectionSummary]{}, wrap("count topics", err)
	}
	var rows []model.Collection
	if err := q.Order("updated_at DESC").Order("id ASC").Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[CollectionSummary]{}, wrap("list topics", err)
	}
	items := make([]CollectionSummary, 0, len(rows))
	for _, c := range rows {
		var n int64
		if err := db.Model(&model.CollectionItem{}).Where("collection_id = ?", c.ID).
			Where("pack_id IN (?)", db.Model(&model.Pack{}).Select("id")).Count(&n).Error; err != nil {
			return Page[CollectionSummary]{}, wrap("count topic packs", err)
		}
		items = append(items, CollectionSummary{Collection: c, PackCount: n})
	}
	return newPage(items, total, pg), nil
}

// GetCollection returns a topic with its packs in order. Trashed packs are hidden.
func (s *Service) GetCollection(ctx context.Context, id string) (CollectionView, error) {
	db := s.db.WithContext(ctx)
	var c model.Collection
	if err := db.Where("id = ?", id).First(&c).Error; err != nil {
		return CollectionView{}, apperr.NotFound("专题不存在")
	}
	var items []model.CollectionItem
	if err := db.Where("collection_id = ?", id).Order("position ASC").Find(&items).Error; err != nil {
		return CollectionView{}, wrap("load topic items", err)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.PackID)
	}
	packs, err := s.packViewsByID(db, ids, false)
	if err != nil {
		return CollectionView{}, err
	}
	view := CollectionView{Collection: c, Packs: []PackView{}}
	for _, it := range items {
		if p, ok := packs[it.PackID]; ok {
			view.Packs = append(view.Packs, p)
		}
	}
	return view, nil
}

// CreateCollection creates a topic, optionally with packs.
func (s *Service) CreateCollection(ctx context.Context, p auth.Principal, in CollectionInput) (CollectionView, error) {
	title, err := validateTitle(in.Title)
	if err != nil {
		return CollectionView{}, err
	}
	desc, err := validateDescription(in.Description)
	if err != nil {
		return CollectionView{}, err
	}
	now := s.now()
	c := model.Collection{ID: newID(), Title: title, Description: desc, Source: model.CollectionSourceHuman,
		Key: collectionKey(title), CreatedAt: now, UpdatedAt: now}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Collection{}).Where("key = ?", c.Key).Count(&n).Error; err != nil {
			return wrap("check topic name", err)
		}
		if n > 0 {
			return apperr.Validation("已有同名专题")
		}
		if err := tx.Create(&c).Error; err != nil {
			return wrap("create topic", err)
		}
		if err := s.addPacksTx(tx, c.ID, in.PackIDs); err != nil {
			return err
		}
		return s.audit(tx, p, "collection.create", c.ID)
	})
	if err != nil {
		return CollectionView{}, err
	}
	return s.GetCollection(ctx, c.ID)
}

// UpdateCollection edits a topic's title or description.
func (s *Service) UpdateCollection(ctx context.Context, p auth.Principal, id string, pt CollectionPatch) (CollectionView, error) {
	if pt.Title == nil && pt.Description == nil {
		return CollectionView{}, apperr.Validation("至少需要提供一个可修改字段")
	}
	// A person edited the topic: AI stops adding packs to it from now on. The
	// key stays as it was so a rename does not make the AI start a new topic.
	updates := map[string]any{"updated_at": s.now(), "locked": true}
	if pt.Title != nil {
		t, err := validateTitle(*pt.Title)
		if err != nil {
			return CollectionView{}, err
		}
		updates["title"] = t
	}
	if pt.Description != nil {
		d, err := validateDescription(*pt.Description)
		if err != nil {
			return CollectionView{}, err
		}
		updates["description"] = d
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Collection{}).Where("id = ?", id).Updates(updates)
		if res.Error != nil {
			return wrap("update topic", res.Error)
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("专题不存在")
		}
		return s.audit(tx, p, "collection.update", id)
	})
	if err != nil {
		return CollectionView{}, err
	}
	return s.GetCollection(ctx, id)
}

// AddCollectionPacks appends packs to a topic, skipping ones already in it.
func (s *Service) AddCollectionPacks(ctx context.Context, p auth.Principal, id string, packIDs []string) (CollectionView, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Collection{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return wrap("check topic", err)
		}
		if n == 0 {
			return apperr.NotFound("专题不存在")
		}
		if err := s.addPacksTx(tx, id, packIDs); err != nil {
			return err
		}
		if err := tx.Model(&model.Collection{}).Where("id = ?", id).Updates(map[string]any{"updated_at": s.now(), "locked": true}).Error; err != nil {
			return wrap("touch topic", err)
		}
		return s.audit(tx, p, "collection.add_packs", id)
	})
	if err != nil {
		return CollectionView{}, err
	}
	return s.GetCollection(ctx, id)
}

func (s *Service) addPacksTx(tx *gorm.DB, collectionID string, packIDs []string) error {
	if len(packIDs) == 0 {
		return nil
	}
	if len(packIDs) > maxCollectionItems {
		return apperr.Validation("专题最多 5000 个套卷")
	}
	var found int64
	if err := tx.Model(&model.Pack{}).Where("id IN ?", packIDs).Count(&found).Error; err != nil {
		return wrap("check packs", err)
	}
	unique := map[string]bool{}
	for _, id := range packIDs {
		unique[id] = true
	}
	if int(found) != len(unique) {
		return apperr.Validation("部分套卷不存在")
	}
	var maxPos struct{ N int }
	if err := tx.Model(&model.CollectionItem{}).Select("COALESCE(MAX(position), -1) AS n").
		Where("collection_id = ?", collectionID).Scan(&maxPos).Error; err != nil {
		return wrap("topic position", err)
	}
	var existing []string
	if err := tx.Model(&model.CollectionItem{}).Where("collection_id = ?", collectionID).Pluck("pack_id", &existing).Error; err != nil {
		return wrap("load topic items", err)
	}
	have := map[string]bool{}
	for _, id := range existing {
		have[id] = true
	}
	now := s.now()
	pos := maxPos.N
	for _, id := range packIDs {
		if have[id] {
			continue
		}
		have[id] = true
		pos++
		if err := tx.Create(&model.CollectionItem{ID: newID(), CollectionID: collectionID, PackID: id, Position: pos, CreatedAt: now}).Error; err != nil {
			return wrap("add topic item", err)
		}
	}
	return nil
}

// ReplaceItems sets the complete ordered pack list of a topic.
func (s *Service) ReplaceItems(ctx context.Context, p auth.Principal, id string, packIDs []string) (CollectionView, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Collection{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return wrap("check topic", err)
		}
		if n == 0 {
			return apperr.NotFound("专题不存在")
		}
		seen := map[string]bool{}
		for _, pid := range packIDs {
			if seen[pid] {
				return apperr.Validation("专题中存在重复套卷")
			}
			seen[pid] = true
		}
		if err := tx.Where("collection_id = ?", id).Delete(&model.CollectionItem{}).Error; err != nil {
			return wrap("clear topic items", err)
		}
		if err := s.addPacksTx(tx, id, packIDs); err != nil {
			return err
		}
		if err := tx.Model(&model.Collection{}).Where("id = ?", id).Updates(map[string]any{"updated_at": s.now(), "locked": true}).Error; err != nil {
			return wrap("touch topic", err)
		}
		return s.audit(tx, p, "collection.replace_items", id)
	})
	if err != nil {
		return CollectionView{}, err
	}
	return s.GetCollection(ctx, id)
}

// DeleteCollection removes a topic and its placements; packs are kept.
func (s *Service) DeleteCollection(ctx context.Context, p auth.Principal, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&model.Collection{})
		if res.Error != nil {
			return wrap("delete topic", res.Error)
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("专题不存在")
		}
		if err := tx.Where("collection_id = ?", id).Delete(&model.CollectionItem{}).Error; err != nil {
			return wrap("delete topic items", err)
		}
		return s.audit(tx, p, "collection.delete", id)
	})
}

// sortedByNatural returns values in natural file-name order.
func sortedByNatural(values []string) []string {
	out := append([]string{}, values...)
	sort.SliceStable(out, func(i, j int) bool { return paths.NaturalLess(out[i], out[j]) })
	return out
}

// collectionKey is how a topic is matched to the series name the model read.
// It folds punctuation and省市 suffixes, so "十年（2012-2021）..." and
// "十年(2012-2021)..." are the same topic.
func collectionKey(title string) string {
	key := truncateRunes(catalogai.NormalizeName(title), 255)
	if key == "" {
		return truncateRunes(title, 255)
	}
	return key
}

// seriesCandidates lists existing topic names so the model reuses one spelling
// per series instead of inventing a variant for every pack. Oldest first, so a
// new topic is appended and the shared prompt prefix stays stable.
func (s *Service) seriesCandidates(db *gorm.DB) ([]string, error) {
	var names []string
	if err := db.Model(&model.Collection{}).Order("created_at ASC").Limit(200).Pluck("title", &names).Error; err != nil {
		return nil, wrap("load series candidates", err)
	}
	return names, nil
}

// attachSeriesTx puts a decided pack into the topic of its series, creating the
// topic when the series is new. A topic a person has edited is left alone: the
// AI never renames, reorders or deletes one.
func (s *Service) attachSeriesTx(tx *gorm.DB, packID, series string, now time.Time) error {
	if series == "" {
		return nil
	}
	key := collectionKey(series)
	var topic model.Collection
	err := tx.Where("key = ?", key).First(&topic).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		topic = model.Collection{ID: newID(), Title: series, Source: model.CollectionSourceAI,
			Key: key, CreatedAt: now, UpdatedAt: now}
		if cerr := tx.Create(&topic).Error; cerr != nil {
			// Another decision created the same series first.
			if ferr := tx.Where("key = ?", key).First(&topic).Error; ferr != nil {
				return wrap("create series topic", cerr)
			}
		}
	case err != nil:
		return wrap("find series topic", err)
	case topic.Locked:
		return nil
	}
	if err := s.addPacksTx(tx, topic.ID, []string{packID}); err != nil {
		return err
	}
	if err := tx.Model(&model.Collection{}).Where("id = ?", topic.ID).Update("updated_at", now).Error; err != nil {
		return wrap("touch series topic", err)
	}
	return nil
}
