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
	return newEnvWithPullCacheTTL(t, 0)
}

func newEnvWithPullCacheTTL(t *testing.T, ttl time.Duration) *testEnv {
	t.Helper()
	return newEnvWith(t, ttl, nil)
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

func TestUploadArchiveAndReplicateEndToEnd(t *testing.T) {
	env := newEnv(t)
	pdf := pdfWith("student")
	packageZip := zipWith(t, map[string][]byte{
		"学生版.pdf":    pdf,
		"答案/教师版.pdf": pdfWith("teacher"),
		"答案/答案.pdf":  pdfWith("answers"),
	})
	uploadID, _ := env.upload(t, "2026 高三数学一套", map[string][]byte{
		"试卷/学生版.pdf": pdf,
		"整套资料.zip":   packageZip,
	})
	zipHash := sha256.Sum256(packageZip)
	zipSHA := fmt.Sprintf("%x", zipHash)
	zipCAS := filepath.Join(env.dataDir, "blobs", zipSHA[:2], zipSHA)
	if _, err := os.Stat(zipCAS); !os.IsNotExist(err) {
		t.Fatalf("new ZIP must not be stored in CAS: %v", err)
	}
	staged, err := filepath.Glob(filepath.Join(env.dataDir, "tmp", "upload-archives", "*.zip"))
	if err != nil || len(staged) != 1 {
		t.Fatalf("uploaded ZIP was not staged: %v", err)
	}
	env.finalizeAndWait(t, uploadID)
	if _, err := os.Stat(staged[0]); !os.IsNotExist(err) {
		t.Fatalf("successful ingest must remove temporary ZIP: %v", err)
	}

	status, body := env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=50", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list materials %d: %s", status, body)
	}
	var list struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				ID           string `json:"id"`
				Part         string `json:"part"`
				Status       string `json:"status"`
				FileName     string `json:"file_name"`
				OriginalName string `json:"original_name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list body: %s", body)
	}
	// The same student copy arrived twice (folder and ZIP): one file, two sources.
	if list.Data.Total != 3 {
		t.Fatalf("expected three unique documents, got %d: %s", list.Data.Total, body)
	}
	foundZipMember := false
	for _, item := range list.Data.Items {
		if strings.HasSuffix(item.FileName, ".zip") {
			t.Fatalf("ZIP original should not become a material: %s", body)
		}
		if item.Status != "archived" {
			t.Fatalf("material %s status %s, want archived (replica not ready)", item.FileName, item.Status)
		}
		if item.OriginalName == "教师版.pdf" {
			foundZipMember = true
			if item.Part != "teacher" {
				t.Fatalf("expected part suggestion teacher, got %s", item.Part)
			}
		}
	}
	if !foundZipMember {
		t.Fatalf("zip member not archived: %s", body)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=20", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":0`) {
		t.Fatalf("uploads must not create topics: %d %s", status, body)
	}
	result := env.uploadResult(t, uploadID)
	if result.Data.NewCount != 3 || result.Data.DuplicateCount != 1 || len(result.Data.Results) != 4 {
		t.Fatalf("upload results must list every arrival: %+v", result.Data)
	}
	// 教师版 and 答案 are named only by their part, so the folder they share
	// makes them one set; the student copy sits elsewhere.
	if got := env.listPacks(t, ""); got.Data.Total != 2 {
		t.Fatalf("expected two packs: %+v", got.Data)
	}
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

