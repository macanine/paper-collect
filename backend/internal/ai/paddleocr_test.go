package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePaddleOCRJSONL(t *testing.T) {
	raw := []byte(strings.Join([]string{
		`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":["第一页第一行","第一页第二行"]}}]}}`,
		`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":["第二页"]}}]}}`,
	}, "\n"))
	result, err := ParsePaddleOCRJSONL(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.PageCount != 2 || len(result.Pages) != 2 {
		t.Fatalf("pages=%d texts=%d", result.PageCount, len(result.Pages))
	}
	if result.Pages[0].PageNo != 1 || !strings.Contains(result.Pages[0].Text, "第一页第二行") {
		t.Fatalf("page 1: %+v", result.Pages[0])
	}
	if !strings.Contains(result.Markdown, "## 第 2 页") {
		t.Fatalf("markdown: %q", result.Markdown)
	}
}

func TestParsePaddleOCRJSONLAcceptsBareAndNestedResults(t *testing.T) {
	raw := []byte(`{"prunedResult":{"rec_texts":["裸结果"]}}` + "\n" +
		`{"result":{"res":{"rec_texts":["嵌套结果"]}}}`)
	result, err := ParsePaddleOCRJSONL(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(result.Pages) != 2 || result.Pages[1].Text != "嵌套结果" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestParsePaddleOCRJSONLRejectsEmptyText(t *testing.T) {
	if _, err := ParsePaddleOCRJSONL([]byte(`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":[]}}]}}`)); err == nil {
		t.Fatal("expected empty result error")
	}
}

func TestPaddleOCRExtractProtocol(t *testing.T) {
	var submitted bool
	var polled bool
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/api/v2/ocr/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer paddle-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"msg":"bad token"}`))
			return
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("multipart: %v", err)
		}
		if r.FormValue("model") != "PP-OCRv6" {
			t.Fatalf("model=%q", r.FormValue("model"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file: %v", err)
		}
		data, _ := io.ReadAll(file)
		file.Close()
		if string(data) != "pdf" {
			t.Fatalf("file=%q", data)
		}
		submitted = true
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"jobId": "job-1"}})
	})
	mux.HandleFunc("/api/v2/ocr/jobs/job-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer paddle-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		polled = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"state": "done", "resultUrl": map[string]string{"jsonUrl": server.URL + "/result.jsonl"}},
		})
	})
	mux.HandleFunc("/result.jsonl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":["识别文本"]}}]}}`))
	})

	client, err := NewPaddleOCR(server.URL, "paddle-token", "PP-OCRv6", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.PollInterval = time.Millisecond
	result, err := client.Extract(context.Background(), "试题.pdf", []byte("pdf"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !submitted || !polled || result.PageCount != 1 || result.Pages[0].Text != "识别文本" {
		t.Fatalf("submitted=%v polled=%v result=%+v", submitted, polled, result)
	}
}

func TestPaddleOCRDefaultClientDoesNotOverrideJobDeadline(t *testing.T) {
	client, err := NewPaddleOCR("https://paddleocr.example.com", "paddle-token", "PP-OCRv6", nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.HTTP.Timeout != 0 {
		t.Fatalf("default HTTP timeout=%s, want no client-wide timeout", client.HTTP.Timeout)
	}
	if client.Timeout != 15*time.Minute {
		t.Fatalf("job timeout=%s, want 15m", client.Timeout)
	}
}
