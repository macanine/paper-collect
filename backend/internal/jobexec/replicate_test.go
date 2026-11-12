package jobexec

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nic/paper-collect/backend/internal/storage"
)

// conflictAdapter fails Put/Move with a conflict until a suffixed key is used.
type conflictAdapter struct {
	putKeys  []string
	moveKeys []string
	taken    string
}

func (a *conflictAdapter) Put(_ context.Context, obj storage.Object) (storage.Stored, error) {
	a.putKeys = append(a.putKeys, obj.Key)
	if obj.Key == a.taken {
		return storage.Stored{}, storage.NewCloudError(storage.ClassConflict, "onedrive_path_taken", false)
	}
	return storage.Stored{RemoteID: "remote-1", Size: obj.Size, Verification: "sha256"}, nil
}

func (a *conflictAdapter) Move(_ context.Context, remoteID, key string) (storage.Stored, error) {
	a.moveKeys = append(a.moveKeys, key)
	if key == a.taken {
		return storage.Stored{}, storage.NewCloudError(storage.ClassConflict, "onedrive_move_http_409", false)
	}
	return storage.Stored{RemoteID: remoteID}, nil
}

func (a *conflictAdapter) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrUnsupported
}
func (a *conflictAdapter) Share(context.Context, string) (storage.Share, error) {
	return storage.Share{Status: "unsupported"}, nil
}
func (a *conflictAdapter) Health(context.Context) error { return nil }
func (a *conflictAdapter) Capabilities() storage.Capabilities {
	return storage.Capabilities{}
}

type replicateEnv struct {
	adapter storage.Adapter
	path    string
}

func (e replicateEnv) OpenBlob(context.Context, Blob) (io.ReadCloser, error) {
	return os.Open(e.path)
}
func (e replicateEnv) Progress(context.Context, string, int, int, map[string]any) {}
func (e replicateEnv) SaveRefreshToken(context.Context, string, string, string) error {
	return nil
}
func (e replicateEnv) Adapter(storage.Config) (storage.Adapter, error) { return e.adapter, nil }
func (e replicateEnv) TempDir() string                                 { return os.TempDir() }
func (e replicateEnv) Logf(string, ...any)                             {}

func replicateFixture(t *testing.T) (replicateEnv, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "original.pdf")
	if err := os.WriteFile(p, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("ab", 32)
	return replicateEnv{path: p}, sha
}

// The drive still holds an older file at the target path: keep both instead of
// failing the replica.
func TestReplicateUploadAvoidsATakenPath(t *testing.T) {
	env, sha := replicateFixture(t)
	adapter := &conflictAdapter{taken: "paper/a.pdf"}
	env.adapter = adapter
	out, err := runReplicate(context.Background(), env, ReplicateInput{
		Drive:       DriveConfig{ID: "d1", Kind: "onedrive"},
		Blob:        Blob{SHA256: sha, Size: 4},
		DesiredPath: "paper/a.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Uploaded || out.RemoteID != "remote-1" {
		t.Fatalf("result = %+v", out)
	}
	if out.RemotePath != "paper/a (abababab).pdf" {
		t.Fatalf("remote path = %q", out.RemotePath)
	}
	if len(adapter.putKeys) != 2 || adapter.putKeys[1] != out.RemotePath {
		t.Fatalf("put keys = %v", adapter.putKeys)
	}
}

// A rename onto a taken path falls back to the suffixed name as well.
func TestReplicateMoveAvoidsATakenPath(t *testing.T) {
	env, sha := replicateFixture(t)
	adapter := &conflictAdapter{taken: "paper/new.pdf"}
	env.adapter = adapter
	out, err := runReplicate(context.Background(), env, ReplicateInput{
		Drive:       DriveConfig{ID: "d1", Kind: "onedrive"},
		Blob:        Blob{SHA256: sha, Size: 4},
		DesiredPath: "paper/new.pdf",
		RemoteID:    "remote-1",
		RemotePath:  "paper/old.pdf",
		Move:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Moved || out.RemotePath != "paper/new (abababab).pdf" {
		t.Fatalf("result = %+v", out)
	}
}

// Moving again onto the same taken path is a no-op once the file is already
// under the conflict-free name, instead of failing every sync.
func TestReplicateMoveOntoItsOwnSuffixedNameIsANoop(t *testing.T) {
	env, sha := replicateFixture(t)
	adapter := &conflictAdapter{taken: "paper/new.pdf"}
	env.adapter = adapter
	out, err := runReplicate(context.Background(), env, ReplicateInput{
		Drive:       DriveConfig{ID: "d1", Kind: "onedrive"},
		Blob:        Blob{SHA256: sha, Size: 4},
		DesiredPath: "paper/new.pdf",
		RemoteID:    "remote-1",
		RemotePath:  "paper/new (abababab).pdf",
		Move:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Moved || out.RemotePath != "paper/new (abababab).pdf" {
		t.Fatalf("result = %+v", out)
	}
	if len(adapter.moveKeys) != 1 {
		t.Fatalf("move keys = %v", adapter.moveKeys)
	}
}
