package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const (
	decideExcerptRunes = 3000 // text per member shown to the model
	decideMaxExcerpts  = 8    // members beyond this are described by their card only
)

type decidePayload struct {
	Revision int `json:"revision"`
}

// enqueueDecideTx queues a decision for a set unless one is already waiting;
// a waiting job reads the latest members when it starts.
func (s *Service) enqueueDecideTx(tx *gorm.DB, packID string, now time.Time) error {
	if packID == "" {
		return nil
	}
	var members []model.Material
	if err := tx.Select("id", "stage", "ai_step", "attention_json").
		Where("pack_id = ? AND stage <> ? AND ai_step NOT IN ?", packID, model.StageReady, []string{model.StepRead, model.StepGroup}).Find(&members).Error; err != nil {
		return wrap("load deciding members", err)
	}
	for _, m := range members {
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"stage": model.StageProcessing, "ai_step": model.StepDecide,
			"attention_json": model.EncodeStrings(carryReasons(model.DecodeStrings(m.AttentionJSON))), "updated_at": now,
		}).Error; err != nil {
			return wrap("mark deciding", err)
		}
	}
	var open int64
	if err := tx.Model(&model.Job{}).Where("kind = ? AND pack_id = ? AND status = ?",
		model.JobDecide, packID, model.JobPending).Count(&open).Error; err != nil {
		return wrap("count decide jobs", err)
	}
	if open > 0 {
		return nil
	}
	return wrap("create decide job", tx.Create(&model.Job{ID: newID(), Kind: model.JobDecide, Status: model.JobPending,
		PackID: packID, CreatedAt: now, UpdatedAt: now}).Error)
}

// lockedValues shows a set's human-fixed fields to the model as fixed.
func lockedValues(p model.Pack) catalogai.Locked {
	l := p.Locked()
	var out catalogai.Locked
	str := func(field, value string) *string {
		if !l[field] {
			return nil
		}
		v := value
		return &v
	}
	out.DocType = str(catalogai.FieldDocType, p.DocType)
	out.Grade = str(catalogai.FieldGrade, p.Grade)
	out.Term = str(catalogai.FieldTerm, p.Term)
	out.Region = str(catalogai.FieldRegion, p.Region)
	out.Organization = str(catalogai.FieldOrganization, p.Organization)
	out.ExamDate = str(catalogai.FieldExamDate, p.ExamDate)
	out.ExamName = str(catalogai.FieldExamName, p.ExamName)
	out.Chapter = str(catalogai.FieldChapter, p.Chapter)
	if l[catalogai.FieldCohort] {
		v := p.Cohort
		out.Cohort = &v
	}
	if l[catalogai.FieldSubjects] {
		v := model.DecodeStrings(p.SubjectsJSON)
		out.Subjects = &v
	}
	if l[catalogai.FieldTags] {
		v := model.DecodeStrings(p.TagsJSON)
		out.Tags = &v
	}
	return out
}

