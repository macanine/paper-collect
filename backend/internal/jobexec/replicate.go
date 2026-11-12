package jobexec

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/storage"
)

// ReplicateInput describes one upload, move or share refresh.
type ReplicateInput struct {
	Drive       DriveConfig `json:"drive"`
	Blob        Blob        `json:"blob"`
	DesiredPath string      `json:"desired_path"`
	RemoteID    string      `json:"remote_id"`
	RemotePath  string      `json:"remote_path"`
	Move        bool        `json:"move"` // the drive moves objects when the desired path changes
	Share       bool        `json:"share"`
}

// ReplicateResult is the observed remote state after execution.
type ReplicateResult struct {
	Uploaded       bool       `json:"uploaded"`
	Moved          bool       `json:"moved"`
	RemoteID       string     `json:"remote_id"`
	RemotePath     string     `json:"remote_path"`
	SizeBytes      int64      `json:"size_bytes"`
	Verification   string     `json:"verification"`
	ShareStatus    string     `json:"share_status"`
	ShareURL       string     `json:"share_url"`
	SharePassword  string     `json:"share_password"`
	ShareExpiresAt *time.Time `json:"share_expires_at"`
	Note           string     `json:"note"`
}

// Share outcomes stored on a replica.
const (
	ShareReady       = "ready"
	ShareUnsupported = "unsupported"
	ShareFailed      = "failed"
)

func runReplicate(ctx context.Context, env Env, in ReplicateInput) (*ReplicateResult, error) {
	adapter, err := NewAdapter(env, in.Drive)
	if err != nil {
		return nil, err
	}
	out := &ReplicateResult{RemoteID: in.RemoteID, RemotePath: in.RemotePath}
	if in.RemoteID == "" {
		path, cleanup, err := localBlobPath(ctx, env, in.Blob)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		env.Progress(ctx, "正在上传文件并验证远端副本", 0, 0, nil)
		key := in.DesiredPath
		stored, err := adapter.Put(ctx, storage.Object{Key: key, Path: path, SHA256: in.Blob.SHA256, Size: in.Blob.Size})
		if pathConflict(err) {
			// The drive still holds an older file at this path. Keep both: upload
			// beside it under the SHA suffix the planner already uses.
			key = ConflictPath(key, in.Blob.SHA256)
			env.Progress(ctx, "目标路径已有其他文件，改用带 SHA 后缀的文件名", 0, 0, nil)
			stored, err = adapter.Put(ctx, storage.Object{Key: key, Path: path, SHA256: in.Blob.SHA256, Size: in.Blob.Size})
		}
		if err != nil {
			return nil, err
		}
		out.Uploaded = true
		out.RemoteID, out.RemotePath = stored.RemoteID, key
		out.SizeBytes, out.Verification = stored.Size, stored.Verification
	} else if in.RemotePath != in.DesiredPath && in.Move {
		mover, ok := adapter.(storage.Mover)
		if !ok {
			return nil, storage.ErrUnsupported
		}
		env.Progress(ctx, "正在移动远端副本", 0, 0, nil)
		target := in.DesiredPath
		stored, err := mover.Move(ctx, in.RemoteID, target)
		if pathConflict(err) {
			alt := ConflictPath(target, in.Blob.SHA256)
			if alt == in.RemotePath {
				// Already under the conflict-free name; nothing to do.
				target = in.RemotePath
				err = nil
			} else {
				stored, err = mover.Move(ctx, in.RemoteID, alt)
				target = alt
			}
		}
		if err != nil {
			return nil, err
		}
		if target != in.RemotePath {
			out.Moved = true
			out.RemoteID, out.RemotePath = stored.RemoteID, target
		}
	}
	if in.Share {
		out.ShareURL, out.SharePassword, out.ShareExpiresAt, out.ShareStatus, out.Note = ShareReplica(ctx, adapter, out.RemoteID)
	}
	return out, nil
}

// pathConflict reports a target key that already holds a different object.
// Replicas never overwrite, so the caller retries under a suffixed name.
func pathConflict(err error) bool {
	return err != nil && storage.AsCloudError(err).Class == storage.ClassConflict
}

// ConflictPath appends the first eight characters of the SHA-256 before the
// extension: the suffix the planner uses when two files resolve to one path.
func ConflictPath(key, sha string) string {
	if len(sha) > 8 {
		sha = sha[:8]
	}
	ext := path.Ext(key)
	return strings.TrimSuffix(key, ext) + " (" + sha + ")" + ext
}

// ShareReplica asks the adapter for a share link and classifies the outcome.
// An unsupported share is a normal outcome, not a failed replica.
func ShareReplica(ctx context.Context, adapter storage.Adapter, remoteID string) (url, password string, expires *time.Time, status, note string) {
	if remoteID == "" {
		return "", "", nil, ShareFailed, "share_remote_id_missing"
	}
	share, err := adapter.Share(ctx, remoteID)
	if err != nil {
		if errors.Is(err, storage.ErrUnsupported) || storage.AsCloudError(err).Class == storage.ClassUnsupported {
			return "", "", nil, ShareUnsupported, ""
		}
		return "", "", nil, ShareFailed, StorageErrorText(err)
	}
	if share.Status == "unsupported" {
		return "", "", nil, ShareUnsupported, ""
	}
	if share.URL == "" {
		return "", "", nil, ShareFailed, "share_url_empty"
	}
	return share.URL, share.Password, share.ExpiresAt, ShareReady, ""
}

// localBlobPath returns a readable file. A local CAS file is used in place;
// otherwise the verified stream is spilled to a private temporary file.
func localBlobPath(ctx context.Context, env Env, b Blob) (string, func(), error) {
	body, err := env.OpenBlob(ctx, b)
	if err != nil {
		return "", nil, err
	}
	defer body.Close()
	if f, ok := body.(*os.File); ok {
		return f.Name(), func() {}, nil
	}
	if p, ok := body.(interface{ Path() string }); ok && p.Path() != "" {
		return p.Path(), func() {}, nil
	}
	if err := os.MkdirAll(env.TempDir(), 0o700); err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp(env.TempDir(), "replicate-*.part")
	if err != nil {
		return "", nil, err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return name, cleanup, nil
}
