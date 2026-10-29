// Package service implements the application logic: credentials, catalogue,
// uploads, archive ingest, replication and the persisted job state machine.
// HTTP and MCP are thin adapters over this package.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nic/paper-collect/backend/internal/archive"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/office"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

// Options wires the service dependencies.
type Options struct {
	DB      *gorm.DB
	Blobs   *blob.Store
	Box     *cryptobox.Box
	Signer  *auth.Signer
	Limiter *auth.Limiter
	// Factory builds storage adapters. Production passes storage.New.
	Factory func(storage.Config) (storage.Adapter, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Limits bounds archive expansion; zero value means archive.DefaultLimits.
	Limits archive.Limits
	// Logf receives server-side diagnostics. Messages are redacted before logging.
	Logf func(format string, args ...any)
	// Worker controls task claiming, leases, and per-kind concurrency.
	Worker WorkerSettings
	// ResetAllRunningOnStart is enabled for SQLite, where only one service may
	// use the database. PostgreSQL recovers only expired leases.
	ResetAllRunningOnStart bool
}

// WorkerSettings controls the embedded job runners.
type WorkerSettings struct {
	InstanceID           string
	IngestConcurrency    int
	ReplicateConcurrency int
	AnalyzeConcurrency   int
	IndexConcurrency     int
	LeaseDuration        time.Duration
	HeartbeatInterval    time.Duration
	PollInterval         time.Duration
}

// previewConverter renders and caches Word previews. Tests can replace it
// without installing LibreOffice.
type previewConverter interface {
	PDF(context.Context, string, func(context.Context) (io.ReadCloser, error)) (string, error)
	Remove(string)
}

// Service is the application façade. It is safe for concurrent use.
type Service struct {
	db                  *gorm.DB
	blobs               *blob.Store
	office              previewConverter
	box                 *cryptobox.Box
	signer              *auth.Signer
	limiter             *auth.Limiter
	factory             func(storage.Config) (storage.Adapter, error)
	now                 func() time.Time
	limits              archive.Limits
	logf                func(format string, args ...any)
	wake                chan struct{}
	wakeMu              sync.Mutex
	worker              WorkerSettings
	resetRunningOnStart bool
}

// ErrBootstrapNeeded means the database has no user and no admin credentials were supplied.
var ErrBootstrapNeeded = errors.New("数据库中没有用户，需要设置 ADMIN_USERNAME 与 ADMIN_PASSWORD 进行首次自举")

// New validates options and returns a Service.
func New(opts Options) (*Service, error) {
	if opts.DB == nil || opts.Blobs == nil || opts.Box == nil || opts.Signer == nil || opts.Limiter == nil {
		return nil, errors.New("service: 缺少必要依赖")
	}
	if opts.Factory == nil {
		return nil, errors.New("service: 缺少存储适配器工厂")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	limits := opts.Limits
	if limits == (archive.Limits{}) {
		limits = archive.DefaultLimits
	}
	logSink := opts.Logf
	if logSink == nil {
		logSink = log.Printf
	}
	logf := func(format string, args ...any) { logSink("%s", redact(fmt.Sprintf(format, args...))) }
	worker := opts.Worker
	if strings.TrimSpace(worker.InstanceID) == "" {
		worker.InstanceID = uuid.NewString()
	}
	if worker.IngestConcurrency < 1 {
		worker.IngestConcurrency = 1
	}
	if worker.ReplicateConcurrency < 1 {
		worker.ReplicateConcurrency = 1
	}
	if worker.AnalyzeConcurrency < 1 {
		worker.AnalyzeConcurrency = 2
	}
	if worker.IndexConcurrency < 1 {
		worker.IndexConcurrency = 2
	}
	if worker.LeaseDuration <= 0 {
		worker.LeaseDuration = 2 * time.Minute
	}
	if worker.HeartbeatInterval <= 0 {
		worker.HeartbeatInterval = 20 * time.Second
	}
	if worker.HeartbeatInterval >= worker.LeaseDuration {
		return nil, errors.New("service: heartbeat interval must be shorter than lease duration")
	}
	if worker.PollInterval <= 0 {
		worker.PollInterval = 2 * time.Second
	}
	return &Service{
		db:                  opts.DB,
		blobs:               opts.Blobs,
		office:              office.New(opts.Blobs.DataDir()),
		box:                 opts.Box,
		signer:              opts.Signer,
		limiter:             opts.Limiter,
		factory:             opts.Factory,
		now:                 func() time.Time { return now().UTC() },
		limits:              limits,
		logf:                logf,
		wake:                make(chan struct{}),
		worker:              worker,
		resetRunningOnStart: opts.ResetAllRunningOnStart,
	}, nil
}

func (s *Service) notify() {
	s.wakeMu.Lock()
	close(s.wake)
	s.wake = make(chan struct{})
	s.wakeMu.Unlock()
}

func (s *Service) wakeSignal() <-chan struct{} {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.wake
}

func newID() string { return uuid.NewString() }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
