package jobexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/docx"
	"github.com/nic/paper-collect/backend/internal/office"
	"golang.org/x/sync/errgroup"
)

const (
	maxDOCXSource = 512 << 20
	parseTimeout  = 16 * time.Minute
	docxMediaType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	docMediaType  = "application/msword"
	pptMediaType  = "application/vnd.ms-powerpoint"
	pptxMediaType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	ocrKindRapid  = "rapidocr"
	ocrKindPaddle = "paddleocr"
	ocrKindMinerU = "mineru"
)

var officeConversionSlots = make(chan struct{}, 2)

// OCRConfig is a decrypted OCR provider.
type OCRConfig struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Token   string `json:"token"`
}

// Page is one parsed page with its 1-based page number.
type Page struct {
	PageNo int    `json:"page_no"`
	Text   string `json:"text"`
}

// ParsedText is reusable parser output. The master caches it per file.
type ParsedText struct {
	ParseKey  string `json:"parse_key"`
	Markdown  string `json:"markdown"`
	PageCount int    `json:"page_count"`
	Pages     []Page `json:"pages"`
}

// OCRClient is the common surface of the document parsers.
type OCRClient interface {
	Health(context.Context) error
	Extract(context.Context, string, []byte) (ai.Result, error)
}

// NewOCRClient builds the parser client for a provider.
func NewOCRClient(cfg OCRConfig) (OCRClient, error) {
	switch cfg.Kind {
	case ocrKindPaddle:
		client, err := ai.NewPaddleOCR(cfg.BaseURL, cfg.Token, cfg.Model, nil)
		if err != nil {
			return nil, err
		}
		client.Timeout = 15 * time.Minute
		return client, nil
	case ocrKindMinerU:
		client, err := ai.NewMinerU(cfg.BaseURL, cfg.Token, nil)
		if err != nil {
			return nil, err
		}
		client.Timeout = 15 * time.Minute
		return client, nil
	case ocrKindRapid:
		client, err := ai.NewRapidOCR(cfg.BaseURL, cfg.Model, nil)
		if err != nil {
			return nil, err
		}
		client.Timeout = 15 * time.Minute
		return client, nil
	}
	return nil, errors.New("不支持的 OCR 服务商类型")
}

// parseDocument chooses the cheapest reliable parser for a file. Word
// documents are read natively for RapidOCR; hosted providers keep receiving the
// original file because their layout models are part of the contract.
func parseDocument(ctx context.Context, env Env, cfg OCRConfig, b Blob) (ai.Result, error) {
	client, err := NewOCRClient(cfg)
	if err != nil {
		return ai.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, parseTimeout)
	defer cancel()
	if cfg.Kind == ocrKindRapid && b.MediaType == docxMediaType {
		return parseDOCX(ctx, env, client, b)
	}
	if isOfficeOCRMediaType(b.MediaType) {
		return parseOfficeForOCR(ctx, env, client, b)
	}
	original, err := env.OpenBlob(ctx, b)
	if err != nil {
		return ai.Result{}, err
	}
	defer original.Close()
	limit := int64(512 << 20)
	if cfg.Kind == ocrKindRapid {
		limit = 256 << 20
	}
	data, err := readAllLimit(original, limit)
	if err != nil {
		return ai.Result{}, fmt.Errorf("读取原件失败: %w", err)
	}
	if len(data) == 0 {
		return ai.Result{}, errors.New("原件为空")
	}
	return client.Extract(ctx, b.FileName, data)
}

func isOfficeOCRMediaType(mediaType string) bool {
	switch mediaType {
	case docMediaType, docxMediaType, pptMediaType, pptxMediaType:
		return true
	default:
		return false
	}
}

// parseOfficeForOCR renders formats the hosted parsers do not consistently
// accept into PDF. LibreOffice is already part of both full application
// images; a process-wide two-slot limit bounds its memory use on busy nodes.
func parseOfficeForOCR(ctx context.Context, env Env, client OCRClient, b Blob) (ai.Result, error) {
	select {
	case officeConversionSlots <- struct{}{}:
		defer func() { <-officeConversionSlots }()
	case <-ctx.Done():
		return ai.Result{}, ctx.Err()
	}
	env.Progress(ctx, "正在将 Office 文档转换为 PDF", 0, 0, nil)
	if err := os.MkdirAll(env.TempDir(), 0o700); err != nil {
		return ai.Result{}, apperr.Internal(err)
	}
	work, err := os.MkdirTemp(env.TempDir(), "office-ocr-")
	if err != nil {
		return ai.Result{}, apperr.Internal(err)
	}
	defer os.RemoveAll(work)
	converter := office.New(work)
	pdfPath, err := converter.Convert(ctx, b.SHA256, b.FileName, func(ctx context.Context) (io.ReadCloser, error) {
		return env.OpenBlob(ctx, b)
	})
	if err != nil {
		return ai.Result{}, err
	}
	pdf, err := os.Open(pdfPath)
	if err != nil {
		return ai.Result{}, apperr.Internal(err)
	}
	defer pdf.Close()
	data, err := readAllLimit(pdf, 512<<20)
	if err != nil {
		return ai.Result{}, fmt.Errorf("读取 Office 转换结果失败: %w", err)
	}
	return client.Extract(ctx, strings.TrimSuffix(b.FileName, filepath.Ext(b.FileName))+".pdf", data)
}

