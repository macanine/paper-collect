package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeSource(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return path
}

func newTestLocal(t *testing.T) (Adapter, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	adapter, err := New(Config{ID: "local-1", Kind: "local", Settings: map[string]string{"root": root}})
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
	return adapter, root
}

func requireClass(t *testing.T, err error, class ErrorClass) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", class)
	}
	if got := AsCloudError(err).Class; got != class {
		t.Fatalf("class = %s, want %s (err=%v)", got, class, err)
	}
}

func TestLocalPutWritesVerifiedObject(t *testing.T) {
	adapter, root := newTestLocal(t)
	data := []byte("%PDF-1.4 local adapter fixture")
	src := writeSource(t, t.TempDir(), "paper.pdf", data)
	key := "ab/" + sha256Hex(data) + "/paper.pdf"

	stored, err := adapter.Put(context.Background(), Object{
		Key: key, Name: "paper.pdf", Path: src, SHA256: sha256Hex(data), Size: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.RemoteID != key || stored.Size != int64(len(data)) || stored.Verification != "sha256" {
		t.Fatalf("stored = %+v", stored)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil || string(got) != string(data) {
		t.Fatalf("object content mismatch: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(filepath.Join(root, filepath.FromSlash(key)))); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %d entries", len(entries))
	}
}

func TestLocalPutWithoutSHA256ReportsSizeOnly(t *testing.T) {
	adapter, _ := newTestLocal(t)
	data := []byte("no hash supplied")
	src := writeSource(t, t.TempDir(), "n.txt", data)

	stored, err := adapter.Put(context.Background(), Object{Key: "x/n.txt", Path: src, Size: int64(len(data))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if stored.Verification != "size" {
		t.Fatalf("verification = %q, want size (no sha256 was checked)", stored.Verification)
	}
}

func TestLocalPutRejectsUnsafeKeys(t *testing.T) {
	adapter, _ := newTestLocal(t)
	src := writeSource(t, t.TempDir(), "p.pdf", []byte("x"))
	bad := []string{"", "/abs/p.pdf", "../escape.pdf", "a/../../b.pdf", "a//b.pdf", "a/./b.pdf", `a\b.pdf`, "a/\x00b", "a/" + strings.Repeat("c", 1100)}
	for _, key := range bad {
		_, err := adapter.Put(context.Background(), Object{Key: key, Path: src, Size: 1})
		requireClass(t, err, ClassInvalidInput)
	}
}

func TestLocalPutRejectsSizeMismatchBeforeWriting(t *testing.T) {
	adapter, root := newTestLocal(t)
	src := writeSource(t, t.TempDir(), "s.pdf", []byte("12345"))
	_, err := adapter.Put(context.Background(), Object{Key: "k/s.pdf", Path: src, Size: 3})
	requireClass(t, err, ClassSizeMismatch)
	if _, statErr := os.Stat(filepath.Join(root, "k", "s.pdf")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("object must not be published on size mismatch")
	}
}

func TestLocalPutHashMismatchLeavesNoObject(t *testing.T) {
	adapter, root := newTestLocal(t)
	src := writeSource(t, t.TempDir(), "h.pdf", []byte("actual bytes"))
	wrong := sha256Hex([]byte("other bytes"))
	_, err := adapter.Put(context.Background(), Object{
		Key: "k/h.pdf", Path: src, SHA256: wrong, Size: int64(len("actual bytes")),
	})
	requireClass(t, err, ClassHashMismatch)
	entries, _ := os.ReadDir(filepath.Join(root, "k"))
	if len(entries) != 0 {
		t.Fatalf("hash mismatch must remove temp file and publish nothing, found %d", len(entries))
	}
}

func TestLocalPutMissingSourceIsFileMissing(t *testing.T) {
	adapter, _ := newTestLocal(t)
	_, err := adapter.Put(context.Background(), Object{Key: "k/m.pdf", Path: filepath.Join(t.TempDir(), "nope"), Size: 1})
	requireClass(t, err, ClassFileMissing)
}

func TestLocalPutRejectsInvalidSHA256Format(t *testing.T) {
	adapter, _ := newTestLocal(t)
	src := writeSource(t, t.TempDir(), "f.pdf", []byte("x"))
	_, err := adapter.Put(context.Background(), Object{Key: "k/f.pdf", Path: src, Size: 1, SHA256: "not-hex"})
	requireClass(t, err, ClassInvalidInput)
}

func TestLocalOpenReturnsContentAndMissingIsFileMissing(t *testing.T) {
	adapter, _ := newTestLocal(t)
	data := []byte("open me")
	src := writeSource(t, t.TempDir(), "o.txt", data)
	if _, err := adapter.Put(context.Background(), Object{Key: "d/o.txt", Path: src, Size: int64(len(data))}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	reader, err := adapter.Open(context.Background(), "d/o.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != string(data) {
		t.Fatalf("read back = %q, %v", got, err)
	}

	_, err = adapter.Open(context.Background(), "d/missing.txt")
	requireClass(t, err, ClassFileMissing)
	_, err = adapter.Open(context.Background(), "../etc/passwd")
	requireClass(t, err, ClassInvalidInput)
}

func TestLocalOpenRejectsSymlinkEscape(t *testing.T) {
	adapter, root := newTestLocal(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := adapter.Open(context.Background(), "link/secret.txt")
	requireClass(t, err, ClassInvalidInput)

	src := writeSource(t, t.TempDir(), "w.txt", []byte("w"))
	_, err = adapter.Put(context.Background(), Object{Key: "link/w.txt", Path: src, Size: 1})
	requireClass(t, err, ClassInvalidInput)
	if _, statErr := os.Stat(filepath.Join(outside, "w.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Put must not write outside the root through a symlink")
	}
}

func TestLocalShareIsUnsupportedWithoutURL(t *testing.T) {
	adapter, _ := newTestLocal(t)
	share, err := adapter.Share(context.Background(), "k/s.pdf")
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if share.Status != "unsupported" || share.URL != "" {
		t.Fatalf("share = %+v, want unsupported without URL", share)
	}
}

func TestLocalHealthProbeLeavesNoFiles(t *testing.T) {
	adapter, root := newTestLocal(t)
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("probe file left behind: %d entries", len(entries))
	}
}

func TestLocalHealthFailsWhenRootRemoved(t *testing.T) {
	adapter, root := newTestLocal(t)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	requireClass(t, adapter.Health(context.Background()), ClassConfig)
}

func TestLocalCapabilities(t *testing.T) {
	adapter, _ := newTestLocal(t)
	caps := adapter.Capabilities()
	if !caps.Upload || !caps.Download || caps.Share {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestLocalConfigRequiresAbsoluteRoot(t *testing.T) {
	for _, settings := range []map[string]string{
		nil,
		{"root": ""},
		{"root": "relative/dir"},
	} {
		_, err := New(Config{Kind: "local", Settings: settings})
		requireClass(t, err, ClassConfig)
	}
}

func TestFactoryUnknownKindIsUnsupported(t *testing.T) {
	_, err := New(Config{Kind: "ftp"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	requireClass(t, err, ClassUnsupported)
}
