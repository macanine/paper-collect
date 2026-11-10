package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"time"

	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

const (
	groupChunkFiles     = 80  // files per GROUP call
	groupChunkStretch   = 120 // a chunk may grow to finish the folder it stopped in
	groupCandidateLimit = 40  // existing sets offered per call
)

type groupPayload struct {
	Files []string `json:"files"`
}

// maybeEnqueueGroupTx queues grouping for a batch once none of its files is
// still being read. Files waiting for AI or needing attention at READ do not
// hold their batch back; they are grouped later against the sets built now.
func (s *Service) maybeEnqueueGroupTx(tx *gorm.DB, batchID string, now time.Time) error {
	if batchID == "" {
		return nil
	}
	var reading int64
	if err := tx.Model(&model.Material{}).Where("batch_id = ? AND ai_step = ? AND stage = ?",
		batchID, model.StepRead, model.StageProcessing).Count(&reading).Error; err != nil {
		return wrap("count reading", err)
	}
	if reading > 0 {
		return nil
	}
	var waiting int64
	if err := tx.Model(&model.Material{}).Where("batch_id = ? AND ai_step = ? AND stage IN ?",
		batchID, model.StepGroup, []string{model.StageProcessing, model.StageWaitingAI}).Count(&waiting).Error; err != nil {
		return wrap("count grouping", err)
	}
	if waiting == 0 {
		return nil
	}
	var open int64
	if err := tx.Model(&model.Job{}).Where("kind = ? AND batch_id = ? AND status = ?",
		model.JobGroup, batchID, model.JobPending).Count(&open).Error; err != nil {
		return wrap("count group jobs", err)
	}
	if open > 0 {
		return nil
	}
	return wrap("create group job", tx.Create(&model.Job{ID: newID(), Kind: model.JobGroup, Status: model.JobPending,
		BatchID: batchID, CreatedAt: now, UpdatedAt: now}).Error)
}

// prepareGroup picks the next chunk of a batch and the existing sets its
// files may join.
func (s *Service) prepareGroup(ctx context.Context, job model.Job) (*jobexec.GroupInput, error) {
	db := s.db.WithContext(ctx)
	var rows []model.Material
	if err := db.Where("batch_id = ? AND ai_step = ? AND stage IN ?", job.BatchID, model.StepGroup,
		[]string{model.StageProcessing, model.StageWaitingAI}).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if s.succeedJob(job.ID, job.LeaseOwner) {
			return nil, errJobDone
		}
		return nil, errLeaseLost
	}
	rows, sourcePaths := s.batchOrder(db, rows)
	chunk := chunkByFolder(rows, sourcePaths)
	ids := make([]string, 0, len(chunk))
	for _, m := range chunk {
		ids = append(ids, m.ID)
	}
	cards := loadCards(db, ids)
	files := make([]catalogai.GroupFile, 0, len(chunk))
	for _, m := range chunk {
		card, ok := cards[m.ID]
		if !ok {
			card = catalogai.Card{Role: m.Part, MetadataOnly: true}
		}
		files = append(files, catalogai.GroupFile{ID: m.ID, Path: sourcePaths[m.ID], Card: card, CurrentSet: m.PackID})
	}
	candidates, err := s.groupCandidates(db, job.BatchID, files)
	if err != nil {
		return nil, err
	}
	models, err := s.aiModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, configError(errors.New("没有启用的 AI 模型端点"))
	}
	payload, _ := json.Marshal(groupPayload{Files: ids})
	if err := db.Model(&model.Job{}).Where("id = ? AND lease_owner = ?", job.ID, job.LeaseOwner).
		Update("payload_json", string(payload)).Error; err != nil {
		return nil, err
	}
	return &jobexec.GroupInput{Request: catalogai.GroupInput{Files: files, Candidates: candidates}, Models: models}, nil
}

