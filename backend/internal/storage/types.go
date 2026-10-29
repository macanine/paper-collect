// Package storage implements remote object stores without depending on HTTP routes or GORM.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrUnsupported = errors.New("operation unsupported by storage provider")

type Config struct {
	ID       string
	Kind     string
	Settings map[string]string
	Secrets  map[string]string

	// SaveRefreshToken persists a rotated OAuth refresh token without exposing it.
	SaveRefreshToken func(context.Context, string, string) error
}

type Object struct {
	Key      string // safe relative CAS key, e.g. ab/<sha256>/paper.pdf
	Name     string // readable remote filename, frozen before first upload
	Path     string // existing local source path
	SHA256   string
	Size     int64
	Progress func(done, total int64) // uploaded bytes, may be called concurrently
}

type Stored struct {
	RemoteID     string
	Size         int64
	Verification string // uploaded, size, sha256: never claim more than was checked
}

type Share struct {
	URL       string
	Password  string
	ExpiresAt *time.Time
	Status    string // ready, unsupported
}

type Capabilities struct {
	Upload   bool `json:"upload"`
	Download bool `json:"download"`
	Share    bool `json:"share"`
}

type Adapter interface {
	Put(context.Context, Object) (Stored, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Share(context.Context, string) (Share, error)
	Health(context.Context) error
	Capabilities() Capabilities
}
