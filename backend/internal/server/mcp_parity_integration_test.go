package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rpcToolResponse struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

func createAgentKey(t *testing.T, e *testEnv, scopes []string) string {
	t.Helper()
	status, raw := e.do(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "mcp-test", "scopes": scopes}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create key status %d", status)
	}
	var response struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Data.Secret == "" {
		t.Fatal("missing key")
	}
	return response.Data.Secret
}
func callTool(t *testing.T, e *testEnv, key, name string, args map[string]any) rpcToolResponse {
	t.Helper()
	status, raw := e.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, map[string]string{"Authorization": "Bearer " + key})
	if status != http.StatusOK {
		t.Fatalf("%s status %d", name, status)
	}
	var response rpcToolResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("%s invalid JSON", name)
	}
	return response
}
func toolData(t *testing.T, r rpcToolResponse) map[string]any {
	t.Helper()
	if r.Error != nil || r.Result.IsError || len(r.Result.Content) == 0 {
		t.Fatalf("tool failed: %+v", r.Error)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(r.Result.Content[0].Text), &data); err != nil {
		t.Fatalf("invalid tool data: %v", err)
	}
	return data
}
func TestMCPApplicationWriteAndUploadParity(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"materials:read", "materials:write", "collections:read", "collections:write", "uploads:write", "jobs:read"})
	collection := toolData(t, callTool(t, env, key, "collections_create", map[string]any{"title": "MCP 合集", "kind": "folder"}))
	id := collection["id"].(string)
	updated := toolData(t, callTool(t, env, key, "collections_update", map[string]any{"id": id, "description": "修改成功"}))
	if updated["description"] != "修改成功" {
		t.Fatal("collection update was not applied")
	}
	roots := toolData(t, callTool(t, env, key, "collections_list", map[string]any{"roots": true, "q": "MCP"}))
	if roots["total"] != float64(1) {
		t.Fatal("collection query filters differ from REST")
	}
	pdf := minimalPDF()
	upload := toolData(t, callTool(t, env, key, "uploads_create", map[string]any{"title": "测试资料", "entries": []any{map[string]any{"relative_path": "test.pdf", "size_bytes": len(pdf)}}}))
	uploadID := upload["id"].(string)
	entries := upload["entries"].([]any)
	entryID := entries[0].(map[string]any)["id"].(string)
	transfer := toolData(t, callTool(t, env, key, "uploads_transfer_info", map[string]any{"id": uploadID, "entry_id": entryID}))
	if transfer["method"] != "PUT" || !strings.HasPrefix(transfer["path"].(string), "/api/v1/uploads/") {
		t.Fatal("invalid upload descriptor")
	}
	_ = toolData(t, callTool(t, env, key, "uploads_file_put", map[string]any{"id": uploadID, "entry_id": entryID, "data_base64": base64.StdEncoding.EncodeToString(pdf)}))
	_ = toolData(t, callTool(t, env, key, "uploads_finalize", map[string]any{"id": uploadID}))
	env.svc.ProcessAvailable(t.Context(), 100)
	materialList := toolData(t, callTool(t, env, key, "materials_search", map[string]any{"q": "test", "role": "other"}))
	if materialList["total"] != float64(1) {
		t.Fatal("material filtering differs from REST")
	}
	material := materialList["items"].([]any)[0].(map[string]any)
	materialID := material["id"].(string)
	_ = toolData(t, callTool(t, env, key, "materials_update", map[string]any{"id": materialID, "description": "已编辑"}))
	download := toolData(t, callTool(t, env, key, "materials_download", map[string]any{"id": materialID}))
	if download["method"] != "GET" || !strings.HasSuffix(download["path"].(string), "/download") {
		t.Fatal("invalid download descriptor")
	}
	req, err := http.NewRequest(http.MethodGet, env.base+download["path"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	contents, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(contents, pdf) {
		t.Fatal("streamed download differs from the uploaded file")
	}
	_ = toolData(t, callTool(t, env, key, "collections_delete", map[string]any{"id": id}))
}
func TestMCPPermissionsAndInputValidation(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"materials:read"})
	for _, name := range []string{"materials_update", "collections_create", "users_list", "ai_settings_get"} {
		r := callTool(t, env, key, name, map[string]any{})
		if !r.Result.IsError {
			t.Errorf("%s must be forbidden", name)
		}
	}
	for _, args := range []map[string]any{{"id": "../users"}, {"id": "%2f"}, {"id": 3}, {"id": "x", "extra": true}} {
		r := callTool(t, env, key, "materials_get", args)
		if r.Error == nil || r.Error.Code != -32602 {
			t.Errorf("invalid arguments accepted: %v", args)
		}
	}
	_, raw := env.do(t, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "materials_search", "arguments": nil}}, map[string]string{"Authorization": "Bearer " + key})
	var invalidArgs rpcToolResponse
	if json.Unmarshal(raw, &invalidArgs) != nil || invalidArgs.Error == nil || invalidArgs.Error.Code != -32602 {
		t.Fatal("null arguments were accepted")
	}
	writer := createAgentKey(t, env, []string{"uploads:write"})
	for _, encoded := range []string{"%%%", strings.Repeat("A", base64.StdEncoding.EncodedLen(512<<10)+4)} {
		r := callTool(t, env, writer, "uploads_file_put", map[string]any{"id": "upload", "entry_id": "file", "data_base64": encoded})
		if r.Error == nil || r.Error.Code != -32602 {
			t.Fatal("invalid or oversized Base64 accepted")
		}
	}
}
func TestMCPDelegatedAdministrationCannotEscalate(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"keys:manage", "users:manage", "materials:read", "collections:read", "jobs:read", "ai:read", "search:read"})
	status, _ := env.do(t, http.MethodGet, "/api/v1/ai/settings", nil, map[string]string{"Authorization": "Bearer " + key})
	if status != http.StatusOK {
		t.Fatalf("explicit AI permission denied: %d", status)
	}
	_ = toolData(t, callTool(t, env, key, "api_keys_create", map[string]any{"name": "child", "scopes": []string{"materials:read"}, "expires_at": nil}))
	invalid := callTool(t, env, key, "api_keys_create", map[string]any{"name": "elevated", "scopes": []string{"storage:write"}})
	if invalid.Error == nil && !invalid.Result.IsError {
		t.Fatal("agent granted a scope it does not hold")
	}
	user := toolData(t, callTool(t, env, key, "users_create", map[string]any{"username": "viewer123", "password": "strong-password-123", "role": "viewer"}))
	if user["role"] != "viewer" {
		t.Fatal("viewer was not created")
	}
	invalid = callTool(t, env, key, "users_create", map[string]any{"username": "admin123", "password": "strong-password-123", "role": "admin"})
	if !invalid.Result.IsError {
		t.Fatal("agent created a more privileged user")
	}
}

