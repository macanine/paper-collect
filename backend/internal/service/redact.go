package service

import (
	"errors"
	"regexp"
	"strings"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/archive"
	"github.com/nic/paper-collect/backend/internal/storage"
)

const maxStoredErrorRunes = 300

var (
	urlPattern    = regexp.MustCompile(`(?i)[a-z][a-z0-9+.\-]*://\S+`)
	keyValPattern = regexp.MustCompile(`(?i)(authorization|cookie|password|passwd|token|secret|api[_-]?key|client[_-]?secret)\s*[:=]\s*\S+`)
	bearerPattern = regexp.MustCompile(`(?i)bearer\s+\S+`)
)

// redact removes URLs, credential-looking pairs and known secret values, then truncates.
// It is applied to every message that is stored in job/replica errors or logs.
func redact(msg string, secrets ...string) string {
	out := msg
	for _, sec := range secrets {
		if len(sec) >= 4 {
			out = strings.ReplaceAll(out, sec, "***")
		}
	}
	out = urlPattern.ReplaceAllString(out, "<url>")
	out = bearerPattern.ReplaceAllString(out, "Bearer ***")
	out = keyValPattern.ReplaceAllStringFunc(out, func(m string) string {
		if i := strings.IndexAny(m, ":="); i >= 0 {
			return strings.TrimSpace(m[:i]) + "=***"
		}
		return "***"
	})
	runes := []rune(out)
	if len(runes) > maxStoredErrorRunes {
		out = string(runes[:maxStoredErrorRunes]) + "…"
	}
	return strings.TrimSpace(out)
}

// storageErrorText turns an adapter error into a stable, storable message.
// Adapter details are machine codes by contract; they are still redacted defensively.
func storageErrorText(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	ce := storage.AsCloudError(err)
	if ce.Class == storage.ClassUnknown && ce.Detail == "unclassified" {
		return "unknown: unclassified"
	}
	return redact(string(ce.Class)+": "+ce.Detail, secrets...)
}

// publicMessage converts a job failure into text that is safe to store and display.
func publicMessage(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	var rej *archive.Rejected
	if errors.As(err, &rej) {
		return rej.Reason
	}
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		message := redact(apiErr.Error())
		if message != "" {
			return message
		}
	}
	return "归档处理失败，请稍后重试"
}