func TestPullCloudReplicaIntoTemporaryCache(t *testing.T) {
	env := newEnvWithPullCacheTTL(t, 2*time.Hour)
	env.syncAllToLocalDrive(t)
	pdf := minimalPDF()
	uploadID, _ := env.upload(t, "临时拉取测试", map[string][]byte{"学生版.pdf": pdf})
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
	if replica.Status != model.ReplicaReady {
		t.Fatalf("replica status = %s, want ready", replica.Status)
	}

	casPath := filepath.Join(env.dataDir, "blobs", blob.SHA256[:2], blob.SHA256)
	if err := os.Remove(casPath); err != nil {
		t.Fatalf("remove CAS copy: %v", err)
	}

	status, body := env.do(t, http.MethodPost, "/api/v1/materials/"+material.ID+"/pull-cache", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("pull cache %d: %s", status, body)
	}
	var pulled struct {
		Data struct {
			DownloadURL string    `json:"download_url"`
			SizeBytes   int64     `json:"size_bytes"`
			SHA256      string    `json:"sha256"`
			ExpiresAt   time.Time `json:"expires_at"`
			ExpiresIn   int64     `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &pulled); err != nil {
		t.Fatalf("pull body: %s", body)
	}
	wantURL := "/api/v1/materials/" + material.ID + "/pull-cache"
	if pulled.Data.DownloadURL != wantURL || pulled.Data.SizeBytes != int64(len(pdf)) ||
		pulled.Data.SHA256 != material.SHA256 || pulled.Data.ExpiresAt.IsZero() || pulled.Data.ExpiresIn < 1 {
		t.Fatalf("unexpected pull metadata: %s", body)
	}
	if _, err := os.Stat(casPath); !os.IsNotExist(err) {
		t.Fatalf("pull cache must not recreate permanent CAS: %v", err)
	}
	cachePath := filepath.Join(env.dataDir, "tmp", "material-pulls", material.SHA256)
	if info, err := os.Stat(cachePath); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("temporary cache missing: %v", err)
	}

	status, body = env.do(t, http.MethodGet, wantURL, nil, nil)
	if status != http.StatusOK || !bytes.Equal(body, pdf) {
		t.Fatalf("download temporary cache %d: %q", status, body)
	}

	stillValid := time.Now().Add(-time.Hour)
	if err := os.Chtimes(cachePath, stillValid, stillValid); err != nil {
		t.Fatal(err)
	}
	status, body = env.do(t, http.MethodGet, wantURL, nil, nil)
	if status != http.StatusOK || !bytes.Equal(body, pdf) {
		t.Fatalf("configured TTL expired too early %d: %q", status, body)
	}

	expired := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(cachePath, expired, expired); err != nil {
		t.Fatal(err)
	}
	status, body = env.do(t, http.MethodGet, wantURL, nil, nil)
	if status != http.StatusGone || !strings.Contains(string(body), `"code":"gone"`) {
		t.Fatalf("expired temporary cache %d: %s", status, body)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("expired temporary cache was not removed: %v", err)
	}
}

func TestPullCacheRejectsCorruptCloudReplicaWithoutPublishing(t *testing.T) {
	env := newEnv(t)
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
	staged, err := filepath.Glob(filepath.Join(env.dataDir, "tmp", "upload-archives", "*.zip"))
	if err != nil || len(staged) != 1 {
		t.Fatalf("failed upload must remain available for retry: %v", err)
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
	status, _ = env.do(t, http.MethodPost, "/api/v1/auth/agent-token", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("key exchange endpoint should no longer exist: %d", status)
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

func TestDedupPairingTrashAndTopics(t *testing.T) {
	env := newEnv(t)
	first := map[string][]byte{
		"2025联考/数学试题.pdf": pdfWith("math-q"),
		"2025联考/数学答案.pdf": pdfWith("math-a"),
		"其他/物理讲义.pdf":     pdfWith("physics"),
	}
	uploadID, _ := env.upload(t, "第一批", first)
	env.finalizeAndWait(t, uploadID)

	packs := env.listPacks(t, "")
	if packs.Data.Total != 2 {
		t.Fatalf("siblings must pair into one pack: %+v", packs.Data)
	}
	var mathPack string
	for _, p := range packs.Data.Items {
		if len(p.Members) == 2 {
			mathPack = p.ID
			if !p.HasAnswers {
				t.Fatalf("paired pack must report answers: %+v", p)
			}
			for _, m := range p.Members {
				if !strings.Contains(m.FileName, "试题卷") && !strings.Contains(m.FileName, "答案解析") {
					t.Fatalf("member file names must follow the canonical part name: %+v", p.Members)
				}
			}
		}
	}
	if mathPack == "" {
		t.Fatalf("math pack not found: %+v", packs.Data)
	}

	// Re-uploading the same content only adds sources.
	second := map[string][]byte{
		"转发/数学试题(1).pdf": pdfWith("math-q"),
		"转发/化学周测.pdf":    pdfWith("chem"),
	}
	uploadID, _ = env.upload(t, "第二批", second)
	env.finalizeAndWait(t, uploadID)
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+uploadID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"new_count":1`) || !strings.Contains(string(body), `"duplicate_count":1`) {
		t.Fatalf("upload must report one new and one duplicate file: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=50", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":4`) {
		t.Fatalf("duplicates must not create files: %s", body)
	}

	// Topics keep packs when deleted.
	status, body = env.do(t, http.MethodPost, "/api/v1/collections", map[string]any{"title": "导数专题", "pack_ids": []string{mathPack}}, nil)
	if status != http.StatusCreated || !strings.Contains(string(body), `"pack_count":1`) {
		t.Fatalf("create topic %d: %s", status, body)
	}
	if got := env.listPacks(t, "&collection_id="+jsonField(t, body, "id")); got.Data.Total != 1 {
		t.Fatalf("topic filter: %+v", got.Data)
	}
	status, body = env.do(t, http.MethodDelete, "/api/v1/collections/"+jsonField(t, body, "id"), nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete topic %d: %s", status, body)
	}

	// Trash, restore, purge.
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/trash", map[string]any{"ids": []string{mathPack}}, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"count":1`) {
		t.Fatalf("trash %d: %s", status, body)
	}
	if got := env.listPacks(t, ""); got.Data.Total != 2 {
		t.Fatalf("trashed pack must leave the library: %+v", got.Data)
	}
	if got := env.listPacks(t, "&trashed=true"); got.Data.Total != 1 {
		t.Fatalf("trashed pack must be listed in the bin: %+v", got.Data)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/purge", map[string]any{"ids": []string{packs.Data.Items[0].ID, packs.Data.Items[1].ID}}, nil)
	if status != http.StatusConflict {
		t.Fatalf("purging live packs must be refused: %d %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/restore", map[string]any{"ids": []string{mathPack}}, nil)
	if status != http.StatusOK || env.listPacks(t, "").Data.Total != 3 {
		t.Fatalf("restore %d: %s", status, body)
	}
	env.do(t, http.MethodPost, "/api/v1/packs/trash", map[string]any{"ids": []string{mathPack}}, nil)
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/purge", map[string]any{"ids": []string{mathPack}}, nil)
	if status != http.StatusOK {
		t.Fatalf("purge %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=50", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":2`) {
		t.Fatalf("purge must remove the pack files: %s", body)
	}
	if got := env.listPacks(t, "&trashed=true"); got.Data.Total != 0 {
		t.Fatalf("bin must be empty: %+v", got.Data)
	}
}

func TestSplitMergeAndBulkEditPacks(t *testing.T) {
	env := newEnv(t)
	uploadID, _ := env.upload(t, "配套", map[string][]byte{
		"卷/英语试题.pdf": pdfWith("en-q"),
		"卷/英语答案.pdf": pdfWith("en-a"),
	})
	env.finalizeAndWait(t, uploadID)
	packs := env.listPacks(t, "")
	if packs.Data.Total != 1 || len(packs.Data.Items[0].Members) != 2 {
		t.Fatalf("expected one pair: %+v", packs.Data)
	}
	pack := packs.Data.Items[0]
	answer := pack.Members[1].ID
	status, body := env.do(t, http.MethodPatch, "/api/v1/materials/"+answer, map[string]any{"new_pack": true}, nil)
	if status != http.StatusOK {
		t.Fatalf("split %d: %s", status, body)
	}
	if got := env.listPacks(t, ""); got.Data.Total != 2 {
		t.Fatalf("split must create a pack: %+v", got.Data)
	}
	newPack := jsonField(t, body, "pack_id")
	status, body = env.do(t, http.MethodPost, "/api/v1/collections", map[string]any{
		"title": "合并专题", "pack_ids": []string{newPack},
	}, nil)
	collectionID := jsonField(t, body, "id")
	if status != http.StatusCreated || collectionID == "" {
		t.Fatalf("create source topic %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/"+pack.ID+"/merge", map[string]any{"source_ids": []string{newPack}}, nil)
	if status != http.StatusOK || env.listPacks(t, "").Data.Total != 1 {
		t.Fatalf("merge %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections/"+collectionID, nil, nil)
	var topic struct {
		Data struct {
			PackCount int `json:"pack_count"`
			Packs     []struct {
				ID string `json:"id"`
			} `json:"packs"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &topic) != nil ||
		topic.Data.PackCount != 1 || len(topic.Data.Packs) != 1 || topic.Data.Packs[0].ID != pack.ID {
		t.Fatalf("merged pack must retain source topics %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/packs/bulk", map[string]any{
		"ids": []string{pack.ID}, "patch": map[string]any{"doc_type": "monthly", "grade": "grade11", "cohort": 2026, "subject_codes": []string{"english"}, "exam_date": "2025-03"},
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("bulk %d: %s", status, body)
	}
	got := env.listPacks(t, "&doc_type=monthly&grade=grade11&cohort=2026&subject=english")
	if got.Data.Total != 1 || got.Data.Items[0].Title != "2026届 3月月考 高二 英语" {
		t.Fatalf("bulk edit must rename the pack: %+v", got.Data)
	}
	if missing := env.listPacks(t, "&missing=answers"); missing.Data.Total != 0 {
		t.Fatalf("complete pack must not be listed as missing answers: %+v", missing.Data)
	}
	status, _ = env.do(t, http.MethodGet, "/api/v1/packs/"+pack.ID+"/download", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("pack download %d", status)
	}
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

func TestZIPReportsUnsupportedMembers(t *testing.T) {
	env := newEnv(t)
	packageZip := zipWith(t, map[string][]byte{"资料.pdf": minimalPDF(), "说明.txt": []byte("unsupported")})
	id, _ := env.upload(t, "压缩包", map[string][]byte{"资料.zip": packageZip})
	env.finalizeAndWait(t, id)
	status, body := env.do(t, http.MethodGet, "/api/v1/jobs?kind=ingest", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list jobs %d", status)
	}
	var jobs struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &jobs); err != nil || len(jobs.Data.Items) != 1 {
		t.Fatalf("find ingest job: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/jobs/"+jobs.Data.Items[0].ID, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), "1 个不支持的文件，已跳过，未归档") {
		t.Fatalf("skipped files should be reported: %s", body)
	}
}

func TestZIPWithNoSupportedMembersDoesNotPublish(t *testing.T) {
	env := newEnv(t)
	id, _ := env.upload(t, "压缩包", map[string][]byte{"空.zip": zipWith(t, map[string][]byte{"说明.txt": []byte("unsupported")})})
	env.finalizeAndWait(t, id)
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"status":"failed"`) {
		t.Fatalf("unsupported-only ZIP must fail: %s", body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":0`) {
		t.Fatalf("unsupported-only ZIP must not publish: %s", body)
	}
}

func TestPreviouslyReceivedZIPInCASCanStillBeProcessed(t *testing.T) {
	env := newEnv(t)
	packageZip := zipWith(t, map[string][]byte{"资料.pdf": minimalPDF()})
	id, _ := env.upload(t, "旧上传", map[string][]byte{"资料.zip": packageZip})
	staged, err := filepath.Glob(filepath.Join(env.dataDir, "tmp", "upload-archives", "*.zip"))
	if err != nil || len(staged) != 1 {
		t.Fatalf("find staged ZIP: %v", err)
	}
	store, err := blob.NewStore(env.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(packageZip)
	sha := fmt.Sprintf("%x", sum)
	if err := store.Publish(staged[0], sha); err != nil {
		t.Fatal(err)
	}
	env.finalizeAndWait(t, id)
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"status":"completed"`) {
		t.Fatalf("legacy ZIP upload did not complete: %s", body)
	}
	if !store.Exists(sha) {
		t.Fatal("old CAS file may belong to a historical material and must be preserved")
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"total":1`) || strings.Contains(string(body), `"file_name":"资料.zip"`) {
		t.Fatalf("legacy ZIP created an unexpected material: %s", body)
	}
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
