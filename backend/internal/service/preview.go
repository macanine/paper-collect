package service

import (
	"context"
	"io"
	"os"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
)

// OpenMaterialPreview opens a PDF without changing the original material.
func (s *Service) OpenMaterialPreview(ctx context.Context, id string) (Download, error) {
	var material model.Material
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&material).Error; err != nil {
		return Download{}, apperr.NotFound("资料不存在")
	}
	switch material.MediaType {
	case blob.MediaTypes[blob.KindPDF]:
		return s.OpenMaterialDownload(ctx, id)
	case blob.MediaTypes[blob.KindDOCX]:
		pdf, err := s.office.PDF(ctx, material.SHA256, func(ctx context.Context) (io.ReadCloser, error) {
			original, err := s.OpenMaterialDownload(ctx, id)
			return original.Reader, err
		})
		if err != nil {
			return Download{}, err
		}
		f, err := os.Open(pdf)
		if err != nil {
			return Download{}, apperr.Internal(err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return Download{}, apperr.Internal(err)
		}
		return Download{Reader: f, MediaType: "application/pdf", FileName: "preview.pdf", Size: info.Size()}, nil
	default:
		return Download{}, apperr.UnsupportedMedia("该文件类型暂不支持在线预览，请下载原件查看")
	}
}
