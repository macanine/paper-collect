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
	Key      string // safe relative path under the drive root, e.g. 数学/2025届/试题.pdf
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
	Folders  bool `json:"folders"` // keys may contain sub-directories
	Move     bool `json:"move"`    // the adapter implements Mover
	Delete   bool `json:"delete"`  // the adapter implements Deleter
}

// Put never overwrites: when the key already holds the same content (same
// size, and the same SHA-256 where the adapter can check it) the object is
// adopted, otherwise Put fails with ClassConflict.
type Adapter interface {
	Put(context.Context, Object) (Stored, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Share(context.Context, string) (Share, error)
	Health(context.Context) error
	Capabilities() Capabilities
}

// Mover moves or renames a stored object to another key under the same root
// without overwriting. The returned RemoteID replaces the old one.
type Mover interface {
	Move(ctx context.Context, remoteID, key string) (Stored, error)
}

// Deleter removes a stored object. A missing object is not an error.
type Deleter interface {
	Delete(ctx context.Context, remoteID string) error
}