func TestMCPRejectsTrailingJSONAndOversizedWhitespace(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"materials:read"})
	valid := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, payload := range []string{valid + ` {}`, valid + strings.Repeat(" ", 1<<20)} {
		req, err := http.NewRequest(http.MethodPost, env.base+"/mcp", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := env.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body rpcToolResponse
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil || body.Error == nil || body.Error.Code != -32700 {
			t.Fatal("invalid MCP framing was accepted")
		}
	}
}

func TestDashboardStatsRESTAndMCPParity(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"materials:read", "jobs:read"})
	headers := map[string]string{"Authorization": "Bearer " + key}
	uploadID, _ := env.upload(t, "统计测试", map[string][]byte{"stats.pdf": minimalPDF()})
	env.finalizeAndWait(t, uploadID)
	for _, test := range []struct {
		path, tool string
		fields     []string
	}{
		{"/api/v1/materials/stats", "materials_stats", []string{"total", "pending_storage", "archived", "degraded"}},
		{"/api/v1/jobs/stats", "jobs_stats", []string{"total", "pending", "running", "succeeded", "failed"}},
	} {
		status, raw := env.do(t, http.MethodGet, test.path, nil, headers)
		if status != http.StatusOK {
			t.Fatalf("%s returned %d", test.path, status)
		}
		var response struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		result := toolData(t, callTool(t, env, key, test.tool, map[string]any{}))
		for _, field := range test.fields {
			if _, ok := response.Data[field]; !ok || result[field] != response.Data[field] {
				t.Fatalf("%s field %s differs between REST and MCP", test.tool, field)
			}
		}
		if response.Data["total"].(float64) < 1 {
			t.Fatalf("%s should include created data", test.tool)
		}
	}
	readKey := createAgentKey(t, env, []string{"materials:read"})
	status, _ := env.do(t, http.MethodGet, "/api/v1/jobs/stats", nil, map[string]string{"Authorization": "Bearer " + readKey})
	if status != http.StatusForbidden {
		t.Fatalf("jobs stats bypassed scope: %d", status)
	}
	if response := callTool(t, env, readKey, "jobs_stats", map[string]any{}); !response.Result.IsError {
		t.Fatal("jobs MCP stats bypassed scope")
	}
}

func TestMCPUponZIPUploadArchivesOnlyMembers(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"uploads:write", "materials:read"})
	packageZip := zipWith(t, map[string][]byte{"试卷.pdf": minimalPDF()})
	upload := toolData(t, callTool(t, env, key, "uploads_create", map[string]any{
		"title": "资料", "collection_kind": "zip", "entries": []any{
			map[string]any{"relative_path": "资料.zip", "size_bytes": len(packageZip)},
		},
	}))
	uploadID := upload["id"].(string)
	entry := upload["entries"].([]any)[0].(map[string]any)
	_ = toolData(t, callTool(t, env, key, "uploads_file_put", map[string]any{
		"id": uploadID, "entry_id": entry["id"], "data_base64": base64.StdEncoding.EncodeToString(packageZip),
	}))
	_ = toolData(t, callTool(t, env, key, "uploads_finalize", map[string]any{"id": uploadID}))
	env.svc.ProcessAvailable(t.Context(), 100)
	result := toolData(t, callTool(t, env, key, "uploads_get", map[string]any{"id": uploadID}))
	if result["status"] != "completed" || result["collection_id"] == "" {
		t.Fatal("MCP ZIP did not create a collection")
	}
	materials := toolData(t, callTool(t, env, key, "materials_search", map[string]any{}))
	if materials["total"] != float64(1) || materials["items"].([]any)[0].(map[string]any)["file_name"] != "试卷.pdf" {
		t.Fatal("MCP did not archive exactly the ZIP member")
	}
}

func TestMCPCreateOneDriveUsingOpenList(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"storage:write"})
	result := callTool(t, env, key, "storage_providers_create", map[string]any{
		"name": "OneDrive", "kind": "onedrive", "role": "backup", "enabled": true,
		"config": map[string]any{"auth_mode": "openlist"},
		"secret": map[string]any{"refresh_token": "test-private-token"},
	})
	provider := toolData(t, result)
	config, ok := provider["config"].(map[string]any)
	if !ok || config["auth_mode"] != "openlist" || provider["secret_configured"] != true {
		t.Fatal("MCP could not create a OneDrive endpoint using OpenList")
	}
	if strings.Contains(result.Result.Content[0].Text, "test-private-token") {
		t.Fatal("MCP response must not expose the refresh token")
	}
}