// chunkByFolder takes up to groupChunkFiles files, then keeps going while
// the next file sits in the same folder as the last one taken.
func chunkByFolder(rows []model.Material, sourcePaths map[string]string) []model.Material {
	if len(rows) <= groupChunkFiles {
		return rows
	}
	n := groupChunkFiles
	last := path.Dir(sourcePaths[rows[n-1].ID])
	for n < len(rows) && n < groupChunkStretch && path.Dir(sourcePaths[rows[n].ID]) == last {
		n++
	}
	return rows[:n]
}

// groupCandidates offers existing sets that could plausibly be the same exam:
// the files' current sets, sets already built from this batch, and library
// sets sharing subject and grade or cohort. Ranking only decides what fits in
// the prompt; the model decides membership.
func (s *Service) groupCandidates(db *gorm.DB, batchID string, files []catalogai.GroupFile) ([]catalogai.GroupCandidate, error) {
	pinned := map[string]bool{}
	for _, f := range files {
		if f.CurrentSet != "" {
			pinned[f.CurrentSet] = true
		}
	}
	var batchPacks []string
	if err := db.Model(&model.Material{}).Where("batch_id = ? AND pack_id <> ''", batchID).
		Distinct("pack_id").Limit(groupCandidateLimit).Pluck("pack_id", &batchPacks).Error; err != nil {
		return nil, err
	}
	for _, id := range batchPacks {
		pinned[id] = true
	}
	subjects, grades, cohorts := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, f := range files {
		for _, c := range f.Card.Subjects {
			subjects[c] = true
		}
		if f.Card.Grade != "" {
			grades[f.Card.Grade] = true
		}
		if f.Card.Cohort != 0 {
			cohorts[f.Card.Cohort] = true
		}
		if f.Card.SchoolYearStart != 0 && f.Card.Grade != "" {
			cohorts[catalogai.CohortFromSchoolYear(f.Card.SchoolYearStart, f.Card.Grade)] = true
		}
	}
	var packs []model.Pack
	if len(subjects) > 0 && (len(grades) > 0 || len(cohorts) > 0) {
		q := db.Model(&model.Pack{}).Where("id IN (?)",
			db.Model(&model.PackSubject{}).Select("pack_id").Where("code IN ?", keys(subjects)))
		gradeList, cohortList := keys(grades), intKeys(cohorts)
		switch {
		case len(gradeList) > 0 && len(cohortList) > 0:
			q = q.Where("(grade IN ? OR cohort IN ?)", gradeList, cohortList)
		case len(gradeList) > 0:
			q = q.Where("grade IN ?", gradeList)
		default:
			q = q.Where("cohort IN ?", cohortList)
		}
		if err := q.Order("updated_at DESC").Limit(300).Find(&packs).Error; err != nil {
			return nil, err
		}
	}
	if len(pinned) > 0 {
		var extra []model.Pack
		if err := db.Where("id IN ?", keys(pinned)).Find(&extra).Error; err != nil {
			return nil, err
		}
		packs = append(packs, extra...)
	}
	type scored struct {
		pack  model.Pack
		score float64
	}
	seen := map[string]bool{}
	ranked := []scored{}
	for _, p := range packs {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		score := 0.0
		if pinned[p.ID] {
			score += 10
		}
		for _, f := range files {
			name := p.ExamName
			if name == "" {
				name = p.Title
			}
			if f.Card.TitleText != "" {
				score = max(score, scoreBase(pinned[p.ID])+catalogai.Similarity(f.Card.TitleText, name))
			}
			for _, mention := range f.Card.OrgMentions {
				if p.Organization != "" && catalogai.Similarity(mention, p.Organization) >= 0.5 {
					score += 0.3
				}
			}
			if f.Card.ExamDate != "" && f.Card.ExamDate == p.ExamDate {
				score += 0.3
			}
		}
		ranked = append(ranked, scored{p, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > groupCandidateLimit {
		ranked = ranked[:groupCandidateLimit]
	}
	ids := make([]string, 0, len(ranked))
	for _, r := range ranked {
		ids = append(ids, r.pack.ID)
	}
	var members []model.Material
	if len(ids) > 0 {
		if err := db.Select("id", "pack_id", "part").Where("pack_id IN ?", ids).Order("position ASC").Find(&members).Error; err != nil {
			return nil, err
		}
	}
	memberIDs := make([]string, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.ID)
	}
	cards := loadCards(db, memberIDs)
	roles, prints := map[string][]string{}, map[string][]string{}
	for _, m := range members {
		roles[m.PackID] = append(roles[m.PackID], m.Part)
		if len(prints[m.PackID]) == 0 {
			prints[m.PackID] = cards[m.ID].Fingerprint
		}
	}
	out := make([]catalogai.GroupCandidate, 0, len(ranked))
	for _, r := range ranked {
		p := r.pack
		out = append(out, catalogai.GroupCandidate{
			ID: p.ID, Title: p.Title, ExamName: p.ExamName, DocType: p.DocType, Subjects: model.DecodeStrings(p.SubjectsJSON),
			Grade: p.Grade, Cohort: p.Cohort, Organization: p.Organization, ExamDate: p.ExamDate,
			Roles: append([]string{}, roles[p.ID]...), Fingerprint: prints[p.ID],
		})
	}
	return out, nil
}

func scoreBase(pinned bool) float64 {
	if pinned {
		return 10
	}
	return 0
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func intKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// commitGroup places the chunk's files into sets and queues their decisions.
func (s *Service) commitGroup(ctx context.Context, job model.Job, out *jobexec.GroupResult) error {
	if out == nil {
		return errors.New("分组结果为空")
	}
	var renamed []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		ids, err := s.applyGroupingTx(tx, job, out.Reply, out.Fallback)
		if err != nil {
			return err
		}
		renamed = ids
		message := "已分组：" + out.Reply.String()
		if out.Fallback {
			message = "没有得到可靠的分组，文件暂时单独成套，需要关注"
		}
		return s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded, message,
			map[string]any{"groups": len(out.Reply.Groups), "problems": len(out.Problems)})
	})
	if err != nil {
		return err
	}
	s.notify()
	s.renameMaterialFiles(context.WithoutCancel(ctx), renamed)
	return nil
}

