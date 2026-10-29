package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/model"
)

// Only named application operations are exposed; callers cannot supply a URL.
type mcpField struct {
	name, location, kind string
	required, nullable   bool
	schema               map[string]any
}
type mcpOperation struct {
	name, description, method, path, scope, special string
	fields                                          []mcpField
	destructive                                     bool
}

func fields(location, kind, names string) []mcpField {
	out := []mcpField{}
	for _, name := range strings.Fields(names) {
		out = append(out, mcpField{name: name, location: location, kind: kind})
	}
	return out
}
func joined(groups ...[]mcpField) []mcpField {
	var out []mcpField
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}
func pathFields(names string) []mcpField {
	out := fields("path", "string", names)
	for i := range out {
		out[i].required = true
	}
	return out
}
func queryS(names string) []mcpField { return fields("query", "string", names) }
func queryI(names string) []mcpField { return fields("query", "integer", names) }
func queryB(names string) []mcpField { return fields("query", "boolean", names) }
func bodyS(names string) []mcpField  { return fields("body", "string", names) }
func bodyI(names string) []mcpField  { return fields("body", "integer", names) }
func bodyB(names string) []mcpField  { return fields("body", "boolean", names) }
func bodyA(names string) []mcpField  { return fields("body", "strings", names) }
func bodyO(names string) []mcpField  { return fields("body", "object", names) }
func customField(name, kind string, schema map[string]any) []mcpField {
	return []mcpField{{name: name, location: "body", kind: kind, schema: schema}}
}
func mustFields(group []mcpField, names string) []mcpField {
	group = append([]mcpField(nil), group...)
	required := map[string]bool{}
	for _, name := range strings.Fields(names) {
		required[name] = true
	}
	for i := range group {
		if required[group[i].name] {
			group[i].required = true
		}
	}
	return group
}
func nullableField(group []mcpField, name string) []mcpField {
	group = append([]mcpField(nil), group...)
	for i := range group {
		if group[i].name == name {
			group[i].nullable = true
		}
	}
	return group
}
func op(name, description, method, path, scope string, group ...[]mcpField) mcpOperation {
	return mcpOperation{name: name, description: description, method: method, path: path, scope: scope, fields: joined(group...), destructive: method == http.MethodDelete}
}
func special(o mcpOperation, name string) mcpOperation { o.special = name; return o }
func destructive(o mcpOperation) mcpOperation          { o.destructive = true; return o }

