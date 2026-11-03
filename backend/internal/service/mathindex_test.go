package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

func TestBackfillMathIndexPreservesExistingQuestionsAndSearchesFormulas(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "math-backfill")
	material := model.Material{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleExam}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	texts := []string{
		"已知函数 x+y=2，求值。",
		"已知函数 x-y=2，求值。",
		`求函数 $x^{2}+1$ 的值。`,
		`已知分式 $\frac{x}{y}$，求值。`,
		"已知 x+10=3，求值。",
	}
	blocks := make([]model.QuestionBlock, 0, len(texts))
	for i, text := range texts {
		block := model.QuestionBlock{ID: newID(), MaterialID: material.ID, Seq: i, PageNo: i + 1, Text: text, Normalized: normalizeSearchText(text)}
		blocks = append(blocks, block)
	}
	if err := db.Create(&blocks).Error; err != nil {
		t.Fatal(err)
	}
	for _, block := range blocks {
		for term, tf := range searchTerms(block.Normalized) {
			if err := db.Create(&model.QuestionTerm{Term: term, QuestionID: block.ID, TF: tf}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx := context.Background()
	if count, err := svc.BackfillMathIndex(ctx); err != nil || count != len(blocks) {
		t.Fatalf("backfill = %d, %v", count, err)
	}
	if count, err := svc.BackfillMathIndex(ctx); err != nil || count != 0 {
		t.Fatalf("repeat backfill = %d, %v", count, err)
	}
	for _, tt := range []struct {
		query string
		want  string
	}{
		{"x+y", texts[0]},
		{"x-y", texts[1]},
		{"x²+1", texts[2]},
		{"x/y", texts[3]},
		{"x+10", texts[4]},
	} {
		result, err := svc.SearchQuestions(ctx, tt.query, "", "", 0, 20, 0)
		if err != nil || result.Total != 1 || result.Items[0].Question.Text != tt.want {
			t.Errorf("search %q = %+v, %v", tt.query, result, err)
			continue
		}
		hit := result.Items[0]
		if len(hit.Highlights) == 0 || hit.Highlights[0][1] > len([]rune(hit.Snippet)) {
			t.Errorf("search %q produced invalid highlights: %+v", tt.query, hit)
		}
	}
	result, err := svc.SearchQuestions(ctx, "x+1", "", "", 0, 20, 0)
	if err != nil || result.Total != 0 {
		t.Fatalf("partial operand must not match x+10: %+v, %v", result, err)
	}
	result, err = svc.SearchQuestions(ctx, "已知函数", "", "", 0, 20, 0)
	if err != nil || result.Total != 2 {
		t.Fatalf("existing text search changed: %+v, %v", result, err)
	}
	var saved model.QuestionBlock
	if err := db.First(&saved, "id = ?", blocks[2].ID).Error; err != nil || saved.Text != texts[2] || saved.MathIndexVersion != mathIndexVersion {
		t.Fatalf("original question was changed: %+v, %v", saved, err)
	}
	if !strings.Contains(saved.Text, `x^{2}`) {
		t.Fatal("original formula representation was not preserved")
	}

	// An older instance may write a legacy question after startup during a
	// rolling deployment. A formula search must backfill it before querying.
	late := model.QuestionBlock{ID: newID(), MaterialID: material.ID, PageNo: 6, Seq: 6, Text: "已知数列 x+y=2，求值。"}
	late.Normalized = normalizeSearchText(late.Text)
	if err := db.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	for term, tf := range searchTerms(late.Normalized) {
		if err := db.Create(&model.QuestionTerm{Term: term, QuestionID: late.ID, TF: tf}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err = svc.SearchQuestions(ctx, "函数 x+y", "", "", 0, 20, 0)
	if err != nil || result.Total != 1 || result.Items[0].Question.ID != blocks[0].ID {
		t.Fatalf("mixed text and formula search = %+v, %v", result, err)
	}
	result, err = svc.SearchQuestions(ctx, "x+y", "", "", 0, 20, 0)
	if err != nil || result.Total != 2 {
		t.Fatalf("late legacy question was not backfilled: %+v, %v", result, err)
	}
}

func TestSearchQuestionsPaginatesStableMatchedPassages(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "matched-passage")
	material := model.Material{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleExam}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	text := "这道题的说明。" + strings.Repeat("甲", 220) + "目标函数的最大值为多少？" + strings.Repeat("乙", 220)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var blocks []model.QuestionBlock
	var postings []model.QuestionTerm
	for i := 24; i >= 0; i-- {
		block := model.QuestionBlock{ID: fmt.Sprintf("q-%03d", i), MaterialID: material.ID, Seq: i, PageNo: 1, Text: text, Normalized: normalizeSearchText(text), MathIndexVersion: mathIndexVersion, CreatedAt: created}
		blocks = append(blocks, block)
		for term, tf := range questionIndexTerms(block.Text, block.Normalized) {
			postings = append(postings, model.QuestionTerm{Term: term, QuestionID: block.ID, TF: tf})
		}
	}
	if err := db.CreateInBatches(&blocks, 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&postings, 100).Error; err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(blocks); offset += 10 {
		result, err := svc.SearchQuestions(context.Background(), "目标函数的最大值", "", "", 0, 10, offset)
		if err != nil || result.Total != len(blocks) {
			t.Fatalf("offset %d: result=%+v error=%v", offset, result, err)
		}
		for index, hit := range result.Items {
			if want := fmt.Sprintf("q-%03d", offset+index); hit.Question.ID != want {
				t.Errorf("offset %d: got %s, want %s", offset, hit.Question.ID, want)
			}
			if !strings.HasPrefix(hit.Snippet, "…") || !strings.Contains(hit.Snippet, "目标函数的最大值") || len(hit.Highlights) != 1 {
				t.Fatalf("result did not contain the matched excerpt: %+v", hit)
			}
		}
	}
}

func TestFormulaSearchKeepsMatchesAheadOfTextOnlyCandidates(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "math-candidate-limit")
	material := model.Material{ID: newID(), Status: model.StatusArchived, MediaType: "application/pdf", Role: model.RoleExam}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	prose := "函数方程解析推理几何代数坐标求解结果"
	selected := make([]string, 0)
	for term := range searchTerms(normalizeSearchText(prose)) {
		selected = append(selected, term)
	}
	sort.Strings(selected)
	mathCount := len(mathTerms(mathFragments("x+y")))
	selected = selected[:24-mathCount]
	// Match enough prose to pass the mixed-query threshold, but fewer total
	// terms than the text-only distractors (even after adding math terms).
	var parts []string
	var partial string
	for _, term := range selected {
		parts = append(parts, term)
		candidate := strings.Join(parts, "，")
		terms := searchTerms(normalizeSearchText(candidate))
		matches := 0
		for _, selectedTerm := range selected {
			if terms[selectedTerm] > 0 {
				matches++
			}
		}
		if float64(matches)/float64(len(selected)) >= 0.35 {
			if matches+mathCount < len(selected) {
				partial = candidate
			}
			break
		}
	}
	if partial == "" {
		t.Fatal("could not construct a partially matching prose fixture")
	}
	blocks := make([]model.QuestionBlock, 0, maxSearchCandidates+2)
	for i := 0; i <= maxSearchCandidates; i++ {
		blocks = append(blocks, model.QuestionBlock{ID: newID(), MaterialID: material.ID, Seq: i, PageNo: 1, Text: prose, Normalized: normalizeSearchText(prose), MathIndexVersion: mathIndexVersion})
	}
	answer := model.QuestionBlock{ID: newID(), MaterialID: material.ID, Seq: len(blocks), PageNo: 1, Text: partial + "，x+y=2", MathIndexVersion: mathIndexVersion}
	answer.Normalized = normalizeSearchText(answer.Text)
	blocks = append(blocks, answer)
	if err := db.CreateInBatches(&blocks, 100).Error; err != nil {
		t.Fatal(err)
	}
	postings := make([]model.QuestionTerm, 0, len(blocks)*len(selected))
	for _, block := range blocks {
		for term, tf := range questionIndexTerms(block.Text, block.Normalized) {
			postings = append(postings, model.QuestionTerm{Term: term, QuestionID: block.ID, TF: tf})
		}
	}
	if err := db.CreateInBatches(&postings, 100).Error; err != nil {
		t.Fatal(err)
	}
	result, err := svc.SearchQuestions(context.Background(), prose+" x+y", "", "", 0, 20, 0)
	if err != nil || result.Total != 1 || result.Items[0].Question.ID != answer.ID {
		t.Fatalf("formula was crowded out by text-only candidates: %+v, %v", result, err)
	}
}

func TestBackfillMathIndexConcurrentAndBatched(t *testing.T) {
	dir := t.TempDir()
	first, db := newWorkerTestService(t, dir, "math-first")
	second, _ := newWorkerTestService(t, dir, "math-second")
	blocks := make([]model.QuestionBlock, 0, 120)
	for i := 0; i < 120; i++ {
		blocks = append(blocks, model.QuestionBlock{ID: newID(), MaterialID: "material", Seq: i, PageNo: 1, Text: "求 x+y=2", Normalized: "求xy2"})
	}
	if err := db.CreateInBatches(&blocks, 40).Error; err != nil {
		t.Fatal(err)
	}
	type result struct {
		count int
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, svc := range []*Service{first, second} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			count, err := s.BackfillMathIndex(context.Background())
			results <- result{count: count, err: err}
		}(svc)
	}
	wg.Wait()
	close(results)
	total := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += result.count
	}
	if total != len(blocks) {
		t.Fatalf("backfilled %d questions, want %d", total, len(blocks))
	}
	var count int64
	if err := db.Model(&model.QuestionTerm{}).Where("term = ?", "m:x+y").Count(&count).Error; err != nil || count != int64(len(blocks)) {
		t.Fatalf("formula postings = %d, %v", count, err)
	}
}

func TestBackfillMathIndexRollsBackOnPostingFailure(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "math-rollback")
	block := model.QuestionBlock{ID: newID(), MaterialID: "material", Seq: 0, PageNo: 1, Text: "x+y=2", Normalized: "xy2"}
	if err := db.Create(&block).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail-math-posting", func(tx *gorm.DB) {
		if tx.Statement.Table == "question_terms" {
			tx.AddError(errors.New("simulated posting failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BackfillMathIndex(context.Background()); err == nil {
		t.Fatal("posting failure was not reported")
	}
	if err := db.First(&block, "id = ?", block.ID).Error; err != nil || block.MathIndexVersion != 0 {
		t.Fatalf("failed backfill changed the version: %+v, %v", block, err)
	}
	var count int64
	if err := db.Model(&model.QuestionTerm{}).Where("question_id = ?", block.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed backfill left postings: %d, %v", count, err)
	}
}
