package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/server"
	"github.com/nic/paper-collect/backend/internal/service"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

const (
	adminUser     = "admin"
	adminPassword = "correct-horse-battery"
)

type testEnv struct {
	base    string
	svc     *service.Service
	client  *http.Client
	token   string
	dataDir string
	db      *gorm.DB
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	return newEnvWith(t, 0, nil)
}

// newEnvWith builds a master. kinds nil runs every job kind on the master;
// an empty slice leaves replicate, analyze and index to worker nodes.
func newEnvWith(t *testing.T, ttl time.Duration, kinds []string) *testEnv {
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
		Factory: storage.New, Logf: t.Logf, PullCacheTTL: ttl,
		Worker: service.WorkerSettings{Kinds: kinds},
	})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	ctx := context.Background()
	if err := svc.Bootstrap(ctx, adminUser, adminPassword); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.RegisterMaster(ctx, "主端", "test"); err != nil {
		t.Fatalf("register master: %v", err)
	}
	srv, err := server.New(server.Options{Service: svc, WebDist: filepath.Join(dir, "missing")})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	env := &testEnv{base: ts.URL, svc: svc, client: ts.Client(), dataDir: dir, db: conn}
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
		"title": title, "hints": map[string]any{"subject_codes": []string{"math"}},
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

// syncAllToLocalDrive adds a local drive and points the fallback rule at it,
// going through the same preview-then-save flow as the storage page.
func (e *testEnv) syncAllToLocalDrive(t *testing.T) string {
	t.Helper()
	return e.syncAllToDrive(t, map[string]any{"name": "本地目录", "kind": "local"})
}