// applyGroupingTx writes a grouping for the files the job was prepared with
// and queues the next chunk of the batch, if any.
func (s *Service) applyGroupingTx(tx *gorm.DB, job model.Job, reply catalogai.GroupReply, fallback bool) ([]string, error) {
	now := s.now()
	var latest model.Job
	if err := tx.Select("id", "payload_json").Where("id = ?", job.ID).First(&latest).Error; err != nil {
		return nil, err
	}
	var payload groupPayload
	_ = json.Unmarshal([]byte(latest.PayloadJSON), &payload)
	allowed := map[string]bool{}
	for _, id := range payload.Files {
		allowed[id] = true
	}
	var renamed []string
	for _, g := range reply.Groups {
		var members []model.Material
		for _, id := range g.Files {
			if !allowed[id] {
				continue
			}
			var m model.Material
			if err := tx.Where("id = ? AND ai_step = ?", id, model.StepGroup).First(&m).Error; err != nil {
				continue
			}
			members = append(members, m)
		}
		if len(members) == 0 {
			continue
		}
		target := g.Existing
		if target != "" {
			var count int64
			if err := tx.Model(&model.Pack{}).Where("id = ?", target).Count(&count).Error; err != nil {
				return nil, err
			}
			if count == 0 {
				target = ""
			}
		}
		if target == "" {
			id, err := s.newPackTx(tx, members[0].OriginalName, now)
			if err != nil {
				return nil, err
			}
			target = id
		}
		for _, m := range members {
			if m.PackID != target {
				old := m.PackID
				if err := s.placeMaterialTx(tx, m, target, now); err != nil {
					return nil, err
				}
				if old != "" {
					if err := s.membersChangedTx(tx, old, now); err != nil {
						return nil, err
					}
				}
			}
			reasons := carryReasons(model.DecodeStrings(m.AttentionJSON))
			if fallback && len(reply.Groups) > 1 {
				reasons = appendUnique(reasons, model.AttentionGroupUnverified)
			}
			if err := tx.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
				"ai_step": model.StepDecide, "stage": model.StageProcessing,
				"attention_json": model.EncodeStrings(reasons), "updated_at": now,
			}).Error; err != nil {
				return nil, err
			}
		}
		if err := s.membersChangedTx(tx, target, now); err != nil {
			return nil, err
		}
		ids, err := s.refreshPackNamesTx(tx, target)
		if err != nil {
			return nil, err
		}
		renamed = append(renamed, ids...)
	}
	// Anything the reply did not place goes back for the next chunk.
	return renamed, s.maybeEnqueueGroupTx(tx, job.BatchID, now)
}

