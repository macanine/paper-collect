package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/searchtext"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	searchPromptVersion = "qseg-v1"
	maxQuestionRunes    = 8000
	minQuestionRunes    = 5
	maxSearchCandidates = 2000
)

func defaultSearchSettings() model.SearchSettings {
	return model.SearchSettings{
		ID: 1, Enabled: true, MaxPages: 500, BatchPages: 10, BatchChars: 12000,
	}
}

// materialNeedsQuestionIndex: only files that contain the questions are cut
// into searchable items. Answer files and teacher copies would duplicate them.
func materialNeedsQuestionIndex(part string) bool {
	return part == model.PartQuestions || part == model.PartFull
}

var indexParts = []string{model.PartQuestions, model.PartFull}

func (s *Service) ensureSearchSettings(ctx context.Context) (model.SearchSettings, error) {
	db := s.db.WithContext(ctx)
	var row model.SearchSettings
	err := db.Where("id = ?", 1).First(&row).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SearchSettings{}, apperr.Internal(err)
	}
	row = defaultSearchSettings()
	row.UpdatedAt = s.now()
	if err := db.Create(&row).Error; err != nil {
		if err := db.Where("id = ?", 1).First(&row).Error; err != nil {
			return model.SearchSettings{}, apperr.Internal(err)
		}
	}
	return row, nil
}

// GetSearchSettings returns the singleton search configuration.
func (s *Service) GetSearchSettings(ctx context.Context) (model.SearchSettings, error) {
	return s.ensureSearchSettings(ctx)
}

// PutSearchSettings validates and stores the complete search configuration.
func (s *Service) PutSearchSettings(ctx context.Context, p auth.Principal, in model.SearchSettings) (model.SearchSettings, error) {
	if in.MaxPages < 1 || in.MaxPages > 5000 {
		return model.SearchSettings{}, apperr.Validation("max_pages 必须在 1 到 5000 之间")
	}
	if in.BatchPages < 1 || in.BatchPages > 50 {
		return model.SearchSettings{}, apperr.Validation("batch_pages 必须在 1 到 50 之间")
	}
	if in.BatchChars < 1000 || in.BatchChars > 100000 {
		return model.SearchSettings{}, apperr.Validation("batch_chars 必须在 1000 到 100000 之间")
	}
	if len([]rune(strings.TrimSpace(in.Model))) > 200 {
		return model.SearchSettings{}, apperr.Validation("模型名不能超过 200 个字符")
	}
	if _, err := s.ensureSearchSettings(ctx); err != nil {
		return model.SearchSettings{}, err
	}
	updates := map[string]any{
		"enabled": in.Enabled, "max_pages": in.MaxPages, "batch_pages": in.BatchPages,
		"batch_chars": in.BatchChars, "provider_id": strings.TrimSpace(in.ProviderID),
		"model": strings.TrimSpace(in.Model), "updated_at": s.now(),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SearchSettings{}).Where("id = ?", 1).Updates(updates).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "search.settings_update", "1")
	}); err != nil {
		return model.SearchSettings{}, apperr.Internal(err)
	}
	return s.ensureSearchSettings(ctx)
}

// GetMaterialIndexState returns the current state or marks ineligible roles as
// not applicable without persisting a synthetic row.
func (s *Service) GetMaterialIndexState(ctx context.Context, materialID string) (model.MaterialIndexState, error) {
	var material model.Material
	if err := s.db.WithContext(ctx).Select("id", "part").Where("id = ?", materialID).First(&material).Error; err != nil {
		return model.MaterialIndexState{}, apperr.NotFound("资料不存在")
	}
	if !materialNeedsQuestionIndex(material.Part) {
		return model.MaterialIndexState{MaterialID: materialID, Status: model.IndexStatusNotApplicable}, nil
	}
	var row model.MaterialIndexState
	err := s.db.WithContext(ctx).Where("material_id = ?", materialID).First(&row).Error
	if err == nil {
		return row, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.MaterialIndexState{MaterialID: materialID, Status: "pending"}, nil
	}
	return model.MaterialIndexState{}, apperr.Internal(err)
}

