package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/docx"
	"github.com/nic/paper-collect/backend/internal/model"
	"golang.org/x/sync/errgroup"
)

const maxDOCXSource = 512 << 20

// parseMaterial chooses the cheapest reliable parser for a material. Word
// documents are read natively for RapidOCR; hosted providers keep receiving the
// original file because their layout models are part of the contract.
func (s *Service) parseMaterial(ctx context.Context, client ocrClient, provider model.OCRProvider, material model.Material) (ai.Result, error) {
	if provider.Kind == model.OCRKindRapidOCR && material.MediaType == blob.MediaTypes[blob.KindDOCX] {
		return s.parseDOCX(ctx, client, material)
	}
	return s.parseOriginal(ctx, client, provider, material)
}

func (s *Service) parseOriginal(ctx context.Context, client ocrClient, provider model.OCRProvider, material model.Material) (ai.Result, error) {
	original, err := s.OpenMaterialDownload(ctx, material.ID)
	if err != nil {
		return ai.Result{}, err
	}
	defer original.Reader.Close()
	limit := int64(512 << 20)
	if provider.Kind == model.OCRKindRapidOCR {
		limit = 256 << 20
	}
	data, err := ioReadAllLimit(original.Reader, limit)
	if err != nil {
		return ai.Result{}, fmt.Errorf("读取原件失败: %w", err)
	}
	if len(data) == 0 {
		return ai.Result{}, errors.New("原件为空")
	}
	return client.Extract(ctx, material.FileName, data)
}

// parseDOCX extracts the Word body directly, then asks the OCR service only
// about embedded raster images. Text extraction therefore stays deterministic
// and does not require LibreOffice or a PDF text layer.
func (s *Service) parseDOCX(ctx context.Context, client ocrClient, material model.Material) (ai.Result, error) {
	s.reportProgress(ctx, "正在读取 Word 文档并提取正文")
	document, err := s.openDOCX(ctx, material)
	if err != nil {
		return ai.Result{}, err
	}
	if len(document.Pages) == 0 {
		return ai.Result{}, apperr.Unprocessable("Word 文档没有可解析的正文")
	}
	imageText := map[int]string{}
	if len(document.Images) > 0 {
		s.reportProgress(ctx, "正在识别 Word 文档中的图片")
		imageText, err = s.ocrDocumentImages(ctx, client, document.Images)
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

func (s *Service) openDOCX(ctx context.Context, material model.Material) (*docx.Document, error) {
	original, err := s.OpenMaterialDownload(ctx, material.ID)
	if err != nil {
		return nil, err
	}
	defer original.Reader.Close()
	reader, size, cleanup, err := s.documentReaderAt(original)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return docx.Parse(reader, size)
}

// documentReaderAt reuses a local file when possible and spills remote
// replicas to a private temporary file, so a large Word document is never held
// in memory as one buffer.
func (s *Service) documentReaderAt(download Download) (io.ReaderAt, int64, func(), error) {
	if download.Size > 0 {
		if reader, ok := download.Reader.(io.ReaderAt); ok {
			return reader, download.Size, func() {}, nil
		}
	}
	if seeker, ok := download.Reader.(io.Seeker); ok {
		if size, err := seeker.Seek(0, io.SeekEnd); err == nil && size > 0 {
			if _, err := seeker.Seek(0, io.SeekStart); err == nil {
				if reader, ok := download.Reader.(io.ReaderAt); ok {
					return reader, size, func() {}, nil
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(s.blobs.DataDir(), "tmp"), 0o700); err != nil {
		return nil, 0, nil, apperr.Internal(err)
	}
	tmp, err := os.CreateTemp(filepath.Join(s.blobs.DataDir(), "tmp"), "docx-*.part")
	if err != nil {
		return nil, 0, nil, apperr.Internal(err)
	}
	name := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(name)
	}
	size, err := io.Copy(tmp, io.LimitReader(download.Reader, maxDOCXSource+1))
	if err != nil {
		cleanup()
		return nil, 0, nil, apperr.Unavailable("读取 Word 原件失败，请检查存储副本")
	}
	if size > maxDOCXSource {
		cleanup()
		return nil, 0, nil, apperr.TooLarge("Word 原件超过 512 MiB")
	}
	if size == 0 {
		cleanup()
		return nil, 0, nil, apperr.Unprocessable("Word 原件为空")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, apperr.Internal(err)
	}
	return tmp, size, cleanup, nil
}

func (s *Service) ocrDocumentImages(ctx context.Context, client ocrClient, images []docx.Image) (map[int]string, error) {
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
				s.logf("Word 图片识别失败: %v", err)
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

func (s *Service) reportProgress(ctx context.Context, message string) {
	if job, ok := ctx.Value(progressJobKey{}).(model.Job); ok {
		s.jobProgress(ctx, job, message, 0, 0)
	}
}