// prepareDecide gathers everything the model needs to decide one set.
func (s *Service) prepareDecide(ctx context.Context, job model.Job) (*jobexec.DecideInput, error) {
	db := s.db.WithContext(ctx)
	var pack model.Pack
	if err := db.Where("id = ?", job.PackID).First(&pack).Error; err != nil {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	var members []model.Material
	if err := db.Where("pack_id = ?", pack.ID).Order("position ASC").Order("created_at ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	for _, m := range members {
		// A member still being read or grouped queues a new decision itself
		// once it is done; deciding now would be decided on stale members.
		if (m.AIStep == model.StepRead || m.AIStep == model.StepGroup) &&
			(m.Stage == model.StageProcessing || m.Stage == model.StageWaitingAI) {
			if s.succeedJob(job.ID, job.LeaseOwner) {
				return nil, errJobDone
			}
			return nil, errLeaseLost
		}
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	cards := loadCards(db, ids)
	var texts []model.MaterialText
	_ = db.Select("material_id", "markdown", "page_count").Where("material_id IN ?", ids).Find(&texts).Error
	textByID := map[string]model.MaterialText{}
	for _, t := range texts {
		textByID[t.MaterialID] = t
	}
	in := catalogai.DecideInput{}
	mentions, regions := []string{}, []string{}
	for i, m := range members {
		card, ok := cards[m.ID]
		if !ok {
			card = catalogai.Card{Role: m.Part, MetadataOnly: true}
		}
		member := catalogai.DecideMember{ID: m.ID, FileName: m.OriginalName, Card: card, PageCount: textByID[m.ID].PageCount}
		var sources []model.MaterialSource
		_ = db.Where("material_id = ?", m.ID).Order("created_at ASC").Limit(3).Find(&sources).Error
		for _, src := range sources {
			member.Paths = append(member.Paths, src.Path)
		}
		if m.PartLocked {
			member.LockedRole, member.LockedPage = m.Part, m.AnswerStartPage
		}
		if i < decideMaxExcerpts && !card.MetadataOnly {
			member.Excerpt = jobexec.SampleAnalysisDocument(textByID[m.ID].Markdown, decideExcerptRunes)
		}
		in.Members = append(in.Members, member)
		mentions = append(mentions, card.OrgMentions...)
		regions = append(regions, card.RegionMentions...)
		if in.Hints == "" {
			in.Hints = s.cardMetaTx(db, m).Hints
		}
	}
	in.Locked = lockedValues(pack)
	if pack.Organization != "" {
		mentions = append(mentions, pack.Organization)
	}
	var err error
	if in.Organizations, err = s.orgCandidates(db, mentions, regions); err != nil {
		return nil, err
	}
	if in.Tags, err = s.tagCandidates(db); err != nil {
		return nil, err
	}
	models, err := s.aiModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, configError(errors.New("没有启用的 AI 模型端点"))
	}
	payload, _ := json.Marshal(decidePayload{Revision: pack.Revision})
	if err := db.Model(&model.Job{}).Where("id = ? AND lease_owner = ?", job.ID, job.LeaseOwner).
		Update("payload_json", string(payload)).Error; err != nil {
		return nil, err
	}
	now := s.now()
	if err := db.Save(&model.PackDecision{PackID: pack.ID, Status: model.AnalysisRunning, JobID: job.ID, Revision: pack.Revision,
		DecisionJSON: "{}", ProblemsJSON: "[]", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		return nil, err
	}
	return &jobexec.DecideInput{Request: in, Models: models}, nil
}

// commitDecide applies a verified decision to the set it was made for.
func (s *Service) commitDecide(ctx context.Context, job model.Job, out *jobexec.DecideResult) error {
	if out == nil {
		return errors.New("编目结果为空")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		now := s.now()
		var latest model.Job
		if err := tx.Select("id", "payload_json").Where("id = ?", job.ID).First(&latest).Error; err != nil {
			return err
		}
		var payload decidePayload
		_ = json.Unmarshal([]byte(latest.PayloadJSON), &payload)
		var pack model.Pack
		if err := tx.Where("id = ?", job.PackID).First(&pack).Error; err != nil {
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "套卷已不存在")
		}
		if pack.Revision != payload.Revision {
			if err := s.enqueueDecideTx(tx, pack.ID, now); err != nil {
				return err
			}
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "套卷成员已变化，已重新安排编目")
		}
		if !out.Usable {
			if err := s.decisionFailedTx(tx, pack.ID, job.ID, "模型没有给出可用的编目", out.Problems); err != nil {
				return err
			}
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobFailed, "模型没有给出可用的编目")
		}
		ids, err := s.applyDecisionTx(tx, pack, job.ID, out, now)
		if err != nil {
			return err
		}
		renamed = ids
		message := fmt.Sprintf("已确定编目（%d 个校验提示）", len(out.Problems))
		if catalogai.HasCritical(out.Problems) {
			message = "编目未通过全部校验，需要关注"
		}
		return s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded, message,
			map[string]any{"problems": len(out.Problems), "members": len(out.Decision.Files)})
	})
	if err != nil {
		return err
	}
	s.notify()
	s.renameMaterialFiles(context.WithoutCancel(ctx), renamed)
	return nil
}

