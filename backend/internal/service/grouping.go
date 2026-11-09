package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// Pairing threshold: plausible matches are sent to the review inbox for confirmation.
const (
	pairSuggestScore = 40
	pairCandidates   = 300
)

// enqueueGroupTx queues pairing for a freshly analysed file. One pending or
// running group job per file is enough.
func (s *Service) enqueueGroupTx(tx *gorm.DB, materialID string, now time.Time) error {
	var open int64
	if err := tx.Model(&model.Job{}).Where("kind = ? AND material_id = ? AND status IN ?",
		model.JobGroup, materialID, []string{model.JobPending, model.JobRunning}).Count(&open).Error; err != nil {
		return fmt.Errorf("count group jobs: %w", err)
	}
	if open > 0 {
		return nil
	}
	return tx.Create(&model.Job{ID: newID(), Kind: model.JobGroup, Status: model.JobPending, MaterialID: materialID,
		CreatedAt: now, UpdatedAt: now}).Error
}

// RunGroup tries to place one analysed file into an existing pack.
func (s *Service) RunGroup(ctx context.Context, jobID string) {
	db := s.db.WithContext(ctx)
	var job model.Job
	if err := db.Where("id = ?", jobID).First(&job).Error; err != nil || job.Kind != model.JobGroup {
		return
	}
	s.jobProgress(ctx, job, "正在查找可配套的试卷", 0, 0)
	var renamed []string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		message, ids, metrics, err := s.pairMaterialTx(tx, job.MaterialID, s.now())
		if err != nil {
			return err
		}
		renamed = ids
		return s.finishJobTxWithMetrics(tx, job.ID, job.LeaseOwner, model.JobSucceeded, message, metrics)
	})
	if err != nil {
		if ctx.Err() != nil {
			if !errors.Is(context.Cause(ctx), errLeaseLost) {
				s.requeueJob(job.ID, job.LeaseOwner)
			}
			return
		}
		if errors.Is(err, errLeaseLost) {
			return
		}
		s.logf("group job failed: %v", err)
		s.failJob(job.ID, job.LeaseOwner, publicMessage(err))
		return
	}
	s.notify()
	s.renameMaterialFiles(context.WithoutCancel(ctx), renamed)
}

type pairCandidate struct {
	Pack  model.Pack
	Score int
}

// pairMaterialTx merges a lone exam file into the best matching pack, or
// records a suggestion for the inbox when the match is not decisive.
func (s *Service) pairMaterialTx(tx *gorm.DB, materialID string, now time.Time) (string, []string, map[string]any, error) {
	var m model.Material
	if err := tx.Where("id = ?", materialID).First(&m).Error; err != nil {
		return "文件已删除，无需配套", nil, nil, nil
	}
	var pack model.Pack
	if err := tx.Where("id = ?", m.PackID).First(&pack).Error; err != nil {
		return "套卷不存在，无需配套", nil, nil, nil
	}
	var members int64
	if err := tx.Model(&model.Material{}).Where("pack_id = ?", pack.ID).Count(&members).Error; err != nil {
		return "", nil, nil, err
	}
	if members > 1 {
		return "已在多文件套卷中，无需配套", nil, nil, nil
	}
	if !examDocTypes[pack.DocType] || !pairablePart(m.Part) {
		return "不是试卷类文件，无需配套", nil, nil, nil
	}

	candidates, err := s.pairCandidatesTx(tx, pack, m.Part)
	if err != nil {
		return "", nil, nil, err
	}
	metrics := map[string]any{"candidate_count": len(candidates)}
	if len(candidates) == 0 {
		return "没有找到可配套的试卷", nil, metrics, nil
	}
	best := candidates[0]
	second := 0
	if len(candidates) > 1 {
		second = candidates[1].Score
	}
	metrics["best_score"], metrics["second_score"] = best.Score, second
	if best.Score >= pairSuggestScore {
		reasons := model.DecodeStrings(m.ReviewReasonsJSON)
		if m.Stage != model.StageReview {
			reasons = nil
		}
		if !containsString(reasons, model.ReviewPackSuggestion) {
			reasons = append(reasons, model.ReviewPackSuggestion)
		}
		if err := s.setStageTx(tx, m.ID, model.StageReview, reasons, best.Pack.ID); err != nil {
			return "", nil, nil, err
		}
		return "找到可能配套的《" + best.Pack.Title + "》，等待确认", nil, metrics, nil
	}
	return "没有足够把握的配套候选", nil, metrics, nil
}

