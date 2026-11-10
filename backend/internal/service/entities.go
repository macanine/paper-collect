package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// The name dictionary keeps one canonical entry per school, league or tag.
// The AI picks entries or creates them; every spelling it meets becomes an
// alias of the entry, and a new entry is checked by the AI for duplicates.
// People only rename, merge, remove aliases or lock entries when needed.

const (
	orgCandidateLimit = 30
	tagCandidateLimit = 150
)

// EntityView is an entry with its aliases and how many sets use it.
type EntityView struct {
	Entity  model.Entity
	Aliases []string
	Usage   int64
}

func validEntityKind(kind string) bool {
	return kind == model.EntityOrganization || kind == model.EntityTag
}

// lookupAliasTx finds the entry a spelling belongs to.
func lookupAliasTx(tx *gorm.DB, kind, text string) (model.Entity, bool, error) {
	norm := catalogai.NormalizeName(text)
	if norm == "" {
		return model.Entity{}, false, nil
	}
	var alias model.EntityAlias
	if err := tx.Where("kind = ? AND norm = ?", kind, norm).First(&alias).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Entity{}, false, nil
		}
		return model.Entity{}, false, err
	}
	var entity model.Entity
	if err := tx.Where("id = ?", alias.EntityID).First(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Entity{}, false, nil
		}
		return model.Entity{}, false, err
	}
	return entity, true, nil
}

// addAliasTx records a spelling for an entry unless it already belongs to one.
func addAliasTx(tx *gorm.DB, kind, entityID, text string, now time.Time) error {
	text = strings.TrimSpace(text)
	norm := catalogai.NormalizeName(text)
	if norm == "" || len([]rune(norm)) > 128 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.EntityAlias{}).Where("kind = ? AND norm = ?", kind, norm).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return tx.Create(&model.EntityAlias{Kind: kind, Norm: norm, EntityID: entityID, Text: truncateRunes(text, 128), CreatedAt: now}).Error
}

// ensureEntityTx returns the entry for a name, creating it when no known
// spelling matches. created reports whether a new entry was made.
func (s *Service) ensureEntityTx(tx *gorm.DB, kind, name string, aliases []string, region, source string, now time.Time) (model.Entity, bool, error) {
	name = truncateRunes(strings.TrimSpace(name), 128)
	for _, spelling := range append([]string{name}, aliases...) {
		entity, ok, err := lookupAliasTx(tx, kind, spelling)
		if err != nil {
			return model.Entity{}, false, err
		}
		if ok {
			if !entity.Locked {
				for _, a := range append([]string{name}, aliases...) {
					if err := addAliasTx(tx, kind, entity.ID, a, now); err != nil {
						return model.Entity{}, false, err
					}
				}
			}
			return entity, false, nil
		}
	}
	if catalogai.NormalizeName(name) == "" {
		return model.Entity{}, false, nil
	}
	entity := model.Entity{ID: newID(), Kind: kind, Name: name, Region: region, Source: source,
		Locked: source == model.EntitySourceHuman, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&entity).Error; err != nil {
		return model.Entity{}, false, wrap("create entity", err)
	}
	for _, a := range append([]string{name}, aliases...) {
		if err := addAliasTx(tx, kind, entity.ID, a, now); err != nil {
			return model.Entity{}, false, err
		}
	}
	return entity, true, nil
}

// resolveChoiceTx turns an AI choice into a dictionary entry. An id that no
// longer exists (merged meanwhile) resolves to nothing.
func (s *Service) resolveChoiceTx(tx *gorm.DB, kind string, choice catalogai.EntityChoice, region string, now time.Time) (model.Entity, bool, error) {
	if choice.ID != "" {
		var entity model.Entity
		if err := tx.Where("id = ? AND kind = ?", choice.ID, kind).First(&entity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.Entity{}, false, nil
			}
			return model.Entity{}, false, err
		}
		if !entity.Locked {
			for _, a := range choice.Aliases {
				if err := addAliasTx(tx, kind, entity.ID, a, now); err != nil {
					return model.Entity{}, false, err
				}
			}
		}
		return entity, false, nil
	}
	if strings.TrimSpace(choice.New) == "" {
		return model.Entity{}, false, nil
	}
	return s.ensureEntityTx(tx, kind, choice.New, choice.Aliases, region, model.EntitySourceAI, now)
}

