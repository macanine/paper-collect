package office

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func converter(t *testing.T) *Converter {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	return New(dir)
}

func TestPDFRendersAndReusesSameDocument(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("LibreOffice Writer is not installed")
	}
	data := fixture(t)
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	c := converter(t)
	var calls atomic.Int32
	open := func(context.Context) (io.ReadCloser, error) {
		calls.Add(1)
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pdf, err := c.PDF(t.Context(), sha, open)
			if err != nil {
				t.Errorf("convert: %v", err)
				return
			}
			result, err := os.ReadFile(pdf)
			if err != nil || !bytes.HasPrefix(result, []byte("%PDF-")) {
				t.Errorf("invalid PDF: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("original opened %d times", calls.Load())
	}
	c.Remove(sha)
	if _, err := os.Stat(c.cachePath(sha)); !os.IsNotExist(err) {
		t.Fatalf("preview cache still exists: %v", err)
	}
}

func TestRejectsInvalidHash(t *testing.T) {
	_, err := converter(t).PDF(t.Context(), "not-a-hash", func(context.Context) (io.ReadCloser, error) {
		t.Fatal("original must not be opened for an invalid hash")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected invalid hash error")
	}
}

func TestRejectsMismatchedOriginal(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("LibreOffice Writer is not installed")
	}
	_, err := converter(t).PDF(t.Context(), strings.Repeat("0", 64), func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(fixture(t))), nil
	})
	if err == nil {
		t.Fatal("expected original hash mismatch")
	}
}

func TestRendererEnvironmentExcludesApplicationSecrets(t *testing.T) {
	t.Setenv("PAPER_TEST_SECRET", "private-test-value")
	for _, entry := range rendererEnvironment() {
		if strings.Contains(entry, "private-test-value") {
			t.Fatal("renderer received an unrelated application secret")
		}
	}
}
