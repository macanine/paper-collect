package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAI struct {
	server      *httptest.Server
	mu          sync.Mutex
	lastChat    map[string]any
	visionHit   bool
	visionSeen  bool
	minerUCalls int
	paddleCalls int
}

func newFakeAI(t *testing.T) *fakeAI {
	t.Helper()
	f := &fakeAI{}
	zipBytes := buildMineruZip(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/file-urls/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mineru-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"msg":"bad token"}`))
			return
		}
		f.mu.Lock()
		f.minerUCalls++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"batch_id": "batch-1", "file_urls": []string{f.server.URL + "/upload/one"}},
		})
	})
	mux.HandleFunc("/upload/one", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v4/extract-results/batch/batch-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"extract_result": []map[string]any{{
				"file_name": "学生版.pdf", "state": "done", "full_zip_url": f.server.URL + "/result.zip",
			}}},
		})
	})
	mux.HandleFunc("/result.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})
	mux.HandleFunc("/api/v2/ocr/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer paddle-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"msg":"bad token"}`))
			return
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Fatalf("paddle multipart: %v", err)
		}
		f.mu.Lock()
		f.paddleCalls++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "data": map[string]any{"jobId": "ocr-job-1"},
		})
	})
	mux.HandleFunc("/api/v2/ocr/jobs/ocr-job-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"state": "done", "resultUrl": map[string]string{"jsonUrl": f.server.URL + "/result.jsonl"},
			},
		})
	})
	mux.HandleFunc("/result.jsonl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Join([]string{
			`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":["1. 已知函数 f(x)=x^2+2x，求最小值。"]}}]}}`,
			`{"result":{"ocrResults":[{"prunedResult":{"rec_texts":["2. 求函数 f(x)=x^2 的导数。"]}}]}}`,
		}, "\n")))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "text-model"}, {"id": "vision-model"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.mu.Lock()
		f.lastChat = payload
		raw, _ := json.Marshal(payload["messages"])
		f.visionHit = bytes.Contains(raw, []byte("image_url"))
		if f.visionHit {
			f.visionSeen = true
		}
		f.mu.Unlock()
		content := `{"title":"2026 高三数学一轮练习","subject_codes":["math"],"grade":"grade12","academic_year":2026,"role":"exam","tags":["一轮复习","函数"],"confidence":0.87}`
		if bytes.Contains(raw, []byte("[PAGE")) {
			pageRe := regexp.MustCompile(`\[PAGE (\d+)\]`)
			matches := pageRe.FindAllSubmatch(raw, -1)
			questions := make([]map[string]any, 0, len(matches))
			for _, match := range matches {
				pageNo, _ := strconv.Atoi(string(match[1]))
				text := "1. 已知函数 f(x)=x^2+2x，求最小值。"
				if pageNo == 2 {
					text = "2. 求函数 f(x)=x^2 的导数。"
				}
				questions = append(questions, map[string]any{
					"page_no": pageNo, "question_no": strconv.Itoa(pageNo), "text": text, "confidence": 0.9,
				})
			}
			encoded, _ := json.Marshal(map[string]any{"questions": questions})
			content = string(encoded)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAI) parseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minerUCalls
}

func (f *fakeAI) paddleParseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paddleCalls
}

func (f *fakeAI) chatContains(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(f.lastChat)
	return bytes.Contains(raw, []byte(text))
}

func buildMineruZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	md, _ := zw.Create("full.md")
	_, _ = md.Write([]byte("# 2026 高三数学一轮练习\n\n学生版，函数与导数。"))
	cl, _ := zw.Create("content_list.json")
	_, _ = cl.Write([]byte(`[
		{"type":"text","text":"1. 已知函数 f(x)=x^2+2x，求最小值。","page_idx":0},
		{"type":"text","text":"2. 求函数 f(x)=x^2 的导数。","page_idx":1}
	]`))
	// Minimal PNG signature is enough for contentType detection in the UI test path.
	img, _ := zw.Create("images/page-1.png")
	_, _ = img.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	return buf.Bytes()
}

func (e *testEnv) processJobs(t *testing.T) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if e.svc.ProcessAvailable(context.Background(), 100) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("jobs did not settle")
}