// entityAliases loads the aliases of many entries.
func entityAliases(db *gorm.DB, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.EntityAlias
	if err := db.Where("entity_id IN ?", ids).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.EntityID] = append(out[r.EntityID], r.Text)
	}
	return out, nil
}

// orgCandidates ranks dictionary schools by how close any alias is to the
// names printed in the set. The model decides which, if any, is meant.
func (s *Service) orgCandidates(db *gorm.DB, mentions, regions []string) ([]catalogai.EntityCandidate, error) {
	var entities []model.Entity
	if err := db.Where("kind = ?", model.EntityOrganization).Limit(5000).Find(&entities).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entities))
	for _, e := range entities {
		ids = append(ids, e.ID)
	}
	aliases, err := entityAliases(db, ids)
	if err != nil {
		return nil, err
	}
	regionSet := map[string]bool{}
	for _, r := range regions {
		for _, known := range catalogai.Regions {
			if strings.Contains(r, known) {
				regionSet[known] = true
			}
		}
	}
	type scored struct {
		entity model.Entity
		score  float64
	}
	ranked := []scored{}
	for _, e := range entities {
		best := 0.0
		for _, mention := range mentions {
			for _, a := range append([]string{e.Name}, aliases[e.ID]...) {
				best = max(best, catalogai.Similarity(mention, a))
			}
		}
		if regionSet[e.Region] {
			best += 0.15
		}
		if best >= 0.3 {
			ranked = append(ranked, scored{e, best})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > orgCandidateLimit {
		ranked = ranked[:orgCandidateLimit]
	}
	out := make([]catalogai.EntityCandidate, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, catalogai.EntityCandidate{ID: r.entity.ID, Name: r.entity.Name, Aliases: aliases[r.entity.ID], Region: r.entity.Region})
	}
	return out, nil
}

// tagCandidates offers the most used tags.
func (s *Service) tagCandidates(db *gorm.DB) ([]catalogai.EntityCandidate, error) {
	var entities []model.Entity
	if err := db.Where("kind = ?", model.EntityTag).Find(&entities).Error; err != nil {
		return nil, err
	}
	usage, err := tagUsage(db)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entities, func(i, j int) bool { return usage[entities[i].Name] > usage[entities[j].Name] })
	if len(entities) > tagCandidateLimit {
		entities = entities[:tagCandidateLimit]
	}
	out := make([]catalogai.EntityCandidate, 0, len(entities))
	for _, e := range entities {
		out = append(out, catalogai.EntityCandidate{ID: e.ID, Name: e.Name})
	}
	return out, nil
}

