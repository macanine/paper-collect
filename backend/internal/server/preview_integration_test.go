package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDOCXUploadPreviewDownloadAndMCP(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skip("LibreOffice Writer is not installed")
	}
	original, err := os.ReadFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	env := newEnv(t)
	id := directUpload(t, env, "custom", map[string][]byte{"数学.docx": original})
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	var upload struct {
		Data struct {
			MaterialID string `json:"material_id"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.MaterialID == "" {
		t.Fatalf("upload: %d %s", status, body)
	}
	materialID := upload.Data.MaterialID
	status, downloaded := env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/download", nil, nil)
	if status != http.StatusOK || !bytes.Equal(downloaded, original) {
		t.Fatal("downloaded DOCX differs from original")
	}
	status, preview := env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/preview", nil, nil)
	if status != http.StatusOK || !bytes.HasPrefix(preview, []byte("%PDF-")) {
		t.Fatalf("invalid preview: %d %s", status, body)
	}
	key := createAgentKey(t, env, []string{"materials:read"})
	descriptor := toolData(t, callTool(t, env, key, "materials_preview", map[string]any{"id": materialID}))
	if descriptor["method"] != "GET" || descriptor["media_type"] != "application/pdf" || !strings.HasSuffix(descriptor["path"].(string), "/preview") {
		t.Fatal("unexpected MCP preview descriptor")
	}
	status, fromMCP := env.do(t, http.MethodGet, descriptor["path"].(string), nil, map[string]string{"Authorization": "Bearer " + key})
	if status != http.StatusOK || !bytes.Equal(fromMCP, preview) {
		t.Fatal("MCP preview differs from REST preview")
	}
	status, _ = env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/preview", nil, map[string]string{"Authorization": ""})
	if status != http.StatusUnauthorized {
		t.Fatalf("preview without authorization returned %d", status)
	}
}
