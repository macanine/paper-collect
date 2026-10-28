package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/model"
)

func textParseKey(provider model.OCRProvider) string {
	value := "ocr-text-v2:" + strings.TrimSpace(provider.Kind) + ":" +
		strings.TrimSpace(provider.BaseURL) + ":" + strings.TrimSpace(provider.Model)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parsedPages(result ai.Result) []pageText {
	pages := make([]pageText, 0, len(result.Pages))
	for _, page := range result.Pages {
		if strings.TrimSpace(page.Text) != "" {
			pages = append(pages, pageText{PageNo: page.PageNo, Text: page.Text})
		}
	}
	return pages
}
