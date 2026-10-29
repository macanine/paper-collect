// Package office creates bounded, reusable PDF previews of Word documents.
// Conversion is only used for previews; document analysis reads DOCX directly.
package office

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/ooxml"
)

const (
	maxOriginal     = 512 << 20
	conversionLimit = 2 * time.Minute
)

// Converter renders previews with one LibreOffice process per application.
type Converter struct {
	dataDir string
	slots   chan struct{}
}

// New returns a converter rooted at dataDir.
func New(dataDir string) *Converter {
	return &Converter{dataDir: dataDir, slots: make(chan struct{}, 1)}
}

// PDF returns the path to a cached PDF, creating it from the original if
// needed. The caller must authorize access to the material before calling.
func (c *Converter) PDF(ctx context.Context, sha string, open func(context.Context) (io.ReadCloser, error)) (string, error) {
	if !validHash(sha) {
		return "", apperr.Unprocessable("资料的文件校验信息无效")
	}
	dest := c.cachePath(sha)
	if regular(dest) {
		return dest, nil
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if regular(dest) {
		return dest, nil
	}
	if _, err := findSOffice(); err != nil {
		return "", apperr.Unavailable("DOCX 预览需要安装 LibreOffice Writer")
	}
	if err := os.MkdirAll(filepath.Join(c.dataDir, "tmp"), 0o700); err != nil {
		return "", apperr.Internal(err)
	}
	work, err := os.MkdirTemp(filepath.Join(c.dataDir, "tmp"), "docx-")
	if err != nil {
		return "", apperr.Internal(err)
	}
	defer os.RemoveAll(work)

	source := filepath.Join(work, "document.docx")
	if err := writeOriginal(ctx, open, source, sha); err != nil {
		return "", err
	}
	if err := ooxml.Validate(source); err != nil {
		return "", err
	}
	pdf, err := c.convert(ctx, work, source)
	if err != nil {
		return "", err
	}
	if err := validatePDF(pdf); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", apperr.Internal(err)
	}
	if err := os.Rename(pdf, dest); err != nil {
		return "", apperr.Internal(err)
	}
	return dest, nil
}

// Remove discards the derived preview when the final reference to a blob is
// deleted.
func (c *Converter) Remove(sha string) {
	if validHash(sha) {
		_ = os.Remove(c.cachePath(sha))
	}
}

func (c *Converter) cachePath(sha string) string {
	return filepath.Join(c.dataDir, "previews", "docx-v1", sha[:2], sha+".pdf")
}

func writeOriginal(ctx context.Context, open func(context.Context) (io.ReadCloser, error), name, want string) error {
	input, err := open(ctx)
	if err != nil {
		return err
	}
	defer input.Close()
	out, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return apperr.Internal(err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(input, maxOriginal+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		return apperr.Unavailable("读取 DOCX 原件失败，请检查存储副本")
	}
	if n > maxOriginal {
		return apperr.TooLarge("DOCX 原件超过 512 MiB")
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		return apperr.Unprocessable("DOCX 原件校验失败，请检查存储副本")
	}
	return nil
}

func validatePDF(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return apperr.Unprocessable("DOCX 无法转换为预览，请检查文档是否损坏或加密")
	}
	var header [5]byte
	_, readErr := io.ReadFull(f, header[:])
	info, statErr := f.Stat()
	f.Close()
	if readErr != nil || statErr != nil || string(header[:]) != "%PDF-" || info.Size() > maxOriginal {
		return apperr.Unprocessable("DOCX 转换结果无效或超过 512 MiB")
	}
	return nil
}

func validHash(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	_, err := hex.DecodeString(sha)
	return err == nil && sha == strings.ToLower(sha)
}

func regular(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular() && info.Size() > 5
}