// EnqueueMaterialIndex queues a manual reindex. force=false reuses cached pages.
func (s *Service) EnqueueMaterialIndex(ctx context.Context, p auth.Principal, materialID string, force bool) (model.MaterialIndexState, string, error) {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", materialID).First(&material).Error; err != nil {
		return model.MaterialIndexState{}, "", apperr.NotFound("资料不存在")
	}
	if material.MediaType == "application/zip" {
		return model.MaterialIndexState{}, "", apperr.Validation("ZIP 容器本身不索引，请索引展开后的资料")
	}
	if !materialNeedsQuestionIndex(material.Part) {
		return model.MaterialIndexState{}, "", apperr.Validation("只有试卷和试卷+答案资料需要建立题目索引")
	}
	now := s.now()
	jobID := newID()
	var state model.MaterialIndexState
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("material_id = ?", materialID).First(&state).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state.MaterialID = materialID
		state.Status = "pending"
		state.JobID = jobID
		state.Error = ""
		state.UpdatedAt = now
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		job := model.Job{ID: jobID, Kind: model.JobIndex, Status: model.JobPending, MaterialID: materialID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		return s.audit(tx, p, "search.index_enqueue", materialID)
	})
	if err != nil {
		return model.MaterialIndexState{}, "", apperr.Internal(err)
	}
	if force {
		_ = s.db.WithContext(ctx).Model(&model.MaterialText{}).Where("material_id = ?", materialID).
			Update("source_hash", "").Error
	}
	s.notify()
	return state, jobID, nil
}

// errJobDone means the job completed during preparation and needs no executor.
var errJobDone = errors.New("job already complete")

// prepareIndex marks the index state running and builds the executor input.
func (s *Service) prepareIndex(ctx context.Context, job model.Job, ocrProviderID string) (*jobexec.IndexInput, error) {
	db := s.db.WithContext(ctx)
	var material model.Material
	if err := db.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
		return nil, apperr.NotFound("资料不存在")
	}
	if !materialNeedsQuestionIndex(material.Part) {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := deleteQuestionIndexForMaterials(tx, []string{material.ID}); err != nil {
				return err
			}
			return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "")
		})
		if err != nil {
			return nil, err
		}
		return nil, errJobDone
	}
	settings, err := s.ensureSearchSettings(ctx)
	if err != nil {
		return nil, apperr.Unavailable("读取搜索设置失败")
	}
	_ = db.Model(&model.MaterialIndexState{}).Where("material_id = ?", material.ID).
		Updates(map[string]any{"status": "running", "job_id": job.ID, "error": "", "updated_at": s.now()}).Error
	aiSettings, err := s.ensureAISettings(ctx)
	if err != nil {
		return nil, err
	}
	ocr, key, err := s.ocrConfigForProvider(ctx, aiSettings, ocrProviderID, material.MediaType)
	if err != nil {
		return nil, err
	}
	provider, modelName, err := s.segmentationTarget(ctx, settings)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.openAISecret(provider.APIKeyCipher, provider.ID)
	if err != nil {
		return nil, err
	}
	in := &jobexec.IndexInput{
		Blob: jobexec.Blob{SHA256: material.SHA256, Size: material.SizeBytes, FileName: material.FileName, MediaType: material.MediaType},
		OCR:  ocr, ParseKey: key,
		Model:    jobexec.ModelConfig{ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: apiKey, Model: modelName},
		MaxPages: settings.MaxPages, BatchPages: settings.BatchPages, BatchChars: settings.BatchChars,
	}
	if cached := s.cachedText(ctx, material, key); cached != nil && len(cached.Pages) > 0 {
		in.Cached = cached
	}
	return in, nil
}