func TestAIAnalysisEndToEnd(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)

	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "测试网关", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model", "vision-model"}, "default_model": "text-model",
		"vision_model": "vision-model", "priority": 5, "enabled": true,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create provider %d: %s", status, body)
	}
	if strings.Contains(string(body), "sk-test") {
		t.Fatalf("API key leaked in response: %s", body)
	}
	var providerBody struct {
		Data struct {
			ID               string `json:"id"`
			APIKeyConfigured bool   `json:"api_key_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &providerBody); err != nil || !providerBody.Data.APIKeyConfigured {
		t.Fatalf("provider body: %s", body)
	}
	providerID := providerBody.Data.ID

	status, body = env.do(t, http.MethodPost, "/api/v1/ai/providers/probe", map[string]any{
		"base_url": fake.server.URL + "/v1", "api_key": "sk-test",
	}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"count\":2") {
		t.Fatalf("probe %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/ai/providers/"+providerID+"/probe", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "vision-model") {
		t.Fatalf("saved probe %d: %s", status, body)
	}

	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": true, "mineru_base_url": fake.server.URL,
		"mineru_token": "mineru-token", "max_pages": 3, "max_images": 2,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put settings %d: %s", status, body)
	}
	if strings.Contains(string(body), "mineru-token") {
		t.Fatalf("MinerU token leaked: %s", body)
	}

	uploadID, _ := env.upload(t, "AI 识别验证", map[string][]byte{"学生版.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	env.processJobs(t)

	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=20", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("materials %d: %s", status, body)
	}
	var list struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Data.Items) == 0 {
		t.Fatalf("materials body: %s", body)
	}
	materialID := list.Data.Items[0].ID

	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/analysis", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("analysis %d: %s", status, body)
	}
	var analysis struct {
		Data struct {
			Status       string  `json:"status"`
			ProviderName string  `json:"provider_name"`
			ImageCount   int     `json:"image_count"`
			Confidence   float64 `json:"confidence"`
			AppliedAt    *string `json:"applied_at"`
			Suggestions  struct {
				Title    string   `json:"title"`
				Subjects []string `json:"subject_codes"`
				Role     string   `json:"role"`
			} `json:"suggestions"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &analysis); err != nil {
		t.Fatalf("analysis body: %s", body)
	}
	if analysis.Data.Status != "succeeded" || analysis.Data.Error != "" {
		t.Fatalf("analysis failed: %s", body)
	}
	if analysis.Data.Suggestions.Title == "" || analysis.Data.Suggestions.Role != "exam" {
		t.Fatalf("unexpected suggestions: %s", body)
	}
	if analysis.Data.ImageCount != 0 || analysis.Data.ProviderName != "测试网关" {
		t.Fatalf("image/provider mismatch: %s", body)
	}
	if !fake.visionSeen {
		t.Fatalf("vision model did not receive image_url parts")
	}
	if !fake.chatContains("学生版，函数与导数。") {
		t.Fatalf("document body was not sent to the classification model")
	}
	if analysis.Data.AppliedAt == nil {
		t.Fatalf("successful suggestions were not auto-applied: %s", body)
	}

	// Automatic application is part of job completion; no manual apply is needed.
	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("material %d: %s", status, body)
	}
	var automatic struct {
		Data struct {
			Title    string   `json:"title"`
			FileName string   `json:"file_name"`
			Subjects []string `json:"subject_codes"`
			Year     int      `json:"academic_year"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &automatic); err != nil {
		t.Fatalf("automatic apply body: %s", body)
	}
	if automatic.Data.Title != "2026 高三数学一轮练习" ||
		automatic.Data.FileName != "2026 高三数学一轮练习.pdf" ||
		automatic.Data.Year != 2026 {
		t.Fatalf("automatic AI metadata not applied: %s", body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/materials/"+materialID+"/analysis/apply", map[string]any{}, nil)
	if status != http.StatusOK {
		t.Fatalf("apply %d: %s", status, body)
	}
	var applied struct {
		Data struct {
			Title    string   `json:"title"`
			Subjects []string `json:"subject_codes"`
			Year     int      `json:"academic_year"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &applied); err != nil {
		t.Fatalf("apply body: %s", body)
	}
	if applied.Data.Title != "2026 高三数学一轮练习" || applied.Data.Year != 2026 {
		t.Fatalf("suggestions could not be reapplied: %s", body)
	}

	// Idempotent second enqueue returns the existing analysis, not a new job.
	status, body = env.do(t, http.MethodPost, "/api/v1/materials/"+materialID+"/analysis", map[string]any{}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("re-enqueue %d: %s", status, body)
	}

	// Reanalysis must not wait for another SQLite connection inside a transaction.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.base+"/api/v1/materials/"+materialID+"/analysis", strings.NewReader(`{"force":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("force reanalysis: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("force reanalysis: %d", resp.StatusCode)
	}
}

