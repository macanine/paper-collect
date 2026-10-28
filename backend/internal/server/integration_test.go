package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/server"
	"github.com/nic/paper-collect/backend/internal/service"
	"github.com/nic/paper-collect/backend/internal/storage"
)

const (
	adminUser     = "admin"
	adminPassword = "correct-horse-battery"
)

type testEnv struct {
	base   string
	svc    *service.Service
	client *http.Client
	token  string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := blob.NewStore(dir)
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	box, err := cryptobox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	signer, err := auth.NewSigner(strings.Repeat("s", 32), time.Now)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	limiter := auth.NewLimiter(time.Now, 50, time.Minute, time.Minute)
	svc, err := service.New(service.Options{
		DB: conn, Blobs: store, Box: box, Signer: signer, Limiter: limiter,
		Factory: storage.New, Logf: t.Logf,
	})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	ctx := context.Background()
	if err := svc.Bootstrap(ctx, adminUser, adminPassword); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv, err := server.New(server.Options{Service: svc, WebDist: filepath.Join(dir, "missing")})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	env := &testEnv{base: ts.URL, svc: svc, client: ts.Client()}
	env.token = env.login(t)
	return env
}

func (e *testEnv) do(t *testing.T, method, path string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.base+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

func (e *testEnv) login(t *testing.T) string {
	t.Helper()
	status, body := e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": adminUser, "password": adminPassword,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("login status %d: %s", status, body)
	}
	var envelope struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.AccessToken == "" {
		t.Fatalf("login body: %s", body)
	}
	return envelope.Data.AccessToken
}