func (e *testEnv) syncAllToDrive(t *testing.T, input map[string]any) string {
	t.Helper()
	status, body := e.do(t, http.MethodPost, "/api/v1/storage/drives", input, nil)
	if status != http.StatusCreated {
		t.Fatalf("create drive %d: %s", status, body)
	}
	var drive struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &drive); err != nil {
		t.Fatal(err)
	}
	status, body = e.do(t, http.MethodGet, "/api/v1/storage/rules", nil, nil)
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &listed) != nil || len(listed.Data) != 1 {
		t.Fatalf("list rules %d: %s", status, body)
	}
	rules := listed.Data
	rules[0]["targets"] = []any{map[string]any{"drive_id": drive.Data.ID, "path_template": model.DefaultPathTemplate, "required": true}}
	status, body = e.do(t, http.MethodPost, "/api/v1/storage/rules/preview", map[string]any{"rules": rules}, nil)
	var preview struct {
		Data struct {
			PreviewToken string `json:"preview_token"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &preview) != nil || preview.Data.PreviewToken == "" {
		t.Fatalf("preview rules %d: %s", status, body)
	}
	status, body = e.do(t, http.MethodPut, "/api/v1/storage/rules", map[string]any{"rules": rules, "preview_token": preview.Data.PreviewToken}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("save rules %d: %s", status, body)
	}
	return drive.Data.ID
}

func TestPullCacheRejectsCorruptCloudReplicaWithoutPublishing(t *testing.T) {
	env := newEnv(t)
	configureCatalogAI(t, env, newFakeAI(t))
	env.syncAllToLocalDrive(t)
	pdf := minimalPDF()
	uploadID, _ := env.upload(t, "损坏副本测试", map[string][]byte{"学生版.pdf": pdf})
	env.finalizeAndWait(t, uploadID)

	var material model.Material
	if err := env.db.First(&material).Error; err != nil {
		t.Fatal(err)
	}
	var blob model.Blob
	if err := env.db.Where("id = ?", material.BlobID).First(&blob).Error; err != nil {
		t.Fatal(err)
	}
	var replica model.Replica
	if err := env.db.Where("blob_id = ?", material.BlobID).First(&replica).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(env.dataDir, "blobs", blob.SHA256[:2], blob.SHA256)); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), pdf...)
	tampered[len(tampered)-1] ^= 1
	remoteKey := replica.RemoteID
	if remoteKey == "" {
		remoteKey = replica.RemotePath
	}
	replicaPath := filepath.Join(env.dataDir, "drives", replica.DriveID, filepath.FromSlash(remoteKey))
	if err := os.WriteFile(replicaPath, tampered, 0o600); err != nil {
		t.Fatalf("tamper local replica: %v", err)
	}

	status, body := env.do(t, http.MethodPost, "/api/v1/materials/"+material.ID+"/pull-cache", nil, nil)
	if status != http.StatusServiceUnavailable || !strings.Contains(string(body), `"code":"service_unavailable"`) {
		t.Fatalf("corrupt replica accepted %d: %s", status, body)
	}
	cachePath := filepath.Join(env.dataDir, "tmp", "material-pulls", material.SHA256)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("corrupt replica left a downloadable cache: %v", err)
	}
	if staged, err := filepath.Glob(filepath.Join(env.dataDir, "tmp", "material-pulls", ".stage-*")); err != nil || len(staged) != 0 {
		t.Fatalf("corrupt replica left staging files: %v %v", staged, err)
	}
	if _, err := os.Stat(filepath.Join(env.dataDir, "blobs", blob.SHA256[:2], blob.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("corrupt pull recreated permanent CAS: %v", err)
	}
}

func TestAgentBearerAndRevocation(t *testing.T) {
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
				ID        string     `json:"id"`
				ExpiresAt *time.Time `json:"expires_at"`
			} `json:"key"`
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &keyBody); err != nil || !strings.HasPrefix(keyBody.Data.Secret, "sk-") || keyBody.Data.Key.ExpiresAt != nil {
		t.Fatal("key should use the new format and be non-expiring by default")
	}
	apiKey := keyBody.Data.Secret

	rpc := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials", nil, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status != http.StatusOK {
		t.Fatalf("API key must authenticate REST directly: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodPost, "/mcp", rpc, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status != http.StatusOK || !strings.Contains(string(body), "materials_search") {
		t.Fatalf("MCP tools/list failed: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodDelete, "/api/v1/api-keys/"+keyBody.Data.Key.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("revoke key %d: %s", status, body)
	}
	status, _ = env.do(t, http.MethodPost, "/mcp", rpc, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key must be rejected immediately, got %d", status)
	}
	status, _ = env.do(t, http.MethodGet, "/api/v1/materials", nil, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key must not access REST, got %d", status)
	}
}

func TestDirectKeyEnforcesScopesAndMCPToolVisibility(t *testing.T) {
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "read-only", "scopes": []string{"materials:read"}}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create key: %d", status)
	}
	var created struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Data.Secret == "" {
		t.Fatal("missing key")
	}
	headers := map[string]string{"Authorization": "Bearer " + created.Data.Secret}
	status, body = env.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25"}}, headers)
	if status != http.StatusOK || !strings.Contains(string(body), `"instructions"`) {
		t.Fatalf("MCP initialize: %d %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, headers)
	if status != http.StatusOK || !strings.Contains(string(body), "materials_search") || !strings.Contains(string(body), `"readOnlyHint":true`) || strings.Contains(string(body), "collections_list") || strings.Contains(string(body), "jobs_get") {
		t.Fatalf("unexpected visible tools: %d %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "collections_list", "arguments": map[string]any{}}}, headers)
	if status != http.StatusOK || !strings.Contains(string(body), `"isError":true`) {
		t.Fatalf("tool scope not enforced: %d %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "method": "ping"}, headers)
	if status != http.StatusAccepted || len(body) != 0 {
		t.Fatalf("notification returned a response: %d", status)
	}
	for _, path := range []string{"/api/v1/collections", "/api/v1/api-keys", "/api/v1/users"} {
		status, _ = env.do(t, http.MethodGet, path, nil, headers)
		if status != http.StatusForbidden {
			t.Fatalf("key may not read %s: %d", path, status)
		}
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/api-keys", nil, nil)
	if status != http.StatusOK || strings.Contains(string(body), created.Data.Secret) {
		t.Fatal("key list disclosed a secret")
	}
	status, _ = env.do(t, http.MethodGet, "/api/v1/materials", nil, map[string]string{"Authorization": "Bearer sk-invalid"})
	if status != http.StatusUnauthorized {
		t.Fatalf("invalid key accepted: %d", status)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	env := newEnv(t)
	env.token = ""
	for _, path := range []string{"/api/v1/materials", "/api/v1/storage/drives", "/api/v1/jobs"} {
		status, _ := env.do(t, http.MethodGet, path, nil, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401", path, status)
		}
	}
}

// pdfWith returns a minimal PDF whose bytes (and so SHA-256) depend on text.
func pdfWith(text string) []byte {
	return []byte("%PDF-1.4\n% " + text + "\n1 0 obj<</Type/Catalog>>endobj\ntrailer<<>>\n%%EOF\n")
}

type packListBody struct {
	Data struct {
		Total int `json:"total"`
		Items []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			HasAnswers bool   `json:"has_answers"`
			Members    []struct {
				ID       string `json:"id"`
				Part     string `json:"part"`
				FileName string `json:"file_name"`
			} `json:"members"`
		} `json:"items"`
	} `json:"data"`
}

func (e *testEnv) listPacks(t *testing.T, query string) packListBody {
	t.Helper()
	status, body := e.do(t, http.MethodGet, "/api/v1/packs?page=1&page_size=50"+query, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list packs %d: %s", status, body)
	}
	var out packListBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("packs body: %s", body)
	}
	return out
}

func jsonField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("body: %s", body)
	}
	v, _ := envelope.Data[field].(string)
	return v
}

type gatedReader struct {
	data    []byte
	ready   chan<- struct{}
	release <-chan struct{}
	read    bool
}

func (r *gatedReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	r.ready <- struct{}{}
	<-r.release
	return copy(p, r.data), io.EOF
}

func TestConcurrentZIPUploadsDoNotReplaceWinningContent(t *testing.T) {
	env := newEnv(t)
	makePackage := func(comment string) []byte {
		var out bytes.Buffer
		w := zip.NewWriter(&out)
		if err := w.SetComment(comment); err != nil {
			t.Fatal(err)
		}
		file, err := w.Create("试卷.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(minimalPDF()); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	first, second := makePackage("A"), makePackage("B")
	if len(first) != len(second) || bytes.Equal(first, second) {
		t.Fatal("test ZIPs must have different content and the same length")
	}
	principal, err := env.svc.Authenticate(t.Context(), env.token)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := env.svc.CreateUpload(t.Context(), principal, service.UploadInput{
		Title: "竞争上传", Entries: []service.UploadEntryInput{{RelativePath: "资料.zip", SizeBytes: int64(len(first))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ready, release := make(chan struct{}, 2), make(chan struct{})
	results := make(chan error, 2)
	for _, data := range [][]byte{first, second} {
		go func(content []byte) {
			_, err := env.svc.PutEntry(t.Context(), principal, upload.Upload.ID, upload.Entries[0].ID, &gatedReader{data: content, ready: ready, release: release}, int64(len(content)))
			results <- err
		}(data)
	}
	<-ready
	<-ready
	close(release)
	ok := 0
	for range 2 {
		if <-results == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one different ZIP must win, got %d", ok)
	}
	view, err := env.svc.GetUpload(t.Context(), principal, upload.Upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(env.dataDir, "tmp", "upload-archives", "*.zip"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected exactly one winning staged ZIP: %v", err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if fmt.Sprintf("%x", sum) != view.Entries[0].SHA256 {
		t.Fatal("staged ZIP differs from the received content")
	}
	env.finalizeAndWait(t, upload.Upload.ID)
	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("successful ingest should clean staged ZIP: %v", err)
	}
}
