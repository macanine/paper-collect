package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAI struct {
	server     *httptest.Server
	mu         sync.Mutex
	visionSeen bool
	bodySeen   bool
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
		messages, _ := json.Marshal(payload["messages"])
		text := fakeChatText(messages)
		f.mu.Lock()
		if bytes.Contains(messages, []byte("image_url")) {
			f.visionSeen = true
		}
		if strings.Contains(text, "学生版，函数与导数。") {
			f.bodySeen = true
		}
		f.mu.Unlock()
		content := `{"same_as":"","reason":"没有重复"}`
		switch {
		case strings.Contains(text, "旁证信息："):
			content = fakeCatalogCard(text)
		case strings.Contains(text, "待分组的文件："):
			content = fakeCatalogGroup(text)
			t.Logf("fake group reply: %s", content)
		case strings.Contains(text, "本套文件（card 为已校验的阅读记录）："):
			content = fakeCatalogDecision(text)
		case strings.Contains(text, "试卷题目结构化助手"):
			pageRe := regexp.MustCompile(`\[PAGE (\d+)\]`)
			matches := pageRe.FindAllStringSubmatch(text, -1)
			questions := make([]map[string]any, 0, len(matches))
			for _, match := range matches {
				pageNo, _ := strconv.Atoi(match[1])
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

func fakeChatText(raw []byte) string {
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &messages) != nil {
		return ""
	}
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			parts = append(parts, text)
			continue
		}
		var content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &content) == nil {
			for _, part := range content {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func fakeCatalogCard(text string) string {
	context := text
	if marker := strings.Index(context, "旁证信息：\n"); marker >= 0 {
		context = context[marker:]
	}
	if marker := strings.Index(context, "同一文件夹的其他文件："); marker >= 0 {
		context = context[:marker]
	}
	role, docType := "questions", "topic"
	subjects := []string{"math"}
	switch {
	case strings.Contains(context, "化学") || strings.Contains(strings.ToLower(context), "chem"):
		subjects = []string{"chemistry"}
	case strings.Contains(context, "物理") || strings.Contains(strings.ToLower(context), "phys"):
		subjects = []string{"physics"}
	case strings.Contains(context, "英语") || strings.Contains(strings.ToLower(context), "english") || strings.Contains(strings.ToLower(context), "en-q"):
		subjects = []string{"english"}
	}
	switch {
	case strings.Contains(context, "教师版") || strings.Contains(context, "教师"):
		role = "teacher"
	case strings.Contains(context, "答案") || strings.Contains(strings.ToLower(context), "answers"):
		role = "answers"
	case strings.Contains(context, "答题卡"):
		role = "answer_sheet"
	case strings.Contains(context, "讲义") || strings.Contains(context, "导学案"):
		role, docType = "main", "lecture"
	}
	card := map[string]any{
		"role": role, "doc_type": docType, "subjects": subjects,
		"title_text": "2026 高三数学一轮练习",
		"grade":      "grade12",
		"cohort":     2026,
		"evidence": map[string]string{
			"title_text": "2026 高三数学一轮练习",
			"grade":      "高三",
			"cohort":     "2026 高三数学一轮练习",
		},
	}
	encoded, _ := json.Marshal(card)
	return string(encoded)
}

func fakeCatalogGroup(text string) string {
	start := strings.Index(text, "待分组的文件：\n")
	if start < 0 {
		return `{"groups":[]}`
	}
	raw := text[start+len("待分组的文件：\n"):]
	var files []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
		Card struct {
			Subjects []string `json:"subjects"`
		} `json:"card"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&files); err != nil {
		return `{"groups":[]}`
	}
	type group struct {
		Files  []string `json:"files"`
		Reason string   `json:"reason"`
	}
	byKey := map[string]*group{}
	order := []string{}
	for _, file := range files {
		subjects := append([]string{}, file.Card.Subjects...)
		if len(subjects) == 0 {
			subjects = []string{"math"}
		}
		key := path.Dir(file.Path) + "\x00" + strings.Join(subjects, ",")
		if byKey[key] == nil {
			byKey[key] = &group{Reason: "同目录、同学科，按同一份资料处理"}
			order = append(order, key)
		}
		byKey[key].Files = append(byKey[key].Files, file.ID)
	}
	groups := make([]group, 0, len(order))
	for _, key := range order {
		groups = append(groups, *byKey[key])
	}
	encoded, _ := json.Marshal(map[string]any{"groups": groups})
	return string(encoded)
}

func fakeCatalogDecision(text string) string {
	start := strings.Index(text, "本套文件（card 为已校验的阅读记录）：\n")
	if start < 0 {
		return `{}`
	}
	raw := text[start+len("本套文件（card 为已校验的阅读记录）：\n"):]
	var members []struct {
		ID       string `json:"id"`
		FileName string `json:"file_name"`
		Card     struct {
			Role     string   `json:"role"`
			Subjects []string `json:"subjects"`
			Grade    string   `json:"grade"`
			Cohort   int      `json:"cohort"`
		} `json:"card"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&members); err != nil {
		return `{}`
	}
	docType := "topic"
	for _, member := range members {
		if strings.Contains(member.FileName, "讲义") || strings.Contains(member.FileName, "导学案") {
			docType = "lecture"
			break
		}
	}
	subjectSet := map[string]bool{}
	cohort := 0
	files := make([]map[string]any, 0, len(members))
	for _, member := range members {
		for _, subject := range member.Card.Subjects {
			subjectSet[subject] = true
		}
		if cohort == 0 {
			cohort = member.Card.Cohort
		}
		role := member.Card.Role
		if role == "" {
			role = "other"
		}
		files = append(files, map[string]any{"id": member.ID, "role": role})
	}
	subjects := make([]string, 0, len(subjectSet))
	for _, subject := range []string{"chinese", "math", "english", "physics", "chemistry", "biology", "politics", "history", "geography"} {
		if subjectSet[subject] {
			subjects = append(subjects, subject)
		}
	}
	decision := map[string]any{
		"doc_type": docType,
		"subjects": subjects,
		"cohort":   cohort,
		"files":    files,
	}
	if docType == "topic" {
		decision["grade"] = "grade12"
	}
	encoded, _ := json.Marshal(decision)
	return string(encoded)
}

func configureOCRProvider(t *testing.T, env *testEnv, baseURL, kind, token string) string {
	t.Helper()
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/ocr-providers", map[string]any{
		"name": kind + " 测试服务", "kind": kind, "base_url": baseURL, "token": token, "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("ocr provider %d: %s", status, body)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Data.ID == "" {
		t.Fatalf("ocr provider response: %s", body)
	}
	return out.Data.ID
}

func configureCatalogAI(t *testing.T, env *testEnv, fake *fakeAI) string {
	t.Helper()
	ocrID := configureOCRProvider(t, env, fake.server.URL, "mineru", "mineru-token")
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "测试网关", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("catalog AI provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"ocr_provider_id": ocrID, "max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("catalog AI settings %d: %s", status, body)
	}
	return ocrID
}

func (f *fakeAI) classificationSawBody() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodySeen
}

func buildMineruZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	md, _ := zw.Create("full.md")
	_, _ = md.Write([]byte("# 2026 高三数学一轮练习\n\n学生版，函数与导数。"))
	cl, _ := zw.Create("content_list.json")
	_, _ = cl.Write([]byte(`[
		{"type":"text","text":"2026 高三数学一轮练习","text_level":1,"page_idx":0},
		{"type":"text","text":"学生版，函数与导数。","page_idx":0},
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
	ocrID := configureOCRProvider(t, env, fake.server.URL, "mineru", "mineru-token")

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
		"ocr_provider_id": ocrID, "max_pages": 3, "max_images": 2,
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
			Status          string `json:"status"`
			ProviderName    string `json:"provider_name"`
			PageCount       int    `json:"page_count"`
			MetadataOnly    bool   `json:"metadata_only"`
			MarkdownExcerpt string `json:"markdown_excerpt"`
			Card            struct {
				Role     string            `json:"role"`
				Title    string            `json:"title_text"`
				Subjects []string          `json:"subjects"`
				Grade    string            `json:"grade"`
				Cohort   int               `json:"cohort"`
				Evidence map[string]string `json:"evidence"`
			} `json:"card"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &analysis); err != nil {
		t.Fatalf("analysis body: %s", body)
	}
	if analysis.Data.Status != "succeeded" || analysis.Data.Error != "" {
		t.Fatalf("analysis failed: %s", body)
	}
	if analysis.Data.Card.Role != "questions" || analysis.Data.Card.Title == "" ||
		len(analysis.Data.Card.Subjects) != 1 || analysis.Data.Card.Subjects[0] != "math" ||
		analysis.Data.Card.Grade != "grade12" || analysis.Data.Card.Cohort != 2026 {
		t.Fatalf("unexpected verified card: %s", body)
	}
	if analysis.Data.PageCount == 0 || analysis.Data.MetadataOnly || analysis.Data.MarkdownExcerpt == "" ||
		analysis.Data.Card.Evidence["grade"] == "" {
		t.Fatalf("read evidence mismatch: %s", body)
	}
	if analysis.Data.ProviderName != "测试网关" {
		t.Fatalf("provider mismatch: %s", body)
	}
	if !fake.visionSeen {
		t.Fatalf("vision model did not receive image_url parts")
	}
	if !fake.classificationSawBody() {
		t.Fatalf("document body was not sent to the classification model")
	}

	// Grouping and DECIDE run after READ; completion goes straight to ready.
	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("material %d: %s", status, body)
	}
	var automatic struct {
		Data struct {
			Title    string `json:"title"`
			FileName string `json:"file_name"`
			Stage    string `json:"stage"`
			Pack     struct {
				Cohort   int      `json:"cohort"`
				Subjects []string `json:"subject_codes"`
				DocType  string   `json:"doc_type"`
				Grade    string   `json:"grade"`
			} `json:"pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &automatic); err != nil {
		t.Fatalf("automatic apply body: %s", body)
	}
	if automatic.Data.Title != "2026届 专题练习 高三 数学 试题卷" ||
		automatic.Data.FileName != "2026届 专题练习 高三 数学 试题卷.pdf" ||
		automatic.Data.Pack.Cohort != 2026 || automatic.Data.Pack.DocType != "topic" ||
		automatic.Data.Pack.Grade != "grade12" || automatic.Data.Stage != "ready" {
		t.Fatalf("automatic AI metadata not applied: %s", body)
	}

	// A manual edit locks the field. Re-deciding keeps the person's value.
	_, body = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID, nil, nil)
	packID := jsonField(t, body, "pack_id")
	status, body = env.do(t, http.MethodPatch, "/api/v1/packs/"+packID, map[string]any{"cohort": 2025}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"cohort":2025`) {
		t.Fatalf("manual edit %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/"+packID+"/ai/redecide", map[string]any{}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("redecide %d: %s", status, body)
	}
	env.processJobs(t)
	status, body = env.do(t, http.MethodGet, "/api/v1/packs/"+packID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"cohort":2025`) {
		t.Fatalf("re-decide overwrote a locked field %d: %s", status, body)
	}

	// Re-reading queues the file while preserving its already created set.
	status, body = env.do(t, http.MethodPost, "/api/v1/materials/"+materialID+"/analysis", map[string]any{}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("re-enqueue %d: %s", status, body)
	}

	// Reanalysis must not wait for another SQLite connection inside a transaction.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.base+"/api/v1/materials/"+materialID+"/analysis", strings.NewReader(`{}`))
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

func TestQuestionSearchIndexAndPages(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)
	ocrID := configureOCRProvider(t, env, fake.server.URL, "mineru", "mineru-token")
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "切题模型", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"ocr_provider_id": ocrID, "max_pages": 10, "max_images": 0,
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
				QuestionID string `json:"question_id"`
				PageNo     int    `json:"page_no"`
				Text       string `json:"text"`
				Snippet    string `json:"snippet"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.Data.Total == 0 || len(result.Data.Items) == 0 {
		t.Fatalf("search body: %s", body)
	}
	if result.Data.Items[0].PageNo != 1 {
		t.Fatalf("expected page 1 first, got %+v", result.Data.Items)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/search/questions/"+result.Data.Items[0].QuestionID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"highlights":[]`) {
		t.Fatalf("question detail should have no query highlights: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "求导数"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"page_no\":2") {
		t.Fatalf("page-2 search %d: %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "x²+2x"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":1`) || !strings.Contains(string(body), `"page_no":1`) {
		t.Fatalf("equivalent formula search %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/search/questions", map[string]any{"query": "x²-2x"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":0`) {
		t.Fatalf("different formula operator should not match %d: %s", status, body)
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