var mcpOperations = func() []mcpOperation {
	page := queryI("page page_size")
	id := pathFields("id")
	meta := joined(bodyS("title description grade role"), bodyI("academic_year"), bodyA("subject_codes tags"))
	collection := joined(bodyS("title description kind parent_id"), bodyA("subject_codes"))
	ai := joined(bodyS("name base_url api_key default_model vision_model"), bodyA("models"), bodyB("enabled"), bodyI("priority"))
	ocr := joined(bodyS("name base_url token model"), bodyB("enabled"), bodyI("priority"))
	provider := joined(bodyS("name role"), bodyB("enabled"), bodyI("priority weight"), bodyO("config secret"))
	str := map[string]any{"type": "string"}
	items := map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"material_id"}, "properties": map[string]any{"material_id": str, "position": map[string]any{"type": "integer"}, "role": str, "relative_path": str}}}
	entries := map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"relative_path", "size_bytes"}, "properties": map[string]any{"relative_path": str, "size_bytes": map[string]any{"type": "integer"}, "sha256": str}}}
	return []mcpOperation{
		op("account_me", "读取当前用户。", "GET", "/auth/me", ""),
		op("subjects_list", "读取固定学科字典。", "GET", "/subjects", model.ScopeMaterialsRead),
		op("users_list", "分页读取用户。", "GET", "/users", model.ScopeUsersManage, page),
		op("users_create", "创建用户，角色权限不得超过当前密钥。", "POST", "/users", model.ScopeUsersManage, mustFields(bodyS("username password role"), "username password role")),
		op("api_keys_list", "分页读取密钥元数据。", "GET", "/api-keys", model.ScopeKeysManage, page),
		op("api_keys_create", "创建权限不超过当前密钥的新密钥；密钥仅在本次响应中返回。", "POST", "/api-keys", model.ScopeKeysManage, nullableField(mustFields(joined(bodyS("name expires_at"), bodyA("scopes")), "name scopes"), "expires_at")),
		op("api_keys_revoke", "撤销 API 密钥。", "DELETE", "/api-keys/:id", model.ScopeKeysManage, id),
		op("audit_logs_list", "分页读取审计日志。", "GET", "/audit-logs", model.ScopeAuditRead, page),
		op("questions_search", "按题干和条件搜索题目。", "POST", "/search/questions", model.ScopeMaterialsRead, mustFields(joined(bodyS("query subject grade"), bodyI("academic_year limit offset")), "query")),
		op("questions_get", "读取题目详情。", "GET", "/search/questions/:id", model.ScopeMaterialsRead, id),
		op("search_settings_get", "读取题目索引设置。", "GET", "/search/settings", model.ScopeSearchRead),
		op("search_settings_update", "更新题目索引设置。", "PUT", "/search/settings", model.ScopeSearchWrite, bodyB("enabled"), bodyI("max_pages batch_pages batch_chars"), bodyS("provider_id model")),
		op("ai_providers_list", "读取 AI 服务配置。", "GET", "/ai/providers", model.ScopeAIRead),
		op("ai_providers_create", "创建 AI 服务配置。", "POST", "/ai/providers", model.ScopeAIWrite, ai),
		op("ai_providers_update", "更新 AI 服务配置。", "PATCH", "/ai/providers/:id", model.ScopeAIWrite, id, ai),
		op("ai_providers_probe", "探测 AI 服务支持的模型。", "POST", "/ai/providers/probe", model.ScopeAIWrite, bodyS("base_url api_key provider_id")),
		op("ai_providers_probe_saved", "探测已保存的 AI 服务模型。", "POST", "/ai/providers/:id/probe", model.ScopeAIWrite, id),
		op("ai_providers_test", "测试 AI 服务。", "POST", "/ai/providers/:id/test", model.ScopeAIWrite, id, bodyS("model")),
		op("ocr_providers_list", "读取 OCR 服务配置。", "GET", "/ocr/providers", model.ScopeAIRead),
		op("ocr_providers_create", "创建 OCR 服务配置。", "POST", "/ocr/providers", model.ScopeAIWrite, ocr, bodyS("kind")),
		op("ocr_providers_update", "更新 OCR 服务配置。", "PATCH", "/ocr/providers/:id", model.ScopeAIWrite, id, ocr),
		op("ocr_providers_test", "测试 OCR 服务。", "POST", "/ocr/providers/:id/test", model.ScopeAIWrite, id),
		op("ai_settings_get", "读取分析和 OCR 设置。", "GET", "/ai/settings", model.ScopeAIRead),
		op("ai_settings_update", "更新分析和 OCR 设置。", "PUT", "/ai/settings", model.ScopeAIWrite, bodyB("analysis_enabled"), bodyS("ocr_provider_id mineru_base_url mineru_token"), bodyI("max_pages max_images")),
		op("ai_mineru_test", "测试 MinerU 服务。", "POST", "/ai/mineru/test", model.ScopeAIWrite),
		op("materials_search", "按关键词、学科、角色、状态和年级查询资料。", "GET", "/materials", model.ScopeMaterialsRead, queryS("q subject role status grade"), page),
		op("materials_stats", "统计资料总数及归档状态。", "GET", "/materials/stats", model.ScopeMaterialsRead),
		op("materials_get", "读取资料详情和存储状态。", "GET", "/materials/:id", model.ScopeMaterialsRead, id),
		op("materials_update", "编辑资料字段。", "PATCH", "/materials/:id", model.ScopeMaterialsWrite, id, meta),
		special(op("materials_download", "取得流式下载路径，使用当前密钥直接请求。", "GET", "/materials/:id/download", model.ScopeMaterialsRead, id), "download"),
		special(op("materials_preview", "取得 PDF 预览路径，使用当前密钥直接请求。", "GET", "/materials/:id/preview", model.ScopeMaterialsRead, id), "preview"),
		op("materials_delete", "删除资料，可指定是否级联。", "DELETE", "/materials/:id", model.ScopeMaterialsWrite, id, queryB("cascade")),
		op("material_analysis_enqueue", "创建资料分析任务。", "POST", "/materials/:id/analysis", model.ScopeMaterialsWrite, id, bodyB("force")),
		op("material_analysis_get", "读取资料分析结果。", "GET", "/materials/:id/analysis", model.ScopeMaterialsRead, id),
		op("material_questions_list", "分页读取资料中的题目。", "GET", "/materials/:id/questions", model.ScopeMaterialsRead, id, page),
		op("material_index_get", "读取资料题目索引状态。", "GET", "/materials/:id/index", model.ScopeMaterialsRead, id),
		op("material_index_enqueue", "创建资料题目索引任务。", "POST", "/materials/:id/index", model.ScopeMaterialsWrite, id, bodyB("force")),
		op("material_analysis_apply", "将分析结果应用到资料。", "POST", "/materials/:id/analysis/apply", model.ScopeMaterialsWrite, id, bodyS("title grade role"), bodyI("academic_year"), bodyA("subject_codes tags")),
		op("collections_list", "查询合集，可筛选根合集或父合集。", "GET", "/collections", model.ScopeCollectionsRead, queryS("q parent_id"), queryB("roots"), page),
		op("collections_create", "创建合集。", "POST", "/collections", model.ScopeCollectionsWrite, mustFields(collection, "title")),
		op("collections_get", "读取合集详情和条目。", "GET", "/collections/:id", model.ScopeCollectionsRead, id),
		op("collections_update", "编辑合集。", "PATCH", "/collections/:id", model.ScopeCollectionsWrite, id, collection),
		destructive(op("collections_items_replace", "替换合集的所有条目。", "PUT", "/collections/:id/items", model.ScopeCollectionsWrite, id, mustFields(customField("items", "array", items), "items"))),
		op("collections_delete", "删除合集，可指定是否级联。", "DELETE", "/collections/:id", model.ScopeCollectionsWrite, id, queryB("cascade")),
		op("uploads_create", "创建文件上传清单；sha256 可省略。", "POST", "/uploads", model.ScopeUploadsWrite, mustFields(joined(bodyS("title collection_kind grade"), bodyI("academic_year"), bodyA("subject_codes tags"), customField("entries", "array", entries)), "title entries")),
		op("uploads_get", "读取当前用户的上传进度。", "GET", "/uploads/:id", model.ScopeUploadsWrite, id),
		special(op("uploads_file_put", "上传不超过 512 KiB 的 Base64 文件；较大文件使用流式上传。", "PUT", "/uploads/:id/files/:entry_id", model.ScopeUploadsWrite, pathFields("id entry_id"), mustFields(bodyS("data_base64"), "data_base64")), "upload"),
		special(op("uploads_transfer_info", "取得上传条目的流式上传路径。", "PUT", "/uploads/:id/files/:entry_id", model.ScopeUploadsWrite, pathFields("id entry_id")), "upload_info"),
		op("uploads_finalize", "完成上传并创建归档任务。", "POST", "/uploads/:id/finalize", model.ScopeUploadsWrite, id),
		op("jobs_list", "按状态和类型查询任务。", "GET", "/jobs", model.ScopeJobsRead, queryS("status kind"), page),
		op("jobs_stats", "统计任务总数及状态。", "GET", "/jobs/stats", model.ScopeJobsRead),
		op("jobs_get", "读取任务进度与日志。", "GET", "/jobs/:id", model.ScopeJobsRead, id),
		op("jobs_retry", "重试任务。", "POST", "/jobs/:id/retry", model.ScopeJobsWrite, id),
		op("storage_providers_list", "读取存储端点。", "GET", "/storage/providers", model.ScopeStorageRead),
		op("storage_providers_create", "创建存储端点。", "POST", "/storage/providers", model.ScopeStorageWrite, provider, bodyS("kind")),
		op("storage_providers_update", "更新存储端点。", "PATCH", "/storage/providers/:id", model.ScopeStorageWrite, id, provider),
		op("storage_providers_test", "测试存储端点。", "POST", "/storage/providers/:id/test", model.ScopeStorageWrite, id),
		op("storage_policy_get", "读取存储策略。", "GET", "/storage/policy", model.ScopeStorageRead),
		op("storage_policy_update", "更新存储策略。", "PUT", "/storage/policy", model.ScopeStorageWrite, bodyS("mode"), bodyI("minimum_replicas")),
		op("storage_reconcile", "检查副本并创建修复任务。", "POST", "/storage/reconcile", model.ScopeStorageWrite),
		op("storage_replica_share", "创建或刷新副本分享。", "POST", "/storage/replicas/:id/share", model.ScopeStorageWrite, id),
	}
}()

