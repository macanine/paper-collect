package jobexec

import (
	"context"
	"errors"
	"net"

	"github.com/nic/paper-collect/backend/internal/ai"
)

// Failure classes decide what happens to a failed AI job:
//   - ClassConfig: no usable model/OCR (missing, disabled, rejected key). The
//     file waits for AI and the job resumes once the configuration changes.
//   - ClassTransient: rate limits, timeouts, unavailable services, lost
//     nodes. The job is retried with backoff.
//   - ClassPermanent: retrying the same input will not help.
const (
	ClassConfig    = "config"
	ClassTransient = "transient"
	ClassPermanent = "permanent"
)

// ClassError attaches a failure class to an error.
type ClassError struct {
	Class string
	Err   error
}

func (e *ClassError) Error() string { return e.Err.Error() }
func (e *ClassError) Unwrap() error { return e.Err }

func classed(class string, err error) error { return &ClassError{Class: class, Err: err} }

// Classify returns the failure class of an execution error.
func Classify(err error) string {
	if err == nil {
		return ""
	}
	var ce *ClassError
	if errors.As(err, &ce) {
		return ce.Class
	}
	var ae *ai.APIError
	if errors.As(err, &ae) {
		switch ae.Class {
		case "config", "auth":
			return ClassConfig
		case "rate_limited", "unavailable", "timeout":
			return ClassTransient
		}
		if ae.Retryable {
			return ClassTransient
		}
		return ClassPermanent
	}
	if errors.Is(err, ErrBlobUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		return ClassTransient
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ClassTransient
	}
	return ClassPermanent
}

// FailureClass is Classify for a node reporting a failure to the master.
func FailureClass(err error) string { return Classify(err) }
