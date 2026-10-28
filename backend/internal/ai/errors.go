// Package ai provides OpenAI-compatible chat/model probing and the MinerU document parser.
package ai

import (
	"strings"
)

// APIError is a redacted upstream failure. Detail is safe to log and store.
type APIError struct {
	Class     string // config, auth, rate_limited, unavailable, invalid_response, timeout, unsupported
	Detail    string
	Retryable bool
}

func (e *APIError) Error() string { return e.Class + ": " + e.Detail }

// Sanitize trims an upstream message to a short single-line diagnostic.
func Sanitize(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	message = strings.Join(strings.Fields(message), " ")
	if len([]rune(message)) > 240 {
		r := []rune(message)
		message = string(r[:240]) + "…"
	}
	return message
}
