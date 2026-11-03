package service

import (
	"context"
	"fmt"

	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	mathIndexVersion = 1
	mathIndexBatch   = 50
)

func questionIndexTerms(text, normalized string) map[string]int {
	terms := searchTerms(normalized)
	for term, count := range mathTerms(mathFragments(text)) {
		terms[term] = count
	}
	return terms
}

// lockQuestionIDs coordinates old-index backfill with question replacement and
// deletion. PostgreSQL must take the question row locks before touching terms;
// SQLite's single connection and immediate transactions already serialize it.
func lockQuestionIDs(tx *gorm.DB, materialIDs []string) ([]string, error) {
	if len(materialIDs) == 0 {
		return nil, nil
	}
	q := tx.Model(&model.QuestionBlock{}).Select("id").Where("material_id IN ?", materialIDs).Order("id ASC")
	if tx.Dialector.Name() == "postgres" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []model.QuestionBlock
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// BackfillMathIndex adds formula postings to existing questions without calling
// OCR or the model and without replacing their original text or old postings.
// Bounded transactions and row locks make repeated or concurrent starts safe.
func (s *Service) BackfillMathIndex(ctx context.Context) (int, error) {
	total := 0
	for {
		count := 0
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			q := tx.Where("math_index_version < ?", mathIndexVersion).Order("id ASC").Limit(mathIndexBatch)
			if tx.Dialector.Name() == "postgres" {
				q = q.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			var blocks []model.QuestionBlock
			if err := q.Find(&blocks).Error; err != nil {
				return err
			}
			if len(blocks) == 0 {
				return nil
			}
			ids := make([]string, 0, len(blocks))
			terms := make([]model.QuestionTerm, 0, 1000)
			flush := func() error {
				if len(terms) == 0 {
					return nil
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&terms, 100).Error; err != nil {
					return err
				}
				terms = terms[:0]
				return nil
			}
			for _, block := range blocks {
				ids = append(ids, block.ID)
				for term, tf := range mathTerms(mathFragments(block.Text)) {
					terms = append(terms, model.QuestionTerm{Term: term, QuestionID: block.ID, TF: tf})
					if len(terms) >= 1000 {
						if err := flush(); err != nil {
							return err
						}
					}
				}
			}
			if err := flush(); err != nil {
				return err
			}
			if err := tx.Model(&model.QuestionBlock{}).Where("id IN ?", ids).Update("math_index_version", mathIndexVersion).Error; err != nil {
				return err
			}
			count = len(blocks)
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("补建公式索引: %w", err)
		}
		total += count
		if count == 0 {
			// A concurrent PostgreSQL transaction can change the rows selected
			// while this one waits for their locks. Recheck before finishing.
			var remaining int64
			if err := s.db.WithContext(ctx).Model(&model.QuestionBlock{}).Where("math_index_version < ?", mathIndexVersion).Count(&remaining).Error; err != nil {
				return total, fmt.Errorf("检查公式索引: %w", err)
			}
			if remaining == 0 {
				return total, nil
			}
		}
	}
}