func pairablePart(part string) bool {
	switch part {
	case model.PartQuestions, model.PartAnswers, model.PartFull, model.PartTeacher, model.PartAnswerSheet, model.PartListening:
		return true
	}
	return false
}

func (s *Service) pairCandidatesTx(tx *gorm.DB, pack model.Pack, part string) ([]pairCandidate, error) {
	q := tx.Model(&model.Pack{}).Where("id <> ?", pack.ID).Where("doc_type IN ?", examDocTypeList())
	cond := []string{}
	args := []any{}
	if pack.ExamName != "" {
		cond = append(cond, "exam_name = ?")
		args = append(args, pack.ExamName)
	}
	if pack.Title != "" {
		cond = append(cond, "title = ?")
		args = append(args, pack.Title)
	}
	if pack.Cohort != 0 && pack.Grade != "" {
		cond = append(cond, "(cohort = ? AND grade = ?)")
		args = append(args, pack.Cohort, pack.Grade)
	}
	if len(cond) == 0 {
		return nil, nil
	}
	var rows []model.Pack
	if err := q.Where("("+strings.Join(cond, " OR ")+")", args...).Order("updated_at DESC").Limit(pairCandidates).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load pair candidates: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	var memberRows []model.Material
	if err := tx.Select("pack_id", "part").Where("pack_id IN ?", ids).Find(&memberRows).Error; err != nil {
		return nil, fmt.Errorf("load candidate parts: %w", err)
	}
	parts := map[string][]string{}
	for _, mr := range memberRows {
		parts[mr.PackID] = append(parts[mr.PackID], mr.Part)
	}
	out := []pairCandidate{}
	for _, c := range rows {
		score, ok := scorePair(pack, part, c, parts[c.ID])
		if ok && score > 0 {
			out = append(out, pairCandidate{Pack: c, Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// scorePair compares catalogue fields. Any known-but-different field rules a
// candidate out; agreement adds evidence. A pack that already holds the same
// part cannot take another copy of it.
func scorePair(own model.Pack, part string, cand model.Pack, candParts []string) (int, bool) {
	if containsString(candParts, part) {
		return 0, false
	}
	if part == model.PartFull || part == model.PartTeacher {
		if containsString(candParts, model.PartFull) || containsString(candParts, model.PartTeacher) {
			return 0, false
		}
	}
	score := 0
	field := func(a, b string, weight int) bool {
		if a == "" || b == "" {
			return true
		}
		if a != b {
			return false
		}
		score += weight
		return true
	}
	if !field(own.DocType, cand.DocType, 5) || !field(own.Grade, cand.Grade, 10) || !field(own.Term, cand.Term, 5) ||
		!field(own.Region, cand.Region, 10) || !field(own.Organization, cand.Organization, 15) || !field(own.ExamDate, cand.ExamDate, 10) {
		return 0, false
	}
	if own.Cohort != 0 && cand.Cohort != 0 {
		if own.Cohort != cand.Cohort {
			return 0, false
		}
		score += 10
	}
	ownSubjects, candSubjects := model.DecodeStrings(own.SubjectsJSON), model.DecodeStrings(cand.SubjectsJSON)
	if len(ownSubjects) > 0 && len(candSubjects) > 0 {
		if !intersects(ownSubjects, candSubjects) {
			return 0, false
		}
		score += 10
	}
	if a, b := normalizeExamName(own.ExamName), normalizeExamName(cand.ExamName); a != "" && b != "" {
		switch {
		case a == b:
			score += 50
		case len([]rune(a)) >= 8 && len([]rune(b)) >= 8 && (strings.Contains(a, b) || strings.Contains(b, a)):
			score += 30
		}
	}
	if own.Title != "" && own.Title == cand.Title {
		score += 20
	}
	hasQ, hasA, _ := packCompleteness(candParts)
	if (part == model.PartAnswers && hasQ && !hasA) || (part == model.PartQuestions && hasA && !hasQ) {
		score += 10
	}
	return score, true
}

func normalizeExamName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(stemNoise.ReplaceAllString(raw, "")) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func intersects(left, right []string) bool {
	set := map[string]bool{}
	for _, v := range left {
		set[v] = true
	}
	for _, v := range right {
		if set[v] {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