func TestAIProviderRejectsBadKeyWithoutLeaking(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers/probe", map[string]any{
		"base_url": fake.server.URL + "/v1", "api_key": "wrong-key",
	}, nil)
	if status != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", status, body)
	}
	if strings.Contains(string(body), "wrong-key") {
		t.Fatalf("key leaked: %s", body)
	}
}

func TestOCRProviderSelectionAndMigration(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)

	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "文本模型", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("model provider %d: %s", status, body)
	}

	// Legacy settings still work and materialize the old MinerU configuration.
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": false, "mineru_base_url": fake.server.URL,
		"mineru_token": "mineru-token", "max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK || strings.Contains(string(body), "mineru-token") {
		t.Fatalf("legacy settings %d: %s", status, body)
	}
	var settings struct {
		Data struct {
			OCRProviderID string `json:"ocr_provider_id"`
			OCRConfigured bool   `json:"ocr_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &settings); err != nil || settings.Data.OCRProviderID == "" || !settings.Data.OCRConfigured {
		t.Fatalf("legacy OCR migration missing: %s", body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/ocr/providers", map[string]any{
		"name": "PaddleOCR v6", "kind": "paddleocr", "base_url": fake.server.URL,
		"model": "PP-OCRv6", "token": "paddle-token", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated || strings.Contains(string(body), "paddle-token") {
		t.Fatalf("paddle provider %d: %s", status, body)
	}
	var paddle struct {
		Data struct {
			ID              string `json:"id"`
			TokenConfigured bool   `json:"token_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &paddle); err != nil || paddle.Data.ID == "" || !paddle.Data.TokenConfigured {
		t.Fatalf("paddle response: %s", body)
	}

	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": true, "ocr_provider_id": paddle.Data.ID,
		"mineru_base_url": fake.server.URL, "max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("select paddle %d: %s", status, body)
	}
	// An older client may omit ocr_provider_id. It must not silently clear the
	// selected PaddleOCR provider.
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": true, "mineru_base_url": fake.server.URL,
		"max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), paddle.Data.ID) {
		t.Fatalf("legacy update cleared OCR provider %d: %s", status, body)
	}

	uploadID, _ := env.upload(t, "Paddle 识别", map[string][]byte{"试题.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	env.processJobs(t)
	if fake.paddleParseCalls() != 1 || fake.parseCalls() != 0 {
		t.Fatalf("expected PaddleOCR only, paddle=%d mineru=%d", fake.paddleParseCalls(), fake.parseCalls())
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/ai/settings", nil, nil)
	if status != http.StatusOK || strings.Contains(string(body), "paddle-token") || strings.Contains(string(body), "mineru-token") {
		t.Fatalf("token leaked: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/ocr/providers/"+paddle.Data.ID+"/test", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"healthy":true`) {
		t.Fatalf("paddle health %d: %s", status, body)
	}
}

func TestQuestionSearchIndexAndPages(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "切题模型", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": false, "mineru_base_url": fake.server.URL,
		"mineru_token": "mineru-token", "max_pages": 10, "max_images": 0,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("ai settings %d: %s", status, body)
	}

	uploadID, _ := env.upload(t, "搜题测试卷", map[string][]byte{"学生版.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	env.processJobs(t)

	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("materials %d: %s", status, body)
	}
	var list struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Data.Items) != 1 {
		t.Fatalf("materials body: %s", body)
	}
	materialID := list.Data.Items[0].ID

	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/index", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"succeeded\"") {
		t.Fatalf("index state %d: %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "已知函数"}, nil)
	if status != http.StatusOK {
		t.Fatalf("search %d: %s", status, body)
	}
	var result struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				PageNo  int    `json:"page_no"`
				Text    string `json:"text"`
				Snippet string `json:"snippet"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Data.Total == 0 {
		t.Fatalf("search body: %s", body)
	}
	if result.Data.Items[0].PageNo != 1 {
		t.Fatalf("expected page 1 first, got %+v", result.Data.Items)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "求导数"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"page_no\":2") {
		t.Fatalf("page-2 search %d: %s", status, body)
	}

	// No cross-material aggregation: 函数 matches both page blocks.
	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "函数"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":2") {
		t.Fatalf("expected 2 independent hits: %s", body)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/questions?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":2") {
		t.Fatalf("material questions %d: %s", status, body)
	}

	// Manual reindex is idempotent: still two questions, not four.
	status, body = env.do(t, http.MethodPost, "/api/v1/materials/"+materialID+"/index", map[string]any{"force": true}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("reindex %d: %s", status, body)
	}
	env.processJobs(t)
	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/questions?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":2") {
		t.Fatalf("reindex duplicated questions: %s", body)
	}
}
