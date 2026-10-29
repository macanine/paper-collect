package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/docx"
	"github.com/nic/paper-collect/backend/internal/model"
)

type fakeOCRServer struct {
	mu      sync.Mutex
	next    int
	uploads map[string][]byte
	names   map[string]string
}

func newFakeOCRServer(t *testing.T) (*fakeOCRServer, *httptest.Server) {
	t.Helper()
	fake := &fakeOCRServer{uploads: map[string][]byte{}, names: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ocr/jobs", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "multipart", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file", http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		file.Close()
		fake.mu.Lock()
		fake.next++
		id := fmt.Sprintf("job-%d", fake.next)
		fake.uploads[id] = data
		fake.names[id] = header.Filename
		fake.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": id})
	})
	mux.HandleFunc("/api/v1/ocr/jobs/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/ocr/jobs/")
		fake.mu.Lock()
		_, ok := fake.uploads[id]
		fake.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "done", "result_url": "/result/" + id})
	})
	mux.HandleFunc("/result/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"page_no\":1,\"texts\":[\"图片文字 123\"]}\n"))
	})
	return fake, httptest.NewServer(mux)
}

func docxMaterial(t *testing.T, svc *Service) model.Material {
	t.Helper()
	data, err := os.ReadFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	material := model.Material{
		ID: "material", BlobID: "blob", SHA256: hex.EncodeToString(hash[:]),
		FileName: "试卷.docx", MediaType: blob.MediaTypes[blob.KindDOCX], SizeBytes: int64(len(data)),
	}
	path := svc.blobs.Path(material.SHA256)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return material
}

func TestDOCXIsParsedNativelyAndImagesAreOCRd(t *testing.T) {
	fake, server := newFakeOCRServer(t)
	defer server.Close()
	svc, db := newWorkerTestService(t, t.TempDir(), "docx-native")
	provider := model.OCRProvider{ID: "ocr", Name: "test", Kind: model.OCRKindRapidOCR, BaseURL: server.URL, Model: "PP-OCRv5", Enabled: true}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	material := docxMaterial(t, svc)
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	result, parsedProvider, err := svc.parseWithOCR(t.Context(), model.AISettings{OCRProviderID: provider.ID}, material)
	if err != nil {
		t.Fatal(err)
	}
	if result.PageCount != 2 || len(result.Pages) != 2 {
		t.Fatalf("unexpected page count: %+v", result)
	}
	for _, want := range []string{"第一题", "| 题号 | 答案 |", "参考答案", "图片文字 123"} {
		if !strings.Contains(result.Markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, result.Markdown)
		}
	}
	if strings.Index(result.Pages[0].Text, "图片文字 123") > strings.Index(result.Pages[0].Text, "x+y=2") {
		t.Fatalf("image text lost its position: %q", result.Pages[0].Text)
	}
	if parsedProvider.ID != provider.ID {
		t.Fatalf("provider = %+v", parsedProvider)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.uploads) != 1 {
		t.Fatalf("OCR uploads = %d, want only the embedded image", len(fake.uploads))
	}
	for _, name := range fake.names {
		if name != "image-01.png" {
			t.Fatalf("OCR filename = %q, DOCX must not be submitted as PDF", name)
		}
	}
	for _, data := range fake.uploads {
		if bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatal("DOCX was converted to PDF")
		}
	}
	if textParseKey(provider, material.MediaType) == textParseKey(provider, "application/pdf") {
		t.Fatal("DOCX and PDF must use different parse cache versions")
	}
}

func TestDOCXKeepsTextWhenImageOCRFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	svc, db := newWorkerTestService(t, t.TempDir(), "docx-image-failure")
	provider := model.OCRProvider{ID: "ocr", Name: "test", Kind: model.OCRKindRapidOCR, BaseURL: server.URL, Model: "PP-OCRv5", Enabled: true}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	material := docxMaterial(t, svc)
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	result, _, err := svc.parseWithOCR(t.Context(), model.AISettings{OCRProviderID: provider.ID}, material)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, "参考答案") || strings.Contains(result.Markdown, "图片文字") {
		t.Fatalf("unexpected markdown: %s", result.Markdown)
	}
}

func TestRemoteDOCXSpillsToTemporaryFile(t *testing.T) {
	svc, _ := newWorkerTestService(t, t.TempDir(), "docx-remote")
	data, err := os.ReadFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	readerAt, size, cleanup, err := svc.documentReaderAt(Download{
		Reader: io.NopCloser(bytes.NewReader(data)),
		Size:   int64(len(data)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	document, err := docx.Parse(readerAt, size)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Pages) != 2 {
		t.Fatalf("pages = %d", len(document.Pages))
	}
}