// carryReasons keeps the attention reasons a file brings from earlier steps.
func carryReasons(reasons []string) []string {
	out := []string{}
	for _, r := range reasons {
		if r == model.AttentionMetadataOnly || r == model.AttentionGroupUnverified {
			out = append(out, r)
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// newPackTx creates an empty set. Its catalogue comes from the decision.
func (s *Service) newPackTx(tx *gorm.DB, placeholder string, now time.Time) (string, error) {
	title := truncateRunes(stripExt(placeholder), maxTitleRunes)
	if title == "" {
		title = "整理中"
	}
	pack := model.Pack{ID: newID(), Title: title, LockedJSON: "[]", SubjectsJSON: "[]", TagsJSON: "[]", CreatedAt: now, UpdatedAt: now}
	return pack.ID, wrap("create pack", tx.Create(&pack).Error)
}

func stripExt(name string) string {
	base := path.Base(name)
	return base[:len(base)-len(path.Ext(base))]
}

// placeMaterialTx puts a file at the end of a set, leaving its previous set
// (dropped when it becomes empty).
func (s *Service) placeMaterialTx(tx *gorm.DB, m model.Material, target string, now time.Time) error {
	if m.PackID != "" {
		return s.moveMaterialTx(tx, m, target, now)
	}
	var maxPos struct{ N int }
	if err := tx.Model(&model.Material{}).Select("COALESCE(MAX(position), -1) AS n").
		Where("pack_id = ?", target).Scan(&maxPos).Error; err != nil {
		return wrap("pack position", err)
	}
	return wrap("place material", tx.Model(&model.Material{}).Where("id = ?", m.ID).
		Updates(map[string]any{"pack_id": target, "position": maxPos.N + 1, "updated_at": now}).Error)
}

// membersChangedTx records that a set's members changed: older decisions
// become stale and a new one is queued. A set that no longer exists is skipped.
func (s *Service) membersChangedTx(tx *gorm.DB, packID string, now time.Time) error {
	res := tx.Model(&model.Pack{}).Where("id = ?", packID).
		Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": now})
	if res.Error != nil {
		return wrap("bump pack revision", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil
	}
	return s.enqueueDecideTx(tx, packID, now)
}

// applyGroupFallback keeps every file of a failed grouping on its own, so a
// model that keeps failing never leaves files without a set.
func (s *Service) applyGroupFallback(job model.Job) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		var latest model.Job
		if err := tx.Select("id", "payload_json").Where("id = ?", job.ID).First(&latest).Error; err != nil {
			return err
		}
		var payload groupPayload
		_ = json.Unmarshal([]byte(latest.PayloadJSON), &payload)
		var rows []model.Material
		if len(payload.Files) > 0 {
			if err := tx.Where("id IN ?", payload.Files).Find(&rows).Error; err != nil {
				return err
			}
		}
		in := catalogai.GroupInput{}
		for _, m := range rows {
			in.Files = append(in.Files, catalogai.GroupFile{ID: m.ID, CurrentSet: m.PackID})
		}
		if _, err := s.applyGroupingTx(tx, job, catalogai.Singletons(in), true); err != nil {
			return err
		}
		return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobFailed, "模型多次未能完成分组，文件暂时单独成套，需要关注")
	})
	if err != nil && !errors.Is(err, errLeaseLost) {
		s.logf("group fallback: %v", err)
	}
	s.notify()
}
