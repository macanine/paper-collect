package jobexec

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
	apiKeyPattern = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{43}|pc_[0-9a-fA-F-]{36}\.[A-Za-z0-9_-]{16,128}|nd-[A-Za-z0-9_-]{43})`)
)

// Redact removes URLs, credential-looking pairs and known secret values, then truncates.
// It is applied to every message that is stored in job/replica errors or logs.
func Redact(msg string, secrets ...string) string {
	out := msg
	for _, sec := range secrets {
		if len(sec) >= 4 {
			out = strings.ReplaceAll(out, sec, "***")
		}
	}
	out = urlPattern.ReplaceAllString(out, "<url>")
	out = bearerPattern.ReplaceAllString(out, "Bearer ***")
	out = apiKeyPattern.ReplaceAllString(out, "<api-key>")
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

// StorageErrorText turns an adapter error into a stable, storable message.
// Adapter details are machine codes by contract; they are still redacted defensively.
func StorageErrorText(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	ce := storage.AsCloudError(err)
	if ce.Class == storage.ClassUnknown && ce.Detail == "unclassified" {
		return "unknown: unclassified"
	}
	return Redact(string(ce.Class)+": "+ce.Detail, secrets...)
}

// PublicMessage converts a job failure into text that is safe to store and display.
func PublicMessage(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return Redact(ae.Message)
	}
	var rej *archive.Rejected
	if errors.As(err, &rej) {
		return Redact(rej.Reason)
	}
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		message := Redact(apiErr.Error())
		if message != "" {
			return message
		}
	}
	return "归档处理失败，请稍后重试"
}

// IndexErrorText keeps the upstream class of an index failure readable.
func IndexErrorText(err error) string {
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		return ai.Sanitize(apiErr.Error())
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ai.Sanitize(ae.Message)
	}
	message := ai.Sanitize(err.Error())
	if message == "" {
		return "索引失败"
	}
	return message
}

// IndexError carries the model call counters of a failed segmentation.
type IndexError struct {
	Err           error
	Calls, Failed int
}

func (e *IndexError) Error() string { return e.Err.Error() }
func (e *IndexError) Unwrap() error { return e.Err }

// Failure converts an execution error into the stored message and the counters
// that belong on the job's projection. It runs on the node that saw the error,
// because error types do not survive the trip to the master.
func Failure(kind string, err error) (string, map[string]int) {
	switch kind {
	case KindReplicate:
		return StorageErrorText(err), nil
	case KindAnalyze:
		return PublicMessage(err), nil
	case KindIndex:
		var ie *IndexError
		if errors.As(err, &ie) {
			return Redact(IndexErrorText(err)), map[string]int{"llm_calls": ie.Calls, "llm_failed_batches": ie.Failed}
		}
		return Redact(IndexErrorText(err)), nil
	}
	return Redact(err.Error()), nil
}
