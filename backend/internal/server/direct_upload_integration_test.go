package server_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func directUpload(t *testing.T, env *testEnv, kind string, files map[string][]byte) string {
	t.Helper()
	entries := make([]map[string]any, 0, len(files))
	for name, content := range files {
		entries = append(entries, map[string]any{"relative_path": name, "size_bytes": len(content)})
	}
	status, body := env.do(t, http.MethodPost, "/api/v1/uploads", map[string]any{
		"title": "自动上传", "collection_kind": kind, "subject_codes": []string{},
		"entries": entries,
	}, nil)
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

func TestDirectSingleFileNeedsNoCollectionOrSubject(t *testing.T) {
	env := newEnv(t)
	pdf := minimalPDF()
	id := directUpload(t, env, "custom", map[string][]byte{"资料.pdf": pdf})
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	var upload struct {
		Data struct {
			Status       string `json:"status"`
			MaterialID   string `json:"material_id"`
			CollectionID string `json:"collection_id"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.Status != "completed" || upload.Data.MaterialID == "" || upload.Data.CollectionID != "" {
		t.Fatalf("single upload %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+upload.Data.MaterialID, nil, nil)
	var material struct {
		Data struct {
			FileName string   `json:"file_name"`
			Subjects []string `json:"subject_codes"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &material) != nil || material.Data.FileName != "资料.pdf" || len(material.Data.Subjects) != 0 {
		t.Fatalf("single material %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":0`)) {
		t.Fatalf("unexpected single-file collection %d: %s", status, body)
	}
	status, data := env.do(t, http.MethodGet, "/api/v1/materials/"+upload.Data.MaterialID+"/download", nil, nil)
	if status != http.StatusOK || !bytes.Equal(data, pdf) {
		t.Fatalf("download %d", status)
	}
	status, body = env.do(t, http.MethodPatch, "/api/v1/materials/"+upload.Data.MaterialID, map[string]any{"subject_codes": []string{"math"}}, nil)
	if status != http.StatusOK {
		t.Fatalf("set subject %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPatch, "/api/v1/materials/"+upload.Data.MaterialID, map[string]any{"subject_codes": []string{}}, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"subject_codes":[]`)) {
		t.Fatalf("clear subject %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodDelete, "/api/v1/materials/"+upload.Data.MaterialID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete material %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"material_id":""`)) {
		t.Fatalf("deleted material still linked: %d %s", status, body)
	}
}

func TestCrossEntityLookupFilters(t *testing.T) {
	env := newEnv(t)
	content := minimalPDF()
	firstUpload := directUpload(t, env, "custom", map[string][]byte{"甲试卷.pdf": content})
	secondUpload := directUpload(t, env, "custom", map[string][]byte{"乙试卷.pdf": content})

	readUpload := func(id string) (materialID, jobID string) {
		t.Helper()
		status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
		var upload struct {
			Data struct {
				MaterialID string `json:"material_id"`
				JobID      string `json:"job_id"`
			} `json:"data"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.MaterialID == "" || upload.Data.JobID == "" {
			t.Fatalf("read upload %s: %d %s", id, status, body)
		}
		return upload.Data.MaterialID, upload.Data.JobID
	}

	firstMaterial, firstJob := readUpload(firstUpload)
	secondMaterial, _ := readUpload(secondUpload)
	if firstMaterial == secondMaterial {
		t.Fatal("logical files with the same content should remain separate materials")
	}

	sum := sha256.Sum256(content)
	sha256Hex := fmt.Sprintf("%x", sum)
	status, body := env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10&sha256="+sha256Hex, nil, nil)
	var materials struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				ID     string `json:"id"`
				SHA256 string `json:"sha256"`
			} `json:"items"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &materials) != nil || materials.Data.Total != 2 {
		t.Fatalf("lookup materials by sha256: %d %s", status, body)
	}
	for _, item := range materials.Data.Items {
		if item.SHA256 != sha256Hex {
			t.Fatalf("unexpected material hash in lookup: %s", body)
		}
	}

	status, body = env.do(t, http.MethodPost, "/api/v1/collections", map[string]any{
		"title": "互链测试合集", "kind": "exam", "subject_codes": []string{"math"},
	}, nil)
	var collection struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if status != http.StatusCreated || json.Unmarshal(body, &collection) != nil || collection.Data.ID == "" {
		t.Fatalf("create collection: %d %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/collections/"+collection.Data.ID+"/items", map[string]any{
		"items": []map[string]any{
			{"material_id": firstMaterial, "position": 0, "role": "exam"},
			{"material_id": secondMaterial, "position": 1, "role": "exam"},
		},
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put collection items: %d %s", status, body)
	}

	status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+firstMaterial, nil, nil)
	var materialDetail struct {
		Data struct {
			Collections []struct {
				ID string `json:"id"`
			} `json:"collections"`
			Replicas []struct {
				ID string `json:"id"`
			} `json:"replicas"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &materialDetail) != nil ||
		len(materialDetail.Data.Collections) != 1 || materialDetail.Data.Collections[0].ID != collection.Data.ID ||
		len(materialDetail.Data.Replicas) == 0 {
		t.Fatalf("material detail relations: %d %s", status, body)
	}
	replicaID := materialDetail.Data.Replicas[0].ID

	status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10&material_id="+firstMaterial, nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":1`)) || !bytes.Contains(body, []byte(collection.Data.ID)) {
		t.Fatalf("lookup collections by material: %d %s", status, body)
	}

	readJobs := func(query string) []struct {
		ID         string `json:"id"`
		UploadID   string `json:"upload_id"`
		MaterialID string `json:"material_id"`
		ReplicaID  string `json:"replica_id"`
	} {
		t.Helper()
		status, body := env.do(t, http.MethodGet, "/api/v1/jobs?page=1&page_size=20&"+query, nil, nil)
		var jobs struct {
			Data struct {
				Total int `json:"total"`
				Items []struct {
					ID         string `json:"id"`
					UploadID   string `json:"upload_id"`
					MaterialID string `json:"material_id"`
					ReplicaID  string `json:"replica_id"`
				} `json:"items"`
			} `json:"data"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &jobs) != nil || jobs.Data.Total == 0 {
			t.Fatalf("lookup jobs by %s: %d %s", query, status, body)
		}
		for _, item := range jobs.Data.Items {
			if query == "upload_id="+firstUpload && item.UploadID != firstUpload {
				t.Fatalf("upload filter leaked another job: %s", body)
			}
			if query == "material_id="+firstMaterial && item.MaterialID != firstMaterial {
				t.Fatalf("material filter leaked another job: %s", body)
			}
			if query == "replica_id="+replicaID && item.ReplicaID != replicaID {
				t.Fatalf("replica filter leaked another job: %s", body)
			}
		}
		return jobs.Data.Items
	}
	uploadJobs := readJobs("upload_id=" + firstUpload)
	if uploadJobs[0].ID != firstJob {
		t.Fatalf("upload relation returned unexpected job: %#v", uploadJobs[0])
	}
	readJobs("material_id=" + firstMaterial)
	readJobs("replica_id=" + replicaID)
}

func TestDirectFolderAndZIPBuildOneCollection(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		files      map[string][]byte
		wantItems  int
	}{
		{"folder", "folder", map[string][]byte{"数学/一.pdf": minimalPDF(), "数学/二.pdf": minimalPDF()}, 2},
		{"zip", "zip", map[string][]byte{"资料.zip": zipWith(t, map[string][]byte{"一.pdf": minimalPDF(), "二.pdf": minimalPDF()})}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			id := directUpload(t, env, tc.kind, tc.files)
			status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
			var upload struct {
				Data struct {
					CollectionID string `json:"collection_id"`
					MaterialID   string `json:"material_id"`
					Status       string `json:"status"`
				} `json:"data"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.Status != "completed" || upload.Data.CollectionID == "" || upload.Data.MaterialID != "" {
				t.Fatalf("collection upload %d: %s", status, body)
			}
			status, body = env.do(t, http.MethodGet, "/api/v1/collections/"+upload.Data.CollectionID, nil, nil)
			var detail struct {
				Data struct {
					Items []json.RawMessage `json:"items"`
				} `json:"data"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &detail) != nil || len(detail.Data.Items) != tc.wantItems {
				t.Fatalf("collection items %d: %s", status, body)
			}
			status, body = env.do(t, http.MethodGet, "/api/v1/collections?page=1&page_size=10", nil, nil)
			if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":1`)) {
				t.Fatalf("expected exactly one collection %d: %s", status, body)
			}
		})
	}
}

func TestDirectUploadStillRejectsUnsafeZIP(t *testing.T) {
	env := newEnv(t)
	bad := zipWith(t, map[string][]byte{"../escape.pdf": minimalPDF()})
	id := directUpload(t, env, "zip", map[string][]byte{"危险.zip": bad})
	status, body := env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"status":"failed"`)) {
		t.Fatalf("unsafe ZIP should fail %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodGet, "/api/v1/materials?page=1&page_size=10", nil, nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"total":0`)) {
		t.Fatalf("unsafe ZIP should not publish material %d: %s", status, body)
	}
}

func TestIndexingCanBeEnabledAfterUpload(t *testing.T) {
	env := newEnv(t)
	settings := map[string]any{"enabled": false, "max_pages": 500, "batch_pages": 10, "batch_chars": 12000}
	status, body := env.do(t, http.MethodPut, "/api/v1/search/settings", settings, nil)
	if status != http.StatusOK {
		t.Fatalf("disable indexing %d: %s", status, body)
	}
	id := directUpload(t, env, "custom", map[string][]byte{"试卷.pdf": minimalPDF()})
	status, body = env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	var upload struct {
		Data struct {
			MaterialID string `json:"material_id"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.MaterialID == "" {
		t.Fatalf("load archived material %d: %s", status, body)
	}
	ctx := context.Background()
	if queued, err := env.svc.EnqueueMissingIndexes(ctx); err != nil || queued != 0 {
		t.Fatalf("indexing disabled: queued %d, error %v", queued, err)
	}
	settings["enabled"] = true
	status, body = env.do(t, http.MethodPut, "/api/v1/search/settings", settings, nil)
	if status != http.StatusOK {
		t.Fatalf("enable indexing %d: %s", status, body)
	}
	if queued, err := env.svc.EnqueueMissingIndexes(ctx); err != nil || queued != 1 {
		t.Fatalf("queue missing index: queued %d, error %v", queued, err)
	}
	state, err := env.svc.GetMaterialIndexState(ctx, upload.Data.MaterialID)
	if err != nil || state.Status != "pending" || state.JobID == "" {
		t.Fatalf("missing index state: %+v, %v", state, err)
	}
	if queued, err := env.svc.EnqueueMissingIndexes(ctx); err != nil || queued != 0 {
		t.Fatalf("duplicate missing index: queued %d, error %v", queued, err)
	}
}

func TestRemovedInspectionAPIAndFields(t *testing.T) {
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/inspect?filename=old.pdf", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("removed inspection endpoint %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPost, "/api/v1/uploads", map[string]any{
		"title": "资料", "entries": []map[string]any{{"relative_path": "资料.pdf", "size_bytes": 10, "inspection_id": "old"}},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("removed inspection field %d: %s", status, body)
	}
}

func TestDirectUploadSharesParsingBetweenAnalysisAndIndex(t *testing.T) {
	fake := newFakeAI(t)
	env := newEnv(t)
	status, body := env.do(t, http.MethodPost, "/api/v1/ai/providers", map[string]any{
		"name": "文本模型", "base_url": fake.server.URL + "/v1", "api_key": "sk-test",
		"models": []string{"text-model"}, "default_model": "text-model", "enabled": true, "priority": 1,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("provider %d: %s", status, body)
	}
	status, body = env.do(t, http.MethodPut, "/api/v1/ai/settings", map[string]any{
		"analysis_enabled": true, "mineru_base_url": fake.server.URL,
		"mineru_token": "mineru-token", "max_pages": 3, "max_images": 0,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("settings %d: %s", status, body)
	}
	id := directUpload(t, env, "custom", map[string][]byte{"试卷.pdf": minimalPDF()})
	status, body = env.do(t, http.MethodGet, "/api/v1/uploads/"+id, nil, nil)
	var upload struct {
		Data struct {
			MaterialID string `json:"material_id"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &upload) != nil || upload.Data.MaterialID == "" {
		t.Fatalf("upload %d: %s", status, body)
	}
	for _, suffix := range []string{"analysis", "index"} {
		status, body = env.do(t, http.MethodGet, "/api/v1/materials/"+upload.Data.MaterialID+"/"+suffix, nil, nil)
		if status != http.StatusOK || !bytes.Contains(body, []byte(`"status":"succeeded"`)) {
			t.Fatalf("%s %d: %s", suffix, status, body)
		}
	}
	if fake.parseCalls() != 1 {
		t.Fatalf("analysis and indexing should share parsing, got %d calls", fake.parseCalls())
	}
}
