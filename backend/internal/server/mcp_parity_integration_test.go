package server_test

import (
	"encoding/base64"
	"encoding/json"
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

func TestMCPCreateOneDriveUsingOpenList(t *testing.T) {
	env := newEnv(t)
	key := createAgentKey(t, env, []string{"storage:write"})
	result := callTool(t, env, key, "storage_drives_create", map[string]any{
		"name": "OneDrive", "kind": "onedrive", "enabled": true,
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