// commitIndex filters and deduplicates the segmented questions, then replaces
// the material's index in one transaction.
func (s *Service) commitIndex(ctx context.Context, job model.Job, out *jobexec.IndexResult) error {
	if out == nil {
		return errors.New("索引结果为空")
	}
	db := s.db.WithContext(ctx)
	if out.Parsed != nil {
		s.saveParsedText(ctx, job.MaterialID, out.Parsed)
	}
	questions := collectQuestions(out.Batches, out.Questions)
	if len(questions) == 0 {
		s.logf("segmentation produced no valid questions (pages=%d calls=%d)", out.PageCount, out.Calls)
		s.failIndex(job.ID, job.LeaseOwner, job.MaterialID, "LLM 未返回有效题目", map[string]any{
			"llm_calls": out.Calls, "llm_failed_batches": out.Failed,
		})
		return nil
	}
	var material model.Material
	if err := db.Where("id = ?", job.MaterialID).First(&material).Error; err != nil {
		return err
	}
	modelName := out.Model
	calls, failedBatches, originalPageCount := out.Calls, out.Failed, out.PageCount
	now := s.now()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := s.lockJobLease(tx, job.ID, job.LeaseOwner); err != nil {
			return err
		}
		oldIDs, err := lockQuestionIDs(tx, []string{material.ID})
		if err != nil {
			return err
		}
		if len(oldIDs) > 0 {
			if err := tx.Where("question_id IN ?", oldIDs).Delete(&model.QuestionTerm{}).Error; err != nil {
				return err
			}
			if err := tx.Where("material_id = ?", material.ID).Delete(&model.QuestionBlock{}).Error; err != nil {
				return err
			}
		}
		blocks := make([]model.QuestionBlock, 0, len(questions))
		for i, q := range questions {
			block := model.QuestionBlock{
				ID: newID(), MaterialID: material.ID, PageNo: q.PageNo, Seq: i,
				QuestionNo: q.QuestionNo, Text: q.Text, Normalized: searchtext.Normalize(q.Text),
				MathIndexVersion: mathIndexVersion, Confidence: q.Confidence, Model: modelName, PromptVersion: searchPromptVersion, CreatedAt: now,
			}
			blocks = append(blocks, block)
		}
		if len(blocks) > 0 {
			if err := tx.CreateInBatches(&blocks, 50).Error; err != nil {
				return err
			}
		}
		terms := make([]model.QuestionTerm, 0, 1000)
		flushTerms := func() error {
			if len(terms) == 0 {
				return nil
			}
			if err := tx.CreateInBatches(&terms, 100).Error; err != nil {
				return err
			}
			terms = terms[:0]
			return nil
		}
		for _, block := range blocks {
			for term, tf := range questionIndexTerms(block.Text, block.Normalized) {
				terms = append(terms, model.QuestionTerm{Term: term, QuestionID: block.ID, TF: tf})
				if len(terms) == cap(terms) {
					if err := flushTerms(); err != nil {
						return err
					}
				}
			}
		}
		if err := flushTerms(); err != nil {
			return err
		}
		indexedAt := now
		res := tx.Model(&model.MaterialIndexState{}).
			Where("material_id = ? AND job_id = ?", material.ID, job.ID).
			Updates(map[string]any{
				"status": "succeeded", "page_count": originalPageCount, "question_count": len(questions),
				"llm_calls": calls, "llm_failed_batches": failedBatches, "error": "",
				"indexed_at": indexedAt, "updated_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errLeaseLost
		}
		return s.finishJobTx(tx, job.ID, job.LeaseOwner, model.JobSucceeded, "")
	})
	if err != nil && !errors.Is(err, errLeaseLost) {
		s.logf("index %s: write: %v", material.ID, err)
		s.failIndex(job.ID, job.LeaseOwner, material.ID, "写入索引失败")
		return nil
	}
	return err
}