// applyDecisionTx writes a decision: catalogue fields (except locked ones),
// canonical names, member roles, stages, sync plans and index jobs.
func (s *Service) applyDecisionTx(tx *gorm.DB, pack model.Pack, jobID string, out *jobexec.DecideResult, now time.Time) ([]string, error) {
	d := out.Decision
	locked := pack.Locked()
	updates := map[string]any{"updated_at": now}
	var created []string
	if !locked[catalogai.FieldDocType] {
		updates["doc_type"] = d.DocType
	}
	subjects := []string(nil)
	if !locked[catalogai.FieldSubjects] {
		subjects = sortSubjects(d.Subjects)
		updates["subjects_json"] = model.EncodeStrings(subjects)
	}
	set := func(field, column string, value any) {
		if !locked[field] {
			updates[column] = value
		}
	}
	set(catalogai.FieldGrade, "grade", d.Grade)
	set(catalogai.FieldTerm, "term", d.Term)
	set(catalogai.FieldCohort, "cohort", d.Cohort)
	set(catalogai.FieldRegion, "region", d.Region)
	set(catalogai.FieldExamDate, "exam_date", d.ExamDate)
	set(catalogai.FieldExamName, "exam_name", d.ExamName)
	set(catalogai.FieldChapter, "chapter", d.Chapter)
	if !locked[catalogai.FieldOrganization] {
		name, id := "", ""
		if d.Organization != nil {
			entity, isNew, err := s.resolveChoiceTx(tx, model.EntityOrganization, *d.Organization, d.Region, now)
			if err != nil {
				return nil, err
			}
			if entity.ID != "" {
				name, id = entity.Name, entity.ID
				if isNew {
					created = append(created, entity.ID)
				}
			}
		}
		updates["organization"], updates["organization_id"] = name, id
	}
	if !locked[catalogai.FieldTags] {
		tags := []string{}
		for _, choice := range d.Tags {
			entity, isNew, err := s.resolveChoiceTx(tx, model.EntityTag, choice, "", now)
			if err != nil {
				return nil, err
			}
			if entity.ID == "" {
				continue
			}
			tags = appendUnique(tags, entity.Name)
			if isNew {
				created = append(created, entity.ID)
			}
		}
		updates["tags_json"] = model.EncodeStrings(tags)
	}
	if err := tx.Model(&model.Pack{}).Where("id = ?", pack.ID).Updates(updates).Error; err != nil {
		return nil, wrap("apply decision", err)
	}
	if subjects != nil {
		if err := tx.Where("pack_id = ?", pack.ID).Delete(&model.PackSubject{}).Error; err != nil {
			return nil, wrap("clear subjects", err)
		}
		if err := insertPackSubjects(tx, pack.ID, subjects); err != nil {
			return nil, err
		}
	}

	critical := []catalogai.Problem{}
	for _, p := range out.Problems {
		if p.Critical {
			critical = append(critical, p)
		}
	}
	setReasons := catalogai.Codes(critical)
	roles := map[string]catalogai.FileDecision{}
	for _, f := range d.Files {
		roles[f.ID] = f
	}
	var members []model.Material
	if err := tx.Where("pack_id = ?", pack.ID).Find(&members).Error; err != nil {
		return nil, err
	}
	changed := []*model.Material{}
	for i := range members {
		m := &members[i]
		mu := map[string]any{"updated_at": now}
		if f, ok := roles[m.ID]; ok && !m.PartLocked {
			if f.Role != m.Part {
				if err := s.syncMaterialIndexForPartTx(tx, m.ID, f.Role, now); err != nil {
					return nil, wrap("sync index state", err)
				}
				m.Part = f.Role
				changed = append(changed, m)
			}
			mu["part"], mu["answer_start_page"] = f.Role, f.AnswerStartPage
		}
		if m.AIStep == model.StepRead || m.AIStep == model.StepGroup {
			// Not settled yet: its own step queues the next decision.
			continue
		}
		reasons := append(append([]string{}, setReasons...), carryReasons(model.DecodeStrings(m.AttentionJSON))...)
		mu["ai_step"], mu["stage"], mu["attention_json"] = "", model.StageReady, model.EncodeStrings(reasons)
		if len(reasons) > 0 {
			mu["stage"] = model.StageAttention
		}
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(mu).Error; err != nil {
			return nil, wrap("update member", err)
		}
	}
	decisionJSON, _ := json.Marshal(d)
	problemsJSON, _ := json.Marshal(out.Problems)
	if err := tx.Save(&model.PackDecision{PackID: pack.ID, Status: model.AnalysisSucceeded, JobID: jobID, Revision: pack.Revision,
		ProviderID: out.ProviderID, Model: out.Model, DecisionJSON: string(decisionJSON), ProblemsJSON: string(problemsJSON),
		DecidedAt: &now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		return nil, err
	}
	renamed, err := s.refreshPackNamesTx(tx, pack.ID)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.AIStep == model.StepRead || m.AIStep == model.StepGroup {
			continue
		}
		if _, err := s.planMaterialTx(tx, m.ID, true); err != nil {
			return nil, err
		}
	}
	if err := s.autoEnqueueIndexTx(tx, changed, now); err != nil {
		return nil, err
	}
	for _, id := range created {
		if err := s.enqueueEntityJobTx(tx, id, now); err != nil {
			return nil, err
		}
	}
	return renamed, nil
}

