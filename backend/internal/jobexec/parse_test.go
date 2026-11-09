package jobexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/storage"
)

type officeOCRTestEnv struct {
	tempDir string
	data    []byte
}

func (e officeOCRTestEnv) OpenBlob(context.Context, Blob) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(e.data)), nil
}

func (officeOCRTestEnv) Progress(context.Context, string, int, int, map[string]any) {}

func (officeOCRTestEnv) SaveRefreshToken(context.Context, string, string, string) error {
	return nil
}

func (officeOCRTestEnv) Adapter(storage.Config) (storage.Adapter, error) {
	return nil, nil
}

func (e officeOCRTestEnv) TempDir() string {
	return e.tempDir
}

func (officeOCRTestEnv) Logf(string, ...any) {}

type officeOCRTestClient struct {
	fileName string
	data     []byte
}

func (officeOCRTestClient) Health(context.Context) error {
	return nil
}

func (c *officeOCRTestClient) Extract(_ context.Context, fileName string, data []byte) (ai.Result, error) {
	c.fileName = fileName
	c.data = append([]byte(nil), data...)
	return ai.Result{Markdown: "recognized", PageCount: 1}, nil
}

func TestOfficeOCRConvertsDOCXToPDF(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("LibreOffice is not installed")
	}
	data, err := os.ReadFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	env := officeOCRTestEnv{tempDir: filepath.Join(t.TempDir(), "not-created-yet"), data: data}
	client := &officeOCRTestClient{}
	result, err := parseOfficeForOCR(t.Context(), env, client, Blob{
		SHA256:    hex.EncodeToString(hash[:]),
		Size:      int64(len(data)),
		FileName:  "原卷.docx",
		MediaType: docxMediaType,
	})
	if err != nil {
		t.Fatalf("parse office document: %v", err)
	}
	if result.Markdown != "recognized" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if client.fileName != "原卷.pdf" {
		t.Fatalf("OCR filename = %q, want 原卷.pdf", client.fileName)
	}
	if !bytes.HasPrefix(client.data, []byte("%PDF-")) {
		t.Fatalf("OCR input is not a PDF: %.16q", client.data)
	}
}