// collectQuestions keeps questions whose page belongs to their batch, drops
// overlap duplicates and keeps the longest text per page and question number.
func collectQuestions(batches [][]jobexec.Page, results [][]jobexec.Question) []jobexec.Question {
	var all []jobexec.Question
	seen := map[string]bool{}
	byNumber := map[string]int{}
	for index, questions := range results {
		if index >= len(batches) {
			break
		}
		allowed := map[int]bool{}
		for _, page := range batches[index] {
			allowed[page.PageNo] = true
		}
		for _, q := range questions {
			if !allowed[q.PageNo] || len([]rune(q.Text)) < minQuestionRunes || len([]rune(q.Text)) > maxQuestionRunes {
				continue
			}
			normalized := searchtext.Normalize(q.Text)
			dedupeKey := fmt.Sprintf("%d|%s", q.PageNo, searchtext.QuestionDedupeText(q.Text))
			if normalized == "" || seen[dedupeKey] {
				continue
			}
			seen[dedupeKey] = true
			if q.QuestionNo != "" {
				numberKey := fmt.Sprintf("%d|%s", q.PageNo, q.QuestionNo)
				if i, ok := byNumber[numberKey]; ok {
					if len([]rune(q.Text)) > len([]rune(all[i].Text)) {
						all[i] = q
					}
					continue
				}
				byNumber[numberKey] = len(all)
			}
			if q.Confidence < 0 {
				q.Confidence = 0
			}
			if q.Confidence > 1 {
				q.Confidence = 1
			}
			all = append(all, q)
		}
	}
	return all
}

func (s *Service) failIndex(jobID, owner, materialID, message string, extra ...map[string]any) {
	message = jobexec.Redact(message)
	updates := map[string]any{"status": "failed", "error": message, "updated_at": s.now()}
	for _, values := range extra {
		for key, value := range values {
			updates[key] = value
		}
	}
	s.failJob(jobID, owner, message, func(tx *gorm.DB) error {
		return tx.Model(&model.MaterialIndexState{}).
			Where("material_id = ? AND job_id = ?", materialID, jobID).
			Updates(updates).Error
	})
}

func (s *Service) segmentationTarget(ctx context.Context, settings model.SearchSettings) (model.AIProvider, string, error) {
	var providers []model.AIProvider
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("priority ASC").Find(&providers).Error; err != nil {
		return model.AIProvider{}, "", err
	}
	if len(providers) == 0 {
		return model.AIProvider{}, "", errors.New("没有启用的 AI 模型端点")
	}
	var provider model.AIProvider
	if settings.ProviderID != "" {
		for _, item := range providers {
			if item.ID == settings.ProviderID {
				provider = item
				break
			}
		}
		if provider.ID == "" {
			return model.AIProvider{}, "", errors.New("搜索设置指定的模型端点未启用")
		}
	} else {
		provider = providers[0]
	}
	modelName := strings.TrimSpace(settings.Model)
	if modelName == "" {
		modelName = provider.DefaultModel
	}
	if modelName == "" {
		return model.AIProvider{}, "", errors.New("切题模型未配置")
	}
	return provider, modelName, nil
}

// QuestionHit is one search result. The same question text found in other
// packs is folded into AlsoIn instead of repeating the hit.
type QuestionHit struct {
	Question   model.QuestionBlock
	Material   model.Material
	Pack       model.Pack
	Subjects   []string
	AlsoIn     []model.Pack
	Snippet    string
	Highlights [][2]int
	Score      float64
}

// SearchResult is a page of question hits.
type SearchResult struct {
	Items  []QuestionHit
	Total  int
	TookMS int64
}