func (e *testEnv) upload(t *testing.T, title string, files map[string][]byte) (uploadID string, entries map[string]string) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	manifest := map[string]any{
		"title": title, "collection_kind": "folder", "subject_codes": []string{"math"},
	}
	list := make([]map[string]any, 0, len(names))
	for _, name := range names {
		list = append(list, map[string]any{"relative_path": name, "size_bytes": len(files[name])})
	}
	manifest["entries"] = list
	status, body := e.do(t, http.MethodPost, "/api/v1/uploads", manifest, nil)
	if status != http.StatusCreated {
		t.Fatalf("create upload %d: %s", status, body)
	}
	var created struct {
		Data struct {
			ID      string `json:"id"`
			Entries []struct {
				ID           string `json:"id"`
				RelativePath string `json:"relative_path"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("upload body: %s", body)
	}
	entries = map[string]string{}
	for _, entry := range created.Data.Entries {
		entries[entry.RelativePath] = entry.ID
	}
	for _, name := range names {
		req, _ := http.NewRequest(http.MethodPut, e.base+"/api/v1/uploads/"+created.Data.ID+"/files/"+entries[name], bytes.NewReader(files[name]))
		req.Header.Set("Authorization", "Bearer "+e.token)
		req.Header.Set("Content-Type", "application/octet-stream")
		res, err := e.client.Do(req)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("put %s status %d: %s", name, res.StatusCode, data)
		}
	}
	return created.Data.ID, entries
}

func minimalPDF() []byte {
	return []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<<>>\n%%EOF\n")
}

func zipWith(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func (e *testEnv) finalizeAndWait(t *testing.T, uploadID string) {
	t.Helper()
	status, body := e.do(t, http.MethodPost, "/api/v1/uploads/"+uploadID+"/finalize", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("finalize status %d: %s", status, body)
	}
	for i := 0; i < 100; i++ {
		if e.svc.ProcessAvailable(context.Background(), 100) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("jobs did not settle")
}

func TestUploadArchiveAndReplicateEndToEnd(t *testing.T) {
	env := newEnv(t)
	pdf := minimalPDF()
	packageZip := zipWith(t, map[string][]byte{
		"学生版.pdf":    pdf,
		"答案/教师版.pdf": pdf,
	})
	uploadID, _ := env.upload(t, "2026 高三数学一套", map[string][]byte{
		"试卷/学生版.pdf": pdf,
		"整套资料.zip":   packageZip,
	})
	env.finalizeAndWait(t, uploadID)

	status, body := env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=50", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list materials %d: %s", status, body)
	}
	var list struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				Title      string `json:"title"`
				Role       string `json:"role"`
				Status     string `json:"status"`
				FileName   string `json:"file_name"`
				SourcePath string `json:"source_path"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list body: %s", body)
	}
	if list.Data.Total < 3 {
		t.Fatalf("expected parent pdf, zip and extracted members, got %d: %s", list.Data.Total, body)
	}
	foundZipMember := false
	for _, item := range list.Data.Items {
		if item.Status != "archived" {
			t.Fatalf("material %s status %s, want archived (replica not ready)", item.FileName, item.Status)
		}
		if strings.Contains(item.SourcePath, "答案/教师版.pdf") {
			foundZipMember = true
			if item.Role != "teacher_edition" {
				t.Fatalf("expected role suggestion teacher_edition, got %s", item.Role)
			}
		}
	}
	if !foundZipMember {
		t.Fatalf("zip member not archived: %s", body)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"zip\"") {
		t.Fatalf("expected a zip collection: %d %s", status, body)
	}
}

func TestMaliciousZipFailsJobWithoutPublishing(t *testing.T) {
	env := newEnv(t)
	evil := zipWith(t, map[string][]byte{"../evil.pdf": minimalPDF()})
	uploadID, _ := env.upload(t, "恶意压缩包", map[string][]byte{"evil.zip": evil})
	env.finalizeAndWait(t, uploadID)

	status, body := env.do(t, http.MethodGet, "/api/v1/jobs?status=failed", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("jobs status %d: %s", status, body)
	}
	if !strings.Contains(string(body), "\"failed\"") {
		t.Fatalf("expected failed job: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=20&q=evil", nil, nil)
	if status != http.StatusOK || strings.Contains(string(body), "\"total\":1") {
		t.Fatalf("path traversal member must not be published: %s", body)
	}
}

func TestAgentDoubleCredentialAndRevocation(t *testing.T) {
	env := newEnv(t)

	status, body := env.do(t, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("browser JWT must not use MCP: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/api-keys", map[string]any{
		"name": "test agent", "scopes": []string{"materials:read", "collections:read", "jobs:read"},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create key %d: %s", status, body)
	}
	var keyBody struct {
		Data struct {
			Key struct {
				ID string `json:"id"`
			} `json:"key"`
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &keyBody); err != nil || keyBody.Data.Secret == "" {
		t.Fatalf("key body: %s", body)
	}
	apiKey := keyBody.Data.Secret

	status, body = env.do(t, http.MethodPost, "/api/v1/auth/agent-token", nil, map[string]string{"X-API-Key": apiKey})
	if status != http.StatusOK {
		t.Fatalf("agent token %d: %s", status, body)
	}
	var agentBody struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &agentBody); err != nil || agentBody.Data.AccessToken == "" {
		t.Fatalf("agent token body: %s", body)
	}

	rpc := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
	status, body = env.do(t, http.MethodPost, "/mcp", rpc, map[string]string{
		"Authorization": "Bearer " + agentBody.Data.AccessToken,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("missing X-API-Key must be rejected: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/mcp", rpc, map[string]string{
		"Authorization": "Bearer " + agentBody.Data.AccessToken,
		"X-API-Key":     apiKey,
	})
	if status != http.StatusOK || !strings.Contains(string(body), "materials_search") {
		t.Fatalf("MCP tools/list failed: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodDelete, "/api/v1/api-keys/"+keyBody.Data.Key.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("revoke key %d: %s", status, body)
	}
	status, _ = env.do(t, http.MethodPost, "/mcp", rpc, map[string]string{
		"Authorization": "Bearer " + agentBody.Data.AccessToken,
		"X-API-Key":     apiKey,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key must be rejected immediately, got %d", status)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	env := newEnv(t)
	env.token = ""
	for _, path := range []string{"/api/v1/materials", "/api/v1/storage/providers", "/api/v1/jobs"} {
		status, _ := env.do(t, http.MethodGet, path, nil, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401", path, status)
		}
	}
}

func TestDeleteCollectionsAndMaterials(t *testing.T) {
	env := newEnv(t)
	zipBytes := zipWith(t, map[string][]byte{"学生版.pdf": minimalPDF(), "答案.pdf": minimalPDF()})
	uploadID, _ := env.upload(t, "待删除合集", map[string][]byte{"资料.zip": zipBytes})
	env.finalizeAndWait(t, uploadID)

	status, body := env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=20&roots=true", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("roots %d: %s", status, body)
	}
	var roots struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &roots); err != nil || len(roots.Data.Items) != 1 {
		t.Fatalf("roots body: %s", body)
	}
	rootID := roots.Data.Items[0].ID

	// Deleting the root collection cascades into the ZIP child but keeps materials.
	status, body = env.do(t, http.MethodDelete, "/api/v1/collections/"+rootID+"?cascade=true", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete collection %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":3") {
		t.Fatalf("materials must survive collection deletion: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":0") {
		t.Fatalf("collections not deleted: %s", body)
	}

	// Find the ZIP container and require explicit cascade for its children.
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=20", nil, nil)
	var list struct {
		Data struct {
			Items []struct {
				ID       string `json:"id"`
				FileName string `json:"file_name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("materials body: %s", body)
	}
	zipID := ""
	for _, item := range list.Data.Items {
		if strings.HasSuffix(item.FileName, ".zip") {
			zipID = item.ID
		}
	}
	if zipID == "" {
		t.Fatalf("zip material not found: %s", body)
	}
	status, body = env.do(t, http.MethodDelete, "/api/v1/materials/"+zipID, nil, nil)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 without cascade, got %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodDelete, "/api/v1/materials/"+zipID+"?cascade=true", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete zip %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "\"total\":0") {
		t.Fatalf("cascade did not remove children: %s", body)
	}
}

func TestMaterialRoleSyncsCollectionItems(t *testing.T) {
	env := newEnv(t)
	uploadID, _ := env.upload(t, "角色同步测试", map[string][]byte{"学生版.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)

	status, body := env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("materials %d: %s", status, body)
	}
	var list struct {
		Data struct {
			Items []struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Data.Items) != 1 {
		t.Fatalf("materials body: %s", body)
	}
	materialID := list.Data.Items[0].ID

	status, body = env.do(t, http.MethodPatch, "/api/v1/materials/"+materialID, map[string]any{"role": "questions_answers"}, nil)
	if status != http.StatusOK {
		t.Fatalf("patch role %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10&roots=true", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("collections %d: %s", status, body)
	}
	var roots struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &roots); err != nil || len(roots.Data.Items) != 1 {
		t.Fatalf("roots body: %s", body)
	}
	collectionID := roots.Data.Items[0].ID
	status, body = env.do(t, http.MethodGet, "/api/v1/collections/"+collectionID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"role":"questions_answers"`) {
		t.Fatalf("item role not synced: %s", body)
	}

	// Item role is always derived from the material; PUT cannot override it.
	status, body = env.do(t, http.MethodPut, "/api/v1/collections/"+collectionID+"/items", map[string]any{
		"items": []map[string]any{{
			"material_id": materialID, "position": 0, "role": "answer", "relative_path": "学生版.pdf",
		}},
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put items %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections/"+collectionID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"role":"questions_answers"`) {
		t.Fatalf("item role must follow material, got: %s", body)
	}
	status, body = env.do(t, http.MethodPatch, "/api/v1/materials/"+materialID, map[string]any{"role": "teacher_edition"}, nil)
	if status != http.StatusOK {
		t.Fatalf("patch role again %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections/"+collectionID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"role":"teacher_edition"`) {
		t.Fatalf("role change did not follow material: %s", body)
	}

	// Title and filename are one name: editing the title rewrites the filename.
	status, body = env.do(t, http.MethodPatch, "/api/v1/materials/"+materialID, map[string]any{"title": "改名后的资料"}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"file_name":"改名后的资料.pdf"`) {
		t.Fatalf("filename not synced with title: %s", body)
	}
}