func tagUsage(db *gorm.DB) (map[string]int64, error) {
	var rows []string
	if err := db.Model(&model.Pack{}).Where("tags_json <> '[]'").Limit(20000).Pluck("tags_json", &rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, raw := range rows {
		for _, t := range model.DecodeStrings(raw) {
			out[t]++
		}
	}
	return out, nil
}

// enqueueEntityJobTx asks the model whether a new entry duplicates another.
func (s *Service) enqueueEntityJobTx(tx *gorm.DB, entityID string, now time.Time) error {
	return wrap("create entity job", tx.Create(&model.Job{ID: newID(), Kind: model.JobEntity, Status: model.JobPending,
		EntityID: entityID, CreatedAt: now, UpdatedAt: now}).Error)
}

// prepareEntity offers the model the entries whose names are close enough to
// be the same, each with a few sets that use it.
func (s *Service) prepareEntity(ctx context.Context, job model.Job) (*jobexec.EntityInput, error) {
	db := s.db.WithContext(ctx)
	var entity model.Entity
	if err := db.Where("id = ?", job.EntityID).First(&entity).Error; err != nil || entity.Locked {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	var others []model.Entity
	if err := db.Where("kind = ? AND id <> ?", entity.Kind, entity.ID).Limit(5000).Find(&others).Error; err != nil {
		return nil, err
	}
	ids := []string{entity.ID}
	for _, o := range others {
		ids = append(ids, o.ID)
	}
	aliases, err := entityAliases(db, ids)
	if err != nil {
		return nil, err
	}
	names := append([]string{entity.Name}, aliases[entity.ID]...)
	type scored struct {
		entity model.Entity
		score  float64
	}
	ranked := []scored{}
	for _, o := range others {
		best := 0.0
		for _, a := range append([]string{o.Name}, aliases[o.ID]...) {
			for _, n := range names {
				best = max(best, catalogai.Similarity(n, a))
			}
		}
		if best >= 0.4 {
			ranked = append(ranked, scored{o, best})
		}
	}
	if len(ranked) == 0 {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > 10 {
		ranked = ranked[:10]
	}
	in := catalogai.DedupeInput{Kind: entity.Kind, Entity: s.dedupeEntity(db, entity, aliases[entity.ID])}
	for _, r := range ranked {
		in.Candidates = append(in.Candidates, s.dedupeEntity(db, r.entity, aliases[r.entity.ID]))
	}
	models, err := s.aiModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, configError(errors.New("没有启用的 AI 模型端点"))
	}
	return &jobexec.EntityInput{Request: in, Models: models}, nil
}

func (s *Service) dedupeEntity(db *gorm.DB, e model.Entity, aliases []string) catalogai.DedupeEntity {
	out := catalogai.DedupeEntity{EntityCandidate: catalogai.EntityCandidate{ID: e.ID, Name: e.Name, Aliases: aliases, Region: e.Region}}
	var titles []string
	if e.Kind == model.EntityOrganization {
		_ = db.Model(&model.Pack{}).Where("organization_id = ?", e.ID).Order("updated_at DESC").Limit(3).Pluck("title", &titles).Error
	} else {
		_ = db.Model(&model.Pack{}).Where("tags_json LIKE ? ESCAPE '!'", "%"+likeEscape(`"`+e.Name+`"`)+"%").
			Order("updated_at DESC").Limit(3).Pluck("title", &titles).Error
	}
	out.Samples = titles
	return out
}

// commitEntity merges a duplicate entry into the one the model named.
func (s *Service) commitEntity(ctx context.Context, job model.Job, out *jobexec.EntityResult) error {
	if out == nil {
		return errors.New("名称检查结果为空")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		if out.Reply.SameAs == "" {
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "没有发现重复的名称")
		}
		ids, err := s.mergeEntityTx(tx, job.EntityID, out.Reply.SameAs, false)
		if err != nil {
			return err
		}
		renamed = ids
		return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "已合并重复的名称："+out.Reply.Reason)
	})
	if err != nil {
		return err
	}
	s.notify()
	s.renameMaterialFiles(context.WithoutCancel(ctx), renamed)
	return nil
}

// mergeEntityTx moves every alias and use of source into target and deletes
// source. A locked source is only merged by a person.
func (s *Service) mergeEntityTx(tx *gorm.DB, sourceID, targetID string, human bool) ([]string, error) {
	if sourceID == targetID {
		return nil, apperr.Validation("不能合并到自身")
	}
	var source, target model.Entity
	if err := tx.Where("id = ?", sourceID).First(&source).Error; err != nil {
		return nil, apperr.NotFound("名称条目不存在")
	}
	if err := tx.Where("id = ?", targetID).First(&target).Error; err != nil {
		return nil, apperr.NotFound("目标名称条目不存在")
	}
	if source.Kind != target.Kind {
		return nil, apperr.Validation("只能合并同一类名称")
	}
	if source.Locked && !human {
		return nil, nil
	}
	now := s.now()
	if err := tx.Model(&model.EntityAlias{}).Where("entity_id = ?", source.ID).Update("entity_id", target.ID).Error; err != nil {
		return nil, wrap("move aliases", err)
	}
	if err := addAliasTx(tx, source.Kind, target.ID, source.Name, now); err != nil {
		return nil, err
	}
	var packIDs []string
	if source.Kind == model.EntityOrganization {
		if err := tx.Unscoped().Model(&model.Pack{}).Where("organization_id = ?", source.ID).Pluck("id", &packIDs).Error; err != nil {
			return nil, err
		}
		if err := tx.Unscoped().Model(&model.Pack{}).Where("organization_id = ?", source.ID).
			Updates(map[string]any{"organization_id": target.ID, "organization": target.Name, "updated_at": now}).Error; err != nil {
			return nil, wrap("repoint packs", err)
		}
	} else {
		ids, err := s.renameTagTx(tx, source.Name, target.Name, now)
		if err != nil {
			return nil, err
		}
		packIDs = ids
	}
	if err := tx.Where("id = ?", source.ID).Delete(&model.Entity{}).Error; err != nil {
		return nil, wrap("delete entity", err)
	}
	return s.refreshPacksTx(tx, packIDs)
}

