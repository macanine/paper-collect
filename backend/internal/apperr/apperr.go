// Package apperr defines the errors that may be exposed to HTTP and MCP clients.
// Message is always a short, redacted, user-readable explanation. Cause is only
// for server-side logs and must never be serialized to a client.
package apperr

import (
	"errors"
	"net/http"
)

// Error is a domain error carrying an HTTP status and a stable machine code.
type Error struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Unwrap exposes Cause for errors.Is/As in server logs.
func (e *Error) Unwrap() error { return e.Cause }

// New builds an Error with the given status, code and message.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Validation is a 400 for semantically invalid field values.
func Validation(message string) *Error {
	return New(http.StatusBadRequest, "validation_error", message)
}

// BadRequest is a 400 for malformed request framing such as broken JSON.
func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "bad_request", message)
}

// Unauthorized is a 401; the message must not reveal whether a credential exists.
func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, "unauthorized", message)
}

// Forbidden is a 403 for authenticated callers without the required scope or ownership.
func Forbidden(message string) *Error {
	return New(http.StatusForbidden, "forbidden", message)
}

// NotFound is a 404.
func NotFound(message string) *Error {
	return New(http.StatusNotFound, "not_found", message)
}

// Conflict is a 409 for state conflicts and duplicate paths.
func Conflict(message string) *Error {
	return New(http.StatusConflict, "conflict", message)
}

// TooLarge is a 413 for payloads over a declared or global limit.
func TooLarge(message string) *Error {
	return New(http.StatusRequestEntityTooLarge, "payload_too_large", message)
}

// UnsupportedMedia is a 415 for unsupported types or mismatched file signatures.
func UnsupportedMedia(message string) *Error {
	return New(http.StatusUnsupportedMediaType, "unsupported_media_type", message)
}

// Unprocessable is a 422 for byte-count or SHA mismatches.
func Unprocessable(message string) *Error {
	return New(http.StatusUnprocessableEntity, "unprocessable_entity", message)
}

// RateLimited is a 429 for login throttling.
func RateLimited(message string) *Error {
	return New(http.StatusTooManyRequests, "rate_limited", message)
}

// Unavailable is a 503 when no usable backend exists.
func Unavailable(message string) *Error {
	return New(http.StatusServiceUnavailable, "service_unavailable", message)
}

// Internal wraps an unexpected error. The cause is kept for logs only.
func Internal(cause error) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: "internal_error", Message: "服务器内部错误", Cause: cause}
}

// As converts any error into an *Error. Unknown errors become internal errors.
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}