// SearchQuestions performs pure-text question search.
func (s *Service) SearchQuestions(ctx context.Context, query, subject, grade string, year, limit, offset int) (SearchResult, error) {
	started := time.Now()
	normalized := searchtext.Normalize(query)
	wantedMath := searchtext.MathFragments(query)
	mathQueryTerms := searchtext.MathTerms(wantedMath)
	if len(mathQueryTerms) > 0 {
		// Also catch questions written by an older instance during a rolling
		// upgrade. Backfill is idempotent and never calls OCR or a model.
		if _, err := s.BackfillMathIndex(ctx); err != nil {
			return SearchResult{}, apperr.Internal(err)
		}
	}
	if len([]rune(normalized)) == 0 && len(mathQueryTerms) == 0 {
		return SearchResult{Items: []QuestionHit{}}, nil
	}
	textQuery := normalized
	if len(wantedMath) > 0 {
		textQuery = searchtext.Normalize(searchtext.TextOutsideMath(query, wantedMath))
	}
	terms := searchtext.Terms(textQuery)
	if len(terms) == 0 && len(mathQueryTerms) == 0 {
		return SearchResult{Items: []QuestionHit{}}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// Formula postings go first so a long Chinese query cannot crowd them out.
	// Sort both groups to keep candidate selection stable across requests.
	mathList := make([]string, 0, len(mathQueryTerms))
	for term := range mathQueryTerms {
		mathList = append(mathList, term)
	}
	sort.Slice(mathList, func(i, j int) bool {
		left, right := len([]rune(mathList[i])), len([]rune(mathList[j]))
		if left != right {
			return left > right
		}
		return mathList[i] < mathList[j]
	})
	if len(mathList) > 16 {
		mathList = mathList[:16]
	}
	textList := make([]string, 0, len(terms))
	for term := range terms {
		textList = append(textList, term)
	}
	sort.Strings(textList)
	termList := append(mathList, textList...)
	if len(termList) > 24 {
		termList = termList[:24]
	}
	selectedTextTerms := termList[len(mathList):]
	args := []any{}
	var where strings.Builder
	where.WriteString(` FROM question_terms qt
		JOIN question_blocks qb ON qb.id = qt.question_id
		JOIN materials m ON m.id = qb.material_id
		JOIN packs p ON p.id = m.pack_id
		WHERE qt.term IN ? AND m.status IN ('archived','missing') AND m.deleted_at IS NULL AND p.deleted_at IS NULL`)
	args = append(args, termList)
	if len(mathList) > 0 {
		// A matching formula contains every selected formula n-gram. Require
		// these before applying the candidate limit so common prose alone
		// cannot displace the actual formula matches.
		where.WriteString(` AND EXISTS (SELECT 1 FROM question_terms qm
			WHERE qm.question_id = qb.id AND qm.term IN ?
			GROUP BY qm.question_id HAVING COUNT(DISTINCT qm.term) = ?)`)
		args = append(args, mathList, len(mathList))
	}
	if subject != "" {
		where.WriteString(` AND EXISTS (SELECT 1 FROM pack_subjects ps WHERE ps.pack_id = m.pack_id AND ps.code = ?)`)
		args = append(args, subject)
	}
	if grade != "" {
		where.WriteString(` AND p.grade = ?`)
		args = append(args, grade)
	}
	if year > 0 {
		where.WriteString(` AND p.cohort = ?`)
		args = append(args, year)
	}
	type candidate struct {
		QuestionID string
		TFSum      int
		Matched    int
	}
	var candidates []candidate
	q := `SELECT qt.question_id AS question_id, SUM(qt.tf) AS tf_sum, COUNT(DISTINCT qt.term) AS matched` +
		where.String() + ` GROUP BY qt.question_id ORDER BY matched DESC, tf_sum DESC, qt.question_id ASC LIMIT ?`
	args = append(args, maxSearchCandidates)
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&candidates).Error; err != nil {
		return SearchResult{}, apperr.Internal(err)
	}
	if len(candidates) == 0 {
		return SearchResult{Items: []QuestionHit{}, TookMS: time.Since(started).Milliseconds()}, nil
	}
	ids := make([]string, 0, len(candidates))
	byID := map[string]candidate{}
	for _, c := range candidates {
		ids = append(ids, c.QuestionID)
		byID[c.QuestionID] = c
	}
	var blocks []model.QuestionBlock
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&blocks).Error; err != nil {
		return SearchResult{}, apperr.Internal(err)
	}
	materialIDs := make([]string, 0, len(blocks))
	for _, block := range blocks {
		materialIDs = append(materialIDs, block.MaterialID)
	}
	var materials []model.Material
	if err := s.db.WithContext(ctx).Where("id IN ?", materialIDs).Find(&materials).Error; err != nil {
		return SearchResult{}, apperr.Internal(err)
	}
	materialMap := map[string]model.Material{}
	for _, m := range materials {
		materialMap[m.ID] = m
	}
	packMap, err := s.packsForMaterials(ctx, materials)
	if err != nil {
		return SearchResult{}, apperr.Internal(err)
	}
	fullHits := make([]QuestionHit, 0, len(blocks))
	queryTermCount := len(termList)
	for _, block := range blocks {
		material, ok := materialMap[block.MaterialID]
		if !ok {
			continue
		}
		c := byID[block.ID]
		if len(wantedMath) > 0 {
			_, ok := searchtext.MatchingMathFragment(block.Text, wantedMath)
			if !ok {
				continue
			}
			if len(selectedTextTerms) > 0 {
				blockTerms := searchtext.Terms(block.Normalized)
				matched := 0
				for _, term := range selectedTextTerms {
					if blockTerms[term] > 0 {
						matched++
					}
				}
				if float64(matched)/float64(len(selectedTextTerms)) < 0.35 {
					continue
				}
			}
		}
		coverage := float64(c.Matched) / float64(queryTermCount)
		// Require a meaningful share of the query terms, unless the exact phrase
		// occurs. This stops a single shared character from flooding results.
		minCoverage := 0.5
		if queryTermCount > 12 {
			minCoverage = 0.35
		}
		if len(wantedMath) == 0 && coverage < minCoverage && !strings.Contains(block.Normalized, normalized) {
			continue
		}
		score := coverage*10 + math.Log1p(float64(c.TFSum))
		if len([]rune(normalized)) >= 4 && strings.Contains(block.Normalized, normalized) {
			score += 6
		}
		if len(wantedMath) > 0 {
			score += 12
		}
		score -= math.Min(2, float64(len([]rune(block.Text)))/2000)
		pack := packMap[material.PackID]
		fullHits = append(fullHits, QuestionHit{
			Question: block, Material: material, Pack: pack,
			Subjects: sortSubjects(model.DecodeStrings(pack.SubjectsJSON)), Score: score,
		})
	}
	sort.SliceStable(fullHits, func(i, j int) bool {
		if fullHits[i].Score != fullHits[j].Score {
			return fullHits[i].Score > fullHits[j].Score
		}
		if !fullHits[i].Question.CreatedAt.Equal(fullHits[j].Question.CreatedAt) {
			return fullHits[i].Question.CreatedAt.After(fullHits[j].Question.CreatedAt)
		}
		return fullHits[i].Question.ID < fullHits[j].Question.ID
	})
	fullHits = foldRepeatedQuestions(fullHits)
	total := len(fullHits)
	if offset > len(fullHits) {
		offset = len(fullHits)
	}
	end := offset + limit
	if end > len(fullHits) {
		end = len(fullHits)
	}
	page := fullHits[offset:end]
	var matcher *searchtext.SnippetMatcher
	if len(wantedMath) == 0 {
		matcher = searchtext.NewSnippetMatcher(normalized)
	}
	for i := range page {
		if len(wantedMath) > 0 {
			match, _ := searchtext.MatchingMathFragment(page[i].Question.Text, wantedMath)
			page[i].Snippet, page[i].Highlights = searchtext.HighlightMathSnippet(page[i].Question.Text, match, 180)
		} else {
			page[i].Snippet, page[i].Highlights = matcher.Highlight(page[i].Question.Text, 180)
		}
	}
	return SearchResult{Items: page, Total: total, TookMS: time.Since(started).Milliseconds()}, nil
}

