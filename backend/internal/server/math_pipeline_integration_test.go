package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormulaBlocksSurviveSegmentationAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ocr/jobs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "formula-job"})
	})
	mux.HandleFunc("/api/v1/ocr/jobs/formula-job", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "done", "result_url": "/formula.jsonl"})
	})
	mux.HandleFunc("/formula.jsonl", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"page_no": 1,
			"blocks": []map[string]string{
				{"type": "text", "text": "1. 已知 "},
				{"type": "inline_formula", "latex": `\frac{x^{2}+1}{2}`},
				{"type": "text", "text": "，求值。"},
			},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		// A model may return a single backslash in its inner JSON. JSON accepts
		// \f as a control escape, so the pipeline must recover the source formula.
		content := `{"questions":[{"page_no":1,"question_no":"1","text":"已知 \frac{x^{2}+1}{2}，求值。","confidence":0.9}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "公式切题模型", "base_url": upstream.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("AI provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/ocr/providers", map[string]any{
		"name": "公式 OCR", "kind": "rapidocr", "base_url": upstream.URL, "model": "test-formula", "enabled": true, "priority": 1,
	}, nil)
	var provider struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if status != http.StatusCreated || json.Unmarshal(body, &provider) != nil || provider.Data.ID == "" {
		t.Fatalf("OCR provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": false, "ocr_provider_id": provider.Data.ID, "max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("OCR settings %d: %s", status, body)
	}
	uploadID, _ := env.upload(t, "公式试卷", map[string][]byte{"数学试卷.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	env.processJobs(t)

	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "(x²+1)/2"}, nil)
	var search struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				Text   string `json:"text"`
				PageNo int    `json:"page_no"`
			} `json:"items"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &search) != nil || search.Data.Total != 1 || len(search.Data.Items) != 1 {
		t.Fatalf("formula search %d: %s", status, body)
	}
	if search.Data.Items[0].PageNo != 1 || !strings.Contains(search.Data.Items[0].Text, `\frac{x^{2}+1}{2}`) {
		t.Fatalf("formula or page was lost: %s", body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "(x²-1)/2"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":0`) {
		t.Fatalf("different formula matched %d: %s", status, body)
	}
}
