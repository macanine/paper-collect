package jobexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/catalogai"
)

const (
	// CardDocumentLimit is how much text the model reads per file.
	CardDocumentLimit          = 8_000
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
	in := catalogai.CardInput{FileName: m.FileName, MediaType: m.MediaType, Paths: m.Paths, Hints: m.Hints,
		Siblings: m.Siblings, PageCount: pageCount, MetadataOnly: metadataOnly}
	if !metadataOnly {
		in.Document = SampleAnalysisDocument(markdown, CardDocumentLimit)
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
	cardIn := in.Meta.CardInput(markdown, parsed.PageCount, metadataOnly)
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
