package jobexec

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/catalogai"
)

const (
	// CardDocumentLimit is how much text the model reads per file.
	CardDocumentLimit          = 16_000
	AnalysisStoreMarkdownLimit = 2 << 20
)

// ModelConfig is a decrypted OpenAI-compatible endpoint.
type ModelConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`
	VisionModel string `json:"vision_model"`
}

// CardMeta is the non-content evidence for reading one file.
type CardMeta struct {
	FileName  string   `json:"file_name"`
	MediaType string   `json:"media_type"`
	Paths     []string `json:"paths,omitempty"`
	Hints     string   `json:"hints,omitempty"`
	Siblings  []string `json:"siblings,omitempty"`
}

// CardInput rebuilds the checked input from the metadata and the parsed text.
func (m CardMeta) CardInput(markdown string, pageCount int, metadataOnly bool) catalogai.CardInput {
	return m.CardInputFromDocument(SampleAnalysisDocument(markdown, CardDocumentLimit), pageCount, metadataOnly)
}

// CardInputFromDocument builds the input from the exact document text the
// model was shown, so the master checks quotes against what the model saw.
func (m CardMeta) CardInputFromDocument(document string, pageCount int, metadataOnly bool) catalogai.CardInput {
	in := catalogai.CardInput{FileName: m.FileName, MediaType: m.MediaType, Paths: m.Paths, Hints: m.Hints,
		Siblings: m.Siblings, PageCount: pageCount, MetadataOnly: metadataOnly}
	if !metadataOnly {
		in.Document = document
	}
	return in
}

// AnalyzeInput describes the READ step for one file.
type AnalyzeInput struct {
	Blob      Blob          `json:"blob"`
	Readable  bool          `json:"readable"`      // the media type can be parsed at all
	OCR       *OCRConfig    `json:"ocr,omitempty"` // nil when Cached is used or the file is not readable
	ParseKey  string        `json:"parse_key"`
	Cached    *ParsedText   `json:"cached,omitempty"`
	Meta      CardMeta      `json:"meta"`
	Models    []ModelConfig `json:"models"`
	MaxPages  int           `json:"max_pages"`
	MaxImages int           `json:"max_images"`
}

// AnalyzeResult is the raw card reply plus the text it was based on; the
// master runs the same checks again before storing the card.
type AnalyzeResult struct {
	ProviderID   string      `json:"provider_id"`
	Model        string      `json:"model"`
	Content      string      `json:"content"`
	Markdown     string      `json:"markdown"`
	Document     string      `json:"document,omitempty"` // the page-marked text the model read
	PageCount    int         `json:"page_count"`
	MetadataOnly bool        `json:"metadata_only"`
	Parsed       *ParsedText `json:"parsed,omitempty"` // fresh parser output to cache
}

func runAnalyze(ctx context.Context, env Env, in AnalyzeInput) (*AnalyzeResult, error) {
	out := &AnalyzeResult{}
	var parsed ai.Result
	metadataOnly := !in.Readable
	if in.Readable {
		switch {
		case in.Cached != nil && strings.TrimSpace(in.Cached.Markdown) != "":
			parsed.Markdown, parsed.PageCount = in.Cached.Markdown, in.Cached.PageCount
			for _, page := range in.Cached.Pages {
				parsed.Pages = append(parsed.Pages, ai.PageText{PageNo: page.PageNo, Text: page.Text})
			}
		case in.OCR == nil:
			return nil, classed(ClassConfig, errors.New("没有可用的 OCR 服务商，请先在 AI 解析中配置并选择"))
		default:
			env.Progress(ctx, "正在读取原件并调用文档解析服务", 0, 0, nil)
			started := time.Now()
			result, err := parseDocument(ctx, env, *in.OCR, in.Blob)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if class := Classify(err); class != ClassPermanent {
					return nil, classed(class, err)
				}
				// The content itself cannot be parsed (damaged, encrypted, empty):
				// the model still decides, from the name and path only.
				env.Logf("parse failed, reading by name only: %s", ai.Sanitize(err.Error()))
				metadataOnly = true
			} else {
				parsed = result
				env.Progress(ctx, "文档解析完成", 0, 0, map[string]any{
					"page_count": parsed.PageCount, "duration_ms": time.Since(started).Milliseconds(), "ocr_provider": in.OCR.Name,
				})
				out.Parsed = parsedText(in.ParseKey, parsed)
			}
		}
	}
	markdown := parsed.Markdown
	if len(markdown) > AnalysisStoreMarkdownLimit {
		markdown = markdown[:AnalysisStoreMarkdownLimit]
	}
	// Images only travel to a vision model in memory; they are never persisted.
	images := capImages(parsed.Images, in.MaxPages, in.MaxImages)
	if strings.TrimSpace(markdown) == "" && len(images) == 0 {
		metadataOnly = true
	}
	out.Markdown, out.PageCount, out.MetadataOnly = markdown, parsed.PageCount, metadataOnly
	document := BuildCardDocument(parsed.Pages, markdown, CardDocumentLimit)
	cardIn := in.Meta.CardInputFromDocument(document, parsed.PageCount, metadataOnly)
	if !metadataOnly {
		out.Document = document
	}
	if metadataOnly {
		images = nil
	}
	env.Progress(ctx, fmt.Sprintf("正在由模型阅读文件（%d 页）", parsed.PageCount), 0, 0, nil)
	outcome, err := chatChecked(ctx, env, in.Models, "阅读文件", func(vision bool) []ai.ChatMessage {
		if vision {
			return catalogai.CardMessages(cardIn, images)
		}
		return catalogai.CardMessages(cardIn, nil)
	}, len(images) > 0, func(content string) ([]catalogai.Problem, error) {
		_, problems, err := catalogai.CheckCard(content, cardIn)
		return problems, err
	})
	if err != nil {
		return nil, err
	}
	out.ProviderID, out.Model, out.Content = outcome.ProviderID, outcome.Model, outcome.Content
	return out, nil
}

// SampleAnalysisDocument keeps both ends of a long document. Answers are often
// placed at the end, so sending only the beginning can misclassify a combined
// paper as an exam. The marker makes the omission explicit to the model.
func SampleAnalysisDocument(document string, limit int) string {
	text := strings.TrimSpace(document)
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	headCount := limit * 2 / 3
	tailCount := limit - headCount
	if headCount < 1 || tailCount < 1 {
		return string(runes[:limit])
	}
	omitted := len(runes) - headCount - tailCount
	return string(runes[:headCount]) +
		fmt.Sprintf("\n\n[...正文中间省略 %d 字，以下为末尾内容...]\n\n", omitted) +
		string(runes[len(runes)-tailCount:])
}

// BuildCardDocument renders the parsed pages with explicit [第 N 页] markers so
// the model can tell where each part of the file sits, for example where a
// combined paper's answers begin. A long document keeps the start of every
// page, so each page's heading is visible; with too many pages for that it
// keeps both ends and says which pages were left out. Without page structure
// it falls back to a plain head-and-tail sample.
func BuildCardDocument(pages []ai.PageText, markdown string, limit int) string {
	var kept []ai.PageText
	for _, page := range pages {
		if strings.TrimSpace(page.Text) != "" {
			page.Text = strings.TrimSpace(page.Text)
			kept = append(kept, page)
		}
	}
	if len(kept) == 0 {
		return SampleAnalysisDocument(markdown, limit)
	}
	render := func(page ai.PageText, text string) string {
		return fmt.Sprintf("[第 %d 页]\n%s", page.PageNo, text)
	}
	total := 0
	for _, page := range kept {
		total += utf8.RuneCountInString(page.Text) + 12
	}
	if limit <= 0 || total <= limit {
		parts := make([]string, 0, len(kept))
		for _, page := range kept {
			parts = append(parts, render(page, page.Text))
		}
		return strings.Join(parts, "\n\n")
	}
	const minPerPage = 150
	if limit/len(kept) >= minPerPage {
		// Water-filling: short pages stay whole and lend their spare room to long ones.
		order := make([]int, len(kept))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool {
			return utf8.RuneCountInString(kept[order[a]].Text) < utf8.RuneCountInString(kept[order[b]].Text)
		})
		allowance := make([]int, len(kept))
		budget := limit - 12*len(kept)
		for rank, i := range order {
			share := budget / (len(order) - rank)
			size := utf8.RuneCountInString(kept[i].Text)
			allowance[i] = min(size, share)
			budget -= allowance[i]
		}
		parts := make([]string, 0, len(kept))
		for i, page := range kept {
			text := page.Text
			if runes := []rune(text); len(runes) > allowance[i] {
				text = string(runes[:allowance[i]]) + "…"
			}
			parts = append(parts, render(page, text))
		}
		return strings.Join(parts, "\n\n")
	}
	headBudget, tailBudget := limit*2/3, limit-limit*2/3
	head, tail := 0, len(kept)
	for head < len(kept) {
		size := utf8.RuneCountInString(kept[head].Text) + 12
		if size > headBudget {
			break
		}
		headBudget -= size
		head++
	}
	for tail > head {
		size := utf8.RuneCountInString(kept[tail-1].Text) + 12
		if size > tailBudget {
			break
		}
		tailBudget -= size
		tail--
	}
	var parts []string
	for _, page := range kept[:head] {
		parts = append(parts, render(page, page.Text))
	}
	if tail > head {
		parts = append(parts, fmt.Sprintf("[...省略第 %d–%d 页的正文，共 %d 页...]", kept[head].PageNo, kept[tail-1].PageNo, tail-head))
	}
	for _, page := range kept[tail:] {
		parts = append(parts, render(page, page.Text))
	}
	return strings.Join(parts, "\n\n")
}

// capImages bounds visual input by both the page and image limits.
func capImages(images [][]byte, maxPages, maxImages int) [][]byte {
	limit := len(images)
	if maxPages > 0 && maxPages < limit {
		limit = maxPages
	}
	if maxImages >= 0 && maxImages < limit {
		limit = maxImages
	}
	if limit < 0 {
		limit = 0
	}
	return images[:limit]
}
