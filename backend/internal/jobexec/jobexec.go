// Package jobexec runs the database-free middle of replicate, analyze (READ),
// index, group, decide and entity jobs. The master prepares a self-contained Input, any node executes it through
// an Env, and the master commits the Result in one transaction. The same code
// runs locally on the master and remotely on worker nodes.
package jobexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nic/paper-collect/backend/internal/storage"
)

// Job kinds a node may execute.
const (
	KindReplicate = "replicate"
	KindAnalyze   = "analyze"
	KindIndex     = "index"
	// Local-only kinds: they need fresh library candidates, so the master
	// prepares and runs them itself.
	KindGroup  = "group"
	KindDecide = "decide"
	KindEntity = "entity"
)

// Protocol is the master/worker wire version. A worker with another version may
// heartbeat, so it stays visible, but cannot claim jobs.
const Protocol = 4

// RemoteKinds lists the kinds that can leave the master.
var RemoteKinds = []string{KindReplicate, KindAnalyze, KindIndex}

// IsRemoteKind reports whether a job kind can run on a worker node.
func IsRemoteKind(kind string) bool {
	for _, k := range RemoteKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Input is the self-contained description of one job execution. It carries
// decrypted credentials, so it is only sent over TLS and never persisted.
type Input struct {
	JobID     string          `json:"job_id"`
	Kind      string          `json:"kind"`
	StartedAt time.Time       `json:"started_at"`
	Replicate *ReplicateInput `json:"replicate,omitempty"`
	Analyze   *AnalyzeInput   `json:"analyze,omitempty"`
	Index     *IndexInput     `json:"index,omitempty"`
	Group     *GroupInput     `json:"-"`
	Decide    *DecideInput    `json:"-"`
	Entity    *EntityInput    `json:"-"`
}

// HasResult reports whether a result carries the output of the given kind.
func (r Result) HasResult(kind string) bool {
	switch kind {
	case KindReplicate:
		return r.Replicate != nil
	case KindAnalyze:
		return r.Analyze != nil
	case KindIndex:
		return r.Index != nil
	case KindGroup:
		return r.Group != nil
	case KindDecide:
		return r.Decide != nil
	case KindEntity:
		return r.Entity != nil
	}
	return false
}

// Result is what an execution produced. Large text is carried inline; the
// master caps it before storing.
type Result struct {
	Replicate *ReplicateResult `json:"replicate,omitempty"`
	Analyze   *AnalyzeResult   `json:"analyze,omitempty"`
	Index     *IndexResult     `json:"index,omitempty"`
	Group     *GroupResult     `json:"-"`
	Decide    *DecideResult    `json:"-"`
	Entity    *EntityResult    `json:"-"`
}

// Blob identifies original content by hash and size.
type Blob struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
}

// DriveConfig is a decrypted storage adapter configuration.
type DriveConfig struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Settings map[string]string `json:"settings"`
	Secrets  map[string]string `json:"secrets"`
}

// Env is everything an execution may touch outside its own memory.
type Env interface {
	// OpenBlob returns verified original content.
	OpenBlob(ctx context.Context, b Blob) (io.ReadCloser, error)
	// Progress records a fixed, application-generated message. Never pass secrets or URLs.
	Progress(ctx context.Context, message string, current, total int, metrics map[string]any)
	// SaveRefreshToken persists a rotated OAuth refresh token for a drive.
	SaveRefreshToken(ctx context.Context, driveID, previous, next string) error
	// Adapter builds a storage adapter. Tests replace the factory.
	Adapter(cfg storage.Config) (storage.Adapter, error)
	// TempDir is where large originals are spilled while being parsed.
	TempDir() string
	// Logf receives redacted diagnostics.
	Logf(format string, args ...any)
}

// ErrBlobUnavailable means no source could provide verified content.
var ErrBlobUnavailable = errors.New("原件不可用")

// Run executes one prepared job.
func Run(ctx context.Context, env Env, in Input) (Result, error) {
	switch in.Kind {
	case KindReplicate:
		if in.Replicate == nil {
			return Result{}, errors.New("复制任务缺少输入")
		}
		out, err := runReplicate(ctx, env, *in.Replicate)
		return Result{Replicate: out}, err
	case KindAnalyze:
		if in.Analyze == nil {
			return Result{}, errors.New("分析任务缺少输入")
		}
		out, err := runAnalyze(ctx, env, *in.Analyze)
		return Result{Analyze: out}, err
	case KindIndex:
		if in.Index == nil {
			return Result{}, errors.New("索引任务缺少输入")
		}
		out, err := runIndex(ctx, env, *in.Index)
		return Result{Index: out}, err
	case KindGroup:
		if in.Group == nil {
			return Result{}, errors.New("分组任务缺少输入")
		}
		out, err := runGroup(ctx, env, *in.Group)
		return Result{Group: out}, err
	case KindDecide:
		if in.Decide == nil {
			return Result{}, errors.New("编目任务缺少输入")
		}
		out, err := runDecide(ctx, env, *in.Decide)
		return Result{Decide: out}, err
	case KindEntity:
		if in.Entity == nil {
			return Result{}, errors.New("名称检查任务缺少输入")
		}
		out, err := runEntity(ctx, env, *in.Entity)
		return Result{Entity: out}, err
	}
	return Result{}, fmt.Errorf("未知任务类型 %q", in.Kind)
}

// DecodeResult reads a Result sent by a node.
func DecodeResult(raw []byte) (Result, error) {
	var out Result
	err := json.Unmarshal(raw, &out)
	return out, err
}

// NewAdapter builds a storage adapter whose token rotation reports to env.
func NewAdapter(env Env, d DriveConfig) (storage.Adapter, error) {
	return env.Adapter(d.storageConfig(env))
}

func (d DriveConfig) storageConfig(env Env) storage.Config {
	secrets := make(map[string]string, len(d.Secrets))
	for k, v := range d.Secrets {
		secrets[k] = v
	}
	cfg := storage.Config{ID: d.ID, Kind: d.Kind, Settings: d.Settings, Secrets: secrets}
	if d.Kind == "onedrive" {
		cfg.SaveRefreshToken = func(ctx context.Context, previous, next string) error {
			if previous == next || next == "" || secrets["refresh_token"] != previous {
				return nil
			}
			if err := env.SaveRefreshToken(ctx, d.ID, previous, next); err != nil {
				return err
			}
			secrets["refresh_token"] = next
			return nil
		}
	}
	return cfg
}
