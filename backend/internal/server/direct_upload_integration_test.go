package server_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func directUpload(t *testing.T, env *testEnv, files map[string][]byte) string {
	t.Helper()
	entries := make([]map[string]any, 0, len(files))
	for name, content := range files {
		entries = append(entries, map[string]any{"relative_path": name, "size_bytes": len(content)})
	}
	status, body := env.do(t, http.MethodPost, "/api/v1/uploads", map[string]any{"entries": entries}, nil)
	var created struct {
		Data struct {
			ID      string `json:"id"`
			Entries []struct {
				ID           string `json:"id"`
				RelativePath string `json:"relative_path"`
			} `json:"entries"`
		} `json:"data"`
	}
	if status != http.StatusCreated || json.Unmarshal(body, &created) != nil || len(created.Data.Entries) != len(files) {
		t.Fatalf("create direct upload %d: %s", status, body)
	}
	for _, entry := range created.Data.Entries {
		req, err := http.NewRequest(http.MethodPut, env.base+"/api/v1/uploads/"+created.Data.ID+"/files/"+entry.ID, bytes.NewReader(files[entry.RelativePath]))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+env.token)
		res, err := env.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("put %s %d: %s", entry.RelativePath, res.StatusCode, data)
		}
	}
	env.finalizeAndWait(t, created.Data.ID)
	return created.Data.ID
}

type uploadResultBody struct {
	Data struct {
		Status         string   `json:"status"`
		NewCount       int      `json:"new_count"`
		DuplicateCount int      `json:"duplicate_count"`
		Skipped        []string `json:"skipped"`
		Results        []struct {
			MaterialID string `json:"material_id"`
			PackID     string `json:"pack_id"`
			Path       string `json:"path"`
			Duplicate  bool   `json:"duplicate"`
		} `json:"results"`
	} `json:"data"`
}

func (e *testEnv) uploadResult(t *testing.T, id string) uploadResultBody {
	t.Helper()
	status, body := e.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	var out uploadResultBody
	if status != http.StatusOK || json.Unmarshal(body, &out) != nil {
		t.Fatalf("upload %d: %s", status, body)
	}
	return out
}

func TestDirectSingleFileBecomesOnePackWithoutCollections(t *testing.T) {
	env := newEnv(t)
	configureCatalogAI(t, env, newFakeAI(t))
	pdf := pdfWith("single")
	id := directUpload(t, env, map[string][]byte{"资料.pdf": pdf})
	result := env.uploadResult(t, id)
	if result.Data.Status != "completed" || result.Data.NewCount != 1 || len(result.Data.Results) != 1 {
		t.Fatalf("single upload: %+v", result.Data)
	}
	materialID, packID := result.Data.Results[0].MaterialID, result.Data.Results[0].PackID
	status, body := env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":0`)) {
		t.Fatalf("uploads must never create topics %d: %s", status, body)
	}
	status, data := env.do(t, http.MethodGet, "/api/v1/materials/"+materialID+"/download", nil, nil)
	if status != http.StatusOK || !bytes.Equal(data, pdf) {
		t.Fatalf("download %d", status)
	}
	status, body = env.do(t, http.MethodPatch, "/api/v1/packs/"+packID, map[string]any{"subject_codes": []string{"math"}, "doc_type": "lecture", "grade": "grade10"}, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"title":"2026届 讲义 高一 数学"`)) || !bytes.Contains(body, []byte(`"locked_fields":[`)) {
		t.Fatalf("pack edit must rename and lock the catalogue %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPatch, "/api/v1/packs/"+packID, map[string]any{"title": "我的讲义"}, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"title"`)) {
		t.Fatalf("manual title %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodDelete, "/api/v1/materials/"+materialID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("trash material %d: %s", status, body)
	}
	if env.listPacks(t, "").Data.Total != 0 || env.listPacks(t, "&trashed=true").Data.Total != 1 {
		t.Fatal("deleting the only file must move its pack to the bin")
	}
}

func TestKnownContentSkipsTransfer(t *testing.T) {
	env := newEnv(t)
	pdf := pdfWith("known")
	directUpload(t, env, map[string][]byte{"原始.pdf": pdf})
	sum := fmt.Sprintf("%x", sha256.Sum256(pdf))
	status, body := env.do(t, http.MethodPost, "/api/v1/uploads", map[string]any{
		"entries": []map[string]any{{"relative_path": "微信转发/原始(1).pdf", "size_bytes": len(pdf), "sha256": sum}},
	}, nil)
	if status != http.StatusCreated || !bytes.Contains(body, []byte(`"status":"existing"`)) {
		t.Fatalf("known content must be marked existing %d: %s", status, body)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &created)
	env.finalizeAndWait(t, created.Data.ID)
	result := env.uploadResult(t, created.Data.ID)
	if result.Data.NewCount != 0 || result.Data.DuplicateCount != 1 || len(result.Data.Results) != 1 || !result.Data.Results[0].Duplicate {
		t.Fatalf("existing file must only gain a source: %+v", result.Data)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+result.Data.Results[0].MaterialID, nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte("微信转发/原始(1).pdf")) || !bytes.Contains(body, []byte(`"path":"原始.pdf"`)) {
		t.Fatalf("both arrival paths must be recorded %d: %s", status, body)
	}
}

func TestFolderAndNestedZIPExpandIntoPacks(t *testing.T) {
	env := newEnv(t)
	configureCatalogAI(t, env, newFakeAI(t))
	inner := zipWith(t, map[string][]byte{"化学试题.pdf": pdfWith("chem-q"), "化学答案.pdf": pdfWith("chem-a")})
	outer := zipWith(t, map[string][]byte{"数学讲义.pdf": pdfWith("math"), "内层.zip": inner, "说明.txt": []byte("x")})
	id := directUpload(t, env, map[string][]byte{"资料包.zip": outer, "目录/物理周测.pdf": pdfWith("phys")})
	result := env.uploadResult(t, id)
	if result.Data.NewCount != 4 || len(result.Data.Skipped) != 1 || !strings.HasSuffix(result.Data.Skipped[0], "说明.txt") {
		t.Fatalf("nested packages must expand and unsupported files be listed: %+v", result.Data)
	}
	packs := env.listPacks(t, "")
	if packs.Data.Total != 3 {
		t.Fatalf("chemistry pair plus two singles expected: %+v", packs.Data)
	}
	status, body := env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":0`)) {
		t.Fatalf("folders and ZIPs must not create topics %d: %s", status, body)
	}
}

func TestDirectUploadStillRejectsUnsafeZIP(t *testing.T) {
	env := newEnv(t)
	bad := zipWith(t, map[string][]byte{"../escape.pdf": minimalPDF()})
	id := directUpload(t, env, map[string][]byte{"危险.zip": bad})
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"status":"failed"`)) {
		t.Fatalf("unsafe ZIP should fail %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":0`)) {
		t.Fatalf("unsafe ZIP should not publish material %d: %s", status, body)
	}
}