func (o mcpOperation) tool() mcpTool {
	props := map[string]any{}
	required := []string{}
	for _, f := range o.fields {
		schema := f.schema
		if schema == nil {
			switch f.kind {
			case "strings":
				schema = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
			case "object":
				schema = map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
			default:
				schema = map[string]any{"type": f.kind}
			}
		}
		if f.nullable {
			schema = map[string]any{"type": []string{f.kind, "null"}}
		}
		props[f.name] = schema
		if f.required {
			required = append(required, f.name)
		}
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	readOnly := o.method == http.MethodGet || o.special == "upload_info" || o.name == "questions_search"
	return mcpTool{Name: o.name, Description: o.description, InputSchema: schema, Annotations: map[string]any{"readOnlyHint": readOnly, "destructiveHint": o.destructive, "idempotentHint": readOnly || o.method == http.MethodPut || o.method == http.MethodDelete, "openWorldHint": true}}
}
func visibleTools(p interface{ Has(string) bool }) []mcpTool {
	out := []mcpTool{}
	for _, o := range mcpOperations {
		if o.scope == "" || p.Has(o.scope) {
			out = append(out, o.tool())
		}
	}
	return out
}
func findOperation(name string) (mcpOperation, bool) {
	for _, o := range mcpOperations {
		if o.name == name {
			return o, true
		}
	}
	return mcpOperation{}, false
}
func validPathValue(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 256 {
		return false
	}
	return !strings.ContainsAny(value, "/\\?#%") && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validateField(f mcpField, v any) bool {
	if v == nil {
		return f.nullable
	}
	switch f.kind {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		n, ok := v.(float64)
		return ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n) && math.Abs(n) <= float64(1<<53)
	case "array":
		_, ok := v.([]any)
		return ok
	case "strings":
		arr, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range obj {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

// Forward to the same handlers the web app uses, retaining their permissions and validation.
func (s *Server) runOperation(c *gin.Context, o mcpOperation, args map[string]any) (any, error) {
	if o.scope != "" && !principal(c).Has(o.scope) {
		return nil, apperr.Forbidden("权限不足")
	}
	known := map[string]mcpField{}
	for _, f := range o.fields {
		known[f.name] = f
	}
	for name := range args {
		if _, ok := known[name]; !ok {
			return nil, apperr.Validation("未知参数: " + name)
		}
	}
	path := "/api/v1" + o.path
	query := url.Values{}
	body := map[string]any{}
	for _, f := range o.fields {
		value, exists := args[f.name]
		if !exists {
			if f.required {
				return nil, apperr.Validation("缺少参数: " + f.name)
			}
			continue
		}
		if !validateField(f, value) {
			return nil, apperr.Validation("参数类型错误: " + f.name)
		}
		switch f.location {
		case "path":
			segment := value.(string)
			if !validPathValue(segment) {
				return nil, apperr.Validation("路径参数非法: " + f.name)
			}
			path = strings.Replace(path, ":"+f.name, url.PathEscape(segment), 1)
		case "query":
			switch f.kind {
			case "integer":
				query.Set(f.name, strconv.FormatInt(int64(value.(float64)), 10))
			case "boolean":
				query.Set(f.name, strconv.FormatBool(value.(bool)))
			default:
				query.Set(f.name, value.(string))
			}
		case "body":
			body[f.name] = value
		}
	}
	if o.special == "download" {
		view, err := s.svc.GetMaterial(c.Request.Context(), args["id"].(string))
		if err != nil {
			return nil, err
		}
		return map[string]any{"method": "GET", "path": path, "file_name": view.Material.FileName, "media_type": view.Material.MediaType, "size_bytes": view.Material.SizeBytes}, nil
	}
	if o.special == "preview" {
		view, err := s.svc.GetMaterial(c.Request.Context(), args["id"].(string))
		if err != nil {
			return nil, err
		}
		if view.Material.MediaType != "application/pdf" && view.Material.MediaType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
			return nil, apperr.UnsupportedMedia("该文件类型暂不支持在线预览，请下载原件查看")
		}
		return map[string]any{"method": "GET", "path": path, "media_type": "application/pdf"}, nil
	}
	if o.special == "upload_info" {
		view, err := s.svc.GetUpload(c.Request.Context(), principal(c), args["id"].(string))
		if err != nil {
			return nil, err
		}
		for _, entry := range view.Entries {
			if entry.ID == args["entry_id"].(string) {
				return map[string]any{"method": "PUT", "path": path, "content_type": "application/octet-stream", "size_bytes": entry.SizeBytes}, nil
			}
		}
		return nil, apperr.NotFound("上传条目不存在")
	}
	var payload []byte
	contentType := "application/json"
	if o.special == "upload" {
		encoded := body["data_base64"].(string)
		if len(encoded) > base64.StdEncoding.EncodedLen(512<<10) {
			return nil, apperr.Validation("MCP 上传限额为 512 KiB，请使用流式上传")
		}
		var err error
		payload, err = base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(payload) > 512<<10 {
			return nil, apperr.Validation("data_base64 格式错误或超过 512 KiB")
		}
		contentType = "application/octet-stream"
	} else if o.method != http.MethodGet && o.method != http.MethodDelete {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, apperr.Internal(err)
		}
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	ctx := context.WithValue(c.Request.Context(), internalPrincipalKey{}, principal(c))
	req, err := http.NewRequestWithContext(ctx, o.method, path, bytes.NewReader(payload))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	req.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, req)
	if response.Code == http.StatusNoContent {
		return map[string]any{"ok": true}, nil
	}
	if response.Code >= 400 {
		var e errorEnvelope
		if json.Unmarshal(response.Body.Bytes(), &e) != nil || e.Error.Message == "" {
			return nil, apperr.New(response.Code, "internal_error", "请求处理失败")
		}
		return nil, apperr.New(response.Code, e.Error.Code, e.Error.Message)
	}
	var e struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &e); err != nil {
		return nil, apperr.Internal(err)
	}
	return e.Data, nil
}