// renameTagTx replaces a tag name in every set that carries it.
func (s *Service) renameTagTx(tx *gorm.DB, from, to string, now time.Time) ([]string, error) {
	var packs []model.Pack
	if err := tx.Unscoped().Select("id", "tags_json").Where("tags_json LIKE ? ESCAPE '!'", "%"+likeEscape(`"`+from+`"`)+"%").Find(&packs).Error; err != nil {
		return nil, err
	}
	ids := []string{}
	for _, p := range packs {
		tags := []string{}
		for _, t := range model.DecodeStrings(p.TagsJSON) {
			if t == from {
				t = to
			}
			tags = appendUnique(tags, t)
		}
		if err := tx.Unscoped().Model(&model.Pack{}).Where("id = ?", p.ID).
			Updates(map[string]any{"tags_json": model.EncodeStrings(tags), "updated_at": now}).Error; err != nil {
			return nil, err
		}
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// refreshPacksTx recomputes names of sets whose catalogue values changed.
func (s *Service) refreshPacksTx(tx *gorm.DB, packIDs []string) ([]string, error) {
	var renamed []string
	for _, id := range packIDs {
		ids, err := s.refreshPackNamesTx(tx, id)
		if err != nil {
			return nil, err
		}
		renamed = append(renamed, ids...)
	}
	return renamed, nil
}

// ListEntities lists dictionary entries of one kind, optionally filtered.
func (s *Service) ListEntities(ctx context.Context, kind, q string, pg PageQuery) (Page[EntityView], error) {
	if !validEntityKind(kind) {
		return Page[EntityView]{}, apperr.Validation("kind 只能是 organization 或 tag")
	}
	db := s.db.WithContext(ctx)
	query := db.Model(&model.Entity{}).Where("kind = ?", kind)
	if q = strings.TrimSpace(q); q != "" {
		query = query.Where("(LOWER(name) LIKE ? ESCAPE '!' OR id IN (?))", likePattern(q),
			db.Model(&model.EntityAlias{}).Select("entity_id").Where("LOWER(text) LIKE ? ESCAPE '!'", likePattern(q)))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return Page[EntityView]{}, apperr.Internal(err)
	}
	var rows []model.Entity
	if err := query.Order("name ASC").Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[EntityView]{}, apperr.Internal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	aliases, err := entityAliases(db, ids)
	if err != nil {
		return Page[EntityView]{}, apperr.Internal(err)
	}
	var usage map[string]int64
	if kind == model.EntityTag {
		if usage, err = tagUsage(db); err != nil {
			return Page[EntityView]{}, apperr.Internal(err)
		}
	}
	items := make([]EntityView, 0, len(rows))
	for _, r := range rows {
		view := EntityView{Entity: r, Aliases: aliases[r.ID]}
		if kind == model.EntityTag {
			view.Usage = usage[r.Name]
		} else if err := db.Model(&model.Pack{}).Where("organization_id = ?", r.ID).Count(&view.Usage).Error; err != nil {
			return Page[EntityView]{}, apperr.Internal(err)
		}
		items = append(items, view)
	}
	return newPage(items, total, pg), nil
}

// EntityPatch is a manual dictionary edit. Any edit locks the entry.
type EntityPatch struct {
	Name          *string
	Region        *string
	Locked        *bool
	RemoveAliases []string
}

// UpdateEntity renames, re-regions, (un)locks an entry or removes aliases.
// Renaming rewrites every set that uses the entry.
func (s *Service) UpdateEntity(ctx context.Context, p auth.Principal, id string, pt EntityPatch) (EntityView, error) {
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e model.Entity
		if err := tx.Where("id = ?", id).First(&e).Error; err != nil {
			return apperr.NotFound("名称条目不存在")
		}
		now := s.now()
		updates := map[string]any{"locked": true, "source": model.EntitySourceHuman, "updated_at": now}
		if pt.Locked != nil {
			updates["locked"] = *pt.Locked
		}
		if pt.Region != nil {
			if !catalogai.IsRegion(*pt.Region) {
				return apperr.Validation("地区不在固定列表中")
			}
			updates["region"] = *pt.Region
		}
		if pt.Name != nil {
			name := strings.TrimSpace(*pt.Name)
			if n := runeLen(name); n < 1 || n > 128 {
				return apperr.Validation("名称长度必须为 1 到 128 个字符")
			}
			if other, ok, err := lookupAliasTx(tx, e.Kind, name); err != nil {
				return err
			} else if ok && other.ID != e.ID {
				return apperr.Conflict("该名称已属于另一个条目「" + other.Name + "」，请使用合并")
			}
			updates["name"] = name
			if err := addAliasTx(tx, e.Kind, e.ID, name, now); err != nil {
				return err
			}
			var packIDs []string
			if e.Kind == model.EntityOrganization {
				if err := tx.Unscoped().Model(&model.Pack{}).Where("organization_id = ?", e.ID).Pluck("id", &packIDs).Error; err != nil {
					return err
				}
				if err := tx.Unscoped().Model(&model.Pack{}).Where("organization_id = ?", e.ID).
					Updates(map[string]any{"organization": name, "updated_at": now}).Error; err != nil {
					return err
				}
			} else {
				ids, err := s.renameTagTx(tx, e.Name, name, now)
				if err != nil {
					return err
				}
				packIDs = ids
			}
			if err := tx.Model(&model.Entity{}).Where("id = ?", e.ID).Updates(updates).Error; err != nil {
				return err
			}
			ids, err := s.refreshPacksTx(tx, packIDs)
			if err != nil {
				return err
			}
			renamed = ids
		} else if err := tx.Model(&model.Entity{}).Where("id = ?", e.ID).Updates(updates).Error; err != nil {
			return err
		}
		for _, text := range pt.RemoveAliases {
			norm := catalogai.NormalizeName(text)
			if norm == catalogai.NormalizeName(e.Name) {
				continue
			}
			if err := tx.Where("kind = ? AND norm = ? AND entity_id = ?", e.Kind, norm, e.ID).Delete(&model.EntityAlias{}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, p, "entity.update", e.ID)
	})
	if err != nil {
		return EntityView{}, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return s.getEntity(ctx, id)
}

// MergeEntity merges source into target as a human decision.
func (s *Service) MergeEntity(ctx context.Context, p auth.Principal, sourceID, targetID string) (EntityView, error) {
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := s.mergeEntityTx(tx, sourceID, targetID, true)
		if err != nil {
			return err
		}
		renamed = ids
		if err := tx.Model(&model.Entity{}).Where("id = ?", targetID).
			Updates(map[string]any{"locked": true, "source": model.EntitySourceHuman, "updated_at": s.now()}).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "entity.merge", sourceID+"->"+targetID)
	})
	if err != nil {
		return EntityView{}, err
	}
	s.renameMaterialFiles(ctx, renamed)
	return s.getEntity(ctx, targetID)
}

func (s *Service) getEntity(ctx context.Context, id string) (EntityView, error) {
	db := s.db.WithContext(ctx)
	var e model.Entity
	if err := db.Where("id = ?", id).First(&e).Error; err != nil {
		return EntityView{}, apperr.NotFound("名称条目不存在")
	}
	aliases, err := entityAliases(db, []string{id})
	if err != nil {
		return EntityView{}, apperr.Internal(err)
	}
	view := EntityView{Entity: e, Aliases: aliases[id]}
	if e.Kind == model.EntityOrganization {
		_ = db.Model(&model.Pack{}).Where("organization_id = ?", id).Count(&view.Usage).Error
	} else if usage, err := tagUsage(db); err == nil {
		view.Usage = usage[e.Name]
	}
	return view, nil
}
