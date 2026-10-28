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

func TestRapidOCRExtractProtocol(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/v1/ocr/jobs", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("multipart: %v", err)
		}
		if r.FormValue("model") != "PP-OCRv5" {
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
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job-1"})
	})
	mux.HandleFunc("/api/v1/ocr/jobs/job-1", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"state": "done", "result_url": "/results/job-1.jsonl",
		})
	})
	mux.HandleFunc("/results/job-1.jsonl", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page_no":1,"texts":["第一页","化学"]}`))
	})

	client, err := NewRapidOCR(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.PollInterval = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	result, err := client.Extract(ctx, "试卷.pdf", []byte("pdf"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if result.PageCount != 1 || !strings.Contains(result.Markdown, "第一页") {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseRapidOCRJSONL(t *testing.T) {
	result, err := ParseRapidOCRJSONL([]byte(
		`{"page_no":2,"texts":["第二页"]}` + "\n" +
			`{"page_no":1,"texts":["第一页"]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.PageCount != 2 || result.Pages[0].PageNo != 2 || result.Pages[0].Text != "第二页" {
		t.Fatalf("result=%+v", result)
	}
}