func (s *Service) packsForMaterials(ctx context.Context, materials []model.Material) (map[string]model.Pack, error) {
	out := map[string]model.Pack{}
	ids := make([]string, 0, len(materials))
	for _, m := range materials {
		ids = append(ids, m.PackID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.Pack
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, p := range rows {
		out[p.ID] = p
	}
	return out, nil
}

// foldRepeatedQuestions keeps the best hit per normalized question text; the
// same question reprinted in other packs is listed under AlsoIn.
func foldRepeatedQuestions(hits []QuestionHit) []QuestionHit {
	out := make([]QuestionHit, 0, len(hits))
	index := map[string]int{}
	for _, h := range hits {
		key := h.Question.Normalized
		if len([]rune(key)) < 12 {
			out = append(out, h)
			continue
		}
		// Repeats inside one pack are distinct questions; only reprints in
		// other packs are folded.
		if i, ok := index[key]; ok && out[i].Pack.ID != h.Pack.ID {
			if len(out[i].AlsoIn) < 10 {
				out[i].AlsoIn = append(out[i].AlsoIn, h.Pack)
			}
			continue
		}
		index[key] = len(out)
		out = append(out, h)
	}
	return out
}

// GetQuestion returns one question block with material summary.
func (s *Service) GetQuestion(ctx context.Context, id string) (QuestionHit, error) {
	var block model.QuestionBlock
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&block).Error; err != nil {
		return QuestionHit{}, apperr.NotFound("题目不存在")
	}
	var material model.Material
	if err := s.db.WithContext(ctx).Where("id = ?", block.MaterialID).First(&material).Error; err != nil {
		return QuestionHit{}, apperr.NotFound("文件不存在")
	}
	var pack model.Pack
	_ = s.db.WithContext(ctx).Where("id = ?", material.PackID).First(&pack).Error
	// A detail request has no search query, so it has no matching highlight.
	snippet, highlights := searchtext.SnippetForMatch(block.Text, -1, -1, 400)
	return QuestionHit{
		Question: block, Material: material, Pack: pack,
		Subjects: sortSubjects(model.DecodeStrings(pack.SubjectsJSON)), Snippet: snippet, Highlights: highlights,
	}, nil
}

// ListMaterialQuestions returns one material's questions in document order.
func (s *Service) ListMaterialQuestions(ctx context.Context, materialID string, pg PageQuery) (Page[model.QuestionBlock], error) {
	db := s.db.WithContext(ctx).Model(&model.QuestionBlock{}).Where("material_id = ?", materialID)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[model.QuestionBlock]{}, apperr.Internal(err)
	}
	var rows []model.QuestionBlock
	if err := db.Order("seq ASC").Limit(pg.PageSize).Offset(pg.offset()).Find(&rows).Error; err != nil {
		return Page[model.QuestionBlock]{}, apperr.Internal(err)
	}
	return newPage(rows, total, pg), nil
}