// decisionFailedTx marks a set whose decision could not be made. Its files
// keep their current catalogue and wait for a person or a retry; sync only
// starts once some decision exists.
func (s *Service) decisionFailedTx(tx *gorm.DB, packID, jobID, message string, problems []catalogai.Problem) error {
	now := s.now()
	problemsJSON, _ := json.Marshal(problems)
	if err := tx.Save(&model.PackDecision{PackID: packID, Status: model.AnalysisFailed, JobID: jobID,
		DecisionJSON: "{}", ProblemsJSON: string(problemsJSON), Error: message, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		return err
	}
	var members []model.Material
	if err := tx.Select("id", "attention_json", "ai_step").Where("pack_id = ? AND ai_step = ?", packID, model.StepDecide).Find(&members).Error; err != nil {
		return err
	}
	for _, m := range members {
		reasons := appendUnique(carryReasons(model.DecodeStrings(m.AttentionJSON)), model.AttentionAIFailed)
		if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"stage": model.StageAttention, "attention_json": model.EncodeStrings(reasons), "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// RedecidePack asks the model to decide a set again (locked fields stay).
func (s *Service) RedecidePack(ctx context.Context, p auth.Principal, id string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.Pack{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return apperr.NotFound("套卷不存在")
		}
		now := s.now()
		if err := tx.Model(&model.Material{}).Where("pack_id = ? AND ai_step <> ?", id, model.StepRead).
			Updates(map[string]any{"stage": model.StageProcessing, "ai_step": model.StepDecide, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := s.enqueueDecideTx(tx, id, now); err != nil {
			return err
		}
		return s.audit(tx, p, "pack.ai_redecide", id)
	})
	if err != nil {
		return err
	}
	s.notify()
	return nil
}

// PackAIView is a set's latest decision plus each member's card.
type PackAIView struct {
	Decision     *model.PackDecision
	ProviderName string
	Parsed       catalogai.Decision
	Problems     []catalogai.Problem
	Cards        map[string]catalogai.Card
}

// GetPackAI returns the AI record of a set.
func (s *Service) GetPackAI(ctx context.Context, id string) (PackAIView, error) {
	db := s.db.WithContext(ctx)
	var count int64
	if err := db.Model(&model.Pack{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return PackAIView{}, apperr.Internal(err)
	}
	if count == 0 {
		return PackAIView{}, apperr.NotFound("套卷不存在")
	}
	view := PackAIView{Cards: map[string]catalogai.Card{}, Problems: []catalogai.Problem{}}
	var decision model.PackDecision
	if err := db.Where("pack_id = ?", id).First(&decision).Error; err == nil {
		view.Decision = &decision
		view.ProviderName = s.providerName(ctx, decision.ProviderID)
		_ = json.Unmarshal([]byte(decision.DecisionJSON), &view.Parsed)
		_ = json.Unmarshal([]byte(decision.ProblemsJSON), &view.Problems)
	}
	var ids []string
	if err := db.Model(&model.Material{}).Where("pack_id = ?", id).Pluck("id", &ids).Error; err != nil {
		return PackAIView{}, apperr.Internal(err)
	}
	view.Cards = loadCards(db, ids)
	return view, nil
}