// parseDOCX extracts the Word body directly, then asks the OCR service only
// about embedded raster images. Text extraction therefore stays deterministic
// and does not require LibreOffice or a PDF text layer.
func parseDOCX(ctx context.Context, env Env, client OCRClient, b Blob) (ai.Result, error) {
	env.Progress(ctx, "正在读取 Word 文档并提取正文", 0, 0, nil)
	original, err := env.OpenBlob(ctx, b)
	if err != nil {
		return ai.Result{}, err
	}
	defer original.Close()
	reader, size, cleanup, err := DocumentReaderAt(env.TempDir(), original, b.Size)
	if err != nil {
		return ai.Result{}, err
	}
	defer cleanup()
	document, err := docx.Parse(reader, size)
	if err != nil {
		return ai.Result{}, err
	}
	if len(document.Pages) == 0 {
		return ai.Result{}, apperr.Unprocessable("Word 文档没有可解析的正文")
	}
	imageText := map[int]string{}
	if len(document.Images) > 0 {
		env.Progress(ctx, "正在识别 Word 文档中的图片", 0, 0, nil)
		imageText, err = ocrDocumentImages(ctx, env, client, document.Images)
		if err != nil {
			return ai.Result{}, err
		}
	}
	pages, markdown := document.Render(imageText)
	if len(pages) == 0 {
		return ai.Result{}, apperr.Unprocessable("Word 文档没有可解析的正文")
	}
	result := ai.Result{Markdown: markdown, PageCount: len(document.Pages), Pages: make([]ai.PageText, 0, len(pages))}
	for _, page := range pages {
		result.Pages = append(result.Pages, ai.PageText{PageNo: page.PageNo, Text: page.Text})
	}
	return result, nil
}

// DocumentReaderAt reuses a local file when possible and spills other streams
// to a private temporary file, so a large Word document is never held in
// memory as one buffer.
func DocumentReaderAt(tempDir string, body io.Reader, size int64) (io.ReaderAt, int64, func(), error) {
	if size > 0 {
		if reader, ok := body.(io.ReaderAt); ok {
			return reader, size, func() {}, nil
		}
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, 0, nil, apperr.Internal(err)
	}
	tmp, err := os.CreateTemp(tempDir, "docx-*.part")
	if err != nil {
		return nil, 0, nil, apperr.Internal(err)
	}
	name := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(name)
	}
	written, err := io.Copy(tmp, io.LimitReader(body, maxDOCXSource+1))
	if err != nil {
		cleanup()
		return nil, 0, nil, apperr.Unavailable("读取 Word 原件失败，请检查存储副本")
	}
	if written > maxDOCXSource {
		cleanup()
		return nil, 0, nil, apperr.TooLarge("Word 原件超过 512 MiB")
	}
	if written == 0 {
		cleanup()
		return nil, 0, nil, apperr.Unprocessable("Word 原件为空")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, apperr.Internal(err)
	}
	return tmp, written, cleanup, nil
}

func ocrDocumentImages(ctx context.Context, env Env, client OCRClient, images []docx.Image) (map[int]string, error) {
	texts := make(map[int]string, len(images))
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(2)
	for index := range images {
		index := index
		image := images[index]
		group.Go(func() error {
			result, err := client.Extract(groupCtx, fmt.Sprintf("image-%02d%s", index+1, imageExtension(image.MediaType)), image.Data)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				// An unreadable decoration must not discard the document text.
				env.Logf("Word 图片识别失败: %v", err)
				return nil
			}
			var parts []string
			for _, page := range result.Pages {
				if text := strings.TrimSpace(page.Text); text != "" {
					parts = append(parts, text)
				}
			}
			text := strings.TrimSpace(strings.Join(parts, "\n"))
			if text == "" {
				return nil
			}
			mu.Lock()
			texts[index] = text
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return texts, nil
}

func imageExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tif"
	case "image/webp":
		return ".webp"
	default:
		return ".img"
	}
}

func readAllLimit(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超过 %d MiB，暂不支持自动解析", limit>>20)
	}
	return data, nil
}

// parsedText keeps the non-empty pages of a parser result for caching.
func parsedText(key string, result ai.Result) *ParsedText {
	pages := make([]Page, 0, len(result.Pages))
	for _, page := range result.Pages {
		if strings.TrimSpace(page.Text) != "" {
			pages = append(pages, Page{PageNo: page.PageNo, Text: page.Text})
		}
	}
	return &ParsedText{ParseKey: key, Markdown: result.Markdown, PageCount: result.PageCount, Pages: pages}
}