func deleteQuestionIndexForMaterials(tx *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	questionIDs, err := lockQuestionIDs(tx, ids)
	if err != nil {
		return err
	}
	if len(questionIDs) > 0 {
		if err := tx.Where("question_id IN ?", questionIDs).Delete(&model.QuestionTerm{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("material_id IN ?", ids).Delete(&model.QuestionBlock{}).Error; err != nil {
		return err
	}
	return tx.Where("material_id IN ?", ids).Delete(&model.MaterialIndexState{}).Error
}

// deleteSearchDataForMaterials removes index rows during material deletion.
func deleteSearchDataForMaterials(tx *gorm.DB, materials []model.Material) error {
	ids := make([]string, 0, len(materials))
	for _, m := range materials {
		ids = append(ids, m.ID)
	}
	if err := deleteQuestionIndexForMaterials(tx, ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return tx.Where("material_id IN ?", ids).Delete(&model.MaterialText{}).Error
}

// EnqueueMissingIndexes removes stale indexes for ineligible roles and queues
// eligible materials archived while indexing was disabled. Safe on every startup.
func (s *Service) EnqueueMissingIndexes(ctx context.Context) (int, error) {
	settings, err := s.ensureSearchSettings(ctx)
	if err != nil {
		return 0, err
	}
	db := s.db.WithContext(ctx)
	eligibleRoles := indexParts
	if err := db.Transaction(func(tx *gorm.DB) error {
		var staleIDs []string
		if err := tx.Model(&model.Material{}).
			Where("part NOT IN ?", eligibleRoles).
			Where("id IN (?)", tx.Model(&model.MaterialIndexState{}).Select("material_id")).
			Pluck("id", &staleIDs).Error; err != nil {
			return err
		}
		if err := deleteQuestionIndexForMaterials(tx, staleIDs); err != nil {
			return err
		}
		return tx.Where(
			"kind = ? AND material_id IN (?)",
			model.JobIndex,
			tx.Model(&model.Material{}).Select("id").Where("part NOT IN ?", eligibleRoles),
		).Delete(&model.Job{}).Error
	}); err != nil {
		return 0, err
	}
	if !settings.Enabled {
		return 0, nil
	}
	var materialIDs []string
	if err := db.Model(&model.Material{}).
		Where("part IN ?", eligibleRoles).
		Where("status IN ? AND media_type <> ?", []string{model.StatusArchived, model.StatusMissing}, "application/zip").
		Where("id NOT IN (?)", db.Model(&model.MaterialIndexState{}).Select("material_id")).
		Pluck("id", &materialIDs).Error; err != nil {
		return 0, err
	}
	now := s.now()
	queued := 0
	for _, materialID := range materialIDs {
		created := false
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			created, err = enqueueIndexTx(tx, materialID, now)
			return err
		})
		if err != nil {
			return queued, err
		}
		if created {
			queued++
		}
	}
	if queued > 0 {
		s.notify()
	}
	return queued, nil
}

// autoEnqueueIndexTx queues indexing only for new exam-like materials.
func (s *Service) autoEnqueueIndexTx(tx *gorm.DB, materials []*model.Material, now time.Time) error {
	var settings model.SearchSettings
	if err := tx.Where("id = ?", 1).First(&settings).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = defaultSearchSettings()
		} else {
			return err
		}
	}
	if !settings.Enabled {
		return nil
	}
	for _, pm := range materials {
		if !analyzable(pm.MediaType) || !materialNeedsQuestionIndex(pm.Part) {
			continue
		}
		if _, err := enqueueIndexTx(tx, pm.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// syncMaterialIndexForPartTx keeps index state aligned with a part change or
// an automatically applied part suggestion.
func (s *Service) syncMaterialIndexForPartTx(tx *gorm.DB, materialID, part string, now time.Time) error {
	if !materialNeedsQuestionIndex(part) {
		if err := deleteQuestionIndexForMaterials(tx, []string{materialID}); err != nil {
			return err
		}
		return tx.Where("material_id = ? AND kind = ?", materialID, model.JobIndex).
			Delete(&model.Job{}).Error
	}
	var settings model.SearchSettings
	if err := tx.Where("id = ?", 1).First(&settings).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		settings = defaultSearchSettings()
	}
	if !settings.Enabled {
		return nil
	}
	var state model.MaterialIndexState
	err := tx.Where("material_id = ?", materialID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_, err = enqueueIndexTx(tx, materialID, now)
		return err
	}
	if err != nil || (state.Status != "failed" && state.Status != model.IndexStatusNotApplicable) {
		return err
	}
	jobID := newID()
	if err := tx.Model(&model.MaterialIndexState{}).Where("material_id = ?", materialID).
		Updates(map[string]any{
			"status": "pending", "job_id": jobID, "page_count": 0, "question_count": 0,
			"llm_calls": 0, "llm_failed_batches": 0, "error": "", "indexed_at": nil, "updated_at": now,
		}).Error; err != nil {
		return err
	}
	job := model.Job{ID: jobID, Kind: model.JobIndex, Status: model.JobPending, MaterialID: materialID, CreatedAt: now, UpdatedAt: now}
	return tx.Create(&job).Error
}

// enqueueIndexTx atomically creates at most one index state and its job. The
// material_id primary key coordinates both ingest and startup across instances.
func enqueueIndexTx(tx *gorm.DB, materialID string, now time.Time) (bool, error) {
	jobID := newID()
	state := model.MaterialIndexState{MaterialID: materialID, Status: "pending", JobID: jobID, UpdatedAt: now}
	inserted := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "material_id"}}, DoNothing: true}).Create(&state)
	if inserted.Error != nil {
		return false, inserted.Error
	}
	if inserted.RowsAffected == 0 {
		return false, nil
	}
	job := model.Job{ID: jobID, Kind: model.JobIndex, Status: model.JobPending, MaterialID: materialID, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&job).Error; err != nil {
		return false, err
	}
	return true, nil
}
