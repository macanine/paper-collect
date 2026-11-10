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
	packMeta := joined(bodyS("title doc_type grade term region organization exam_date exam_name chapter description"), bodyI("cohort"), bodyA("subject_codes tags"))
	packPatch := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"doc_type": map[string]any{"type": "string"}, "grade": map[string]any{"type": "string"}, "term": map[string]any{"type": "string"},
		"region": map[string]any{"type": "string"}, "organization": map[string]any{"type": "string"}, "exam_date": map[string]any{"type": "string"},
		"exam_name": map[string]any{"type": "string"}, "chapter": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
		"cohort": map[string]any{"type": "integer"}, "subject_codes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}}
	hints := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"doc_type": map[string]any{"type": "string"}, "grade": map[string]any{"type": "string"}, "region": map[string]any{"type": "string"},
		"organization": map[string]any{"type": "string"}, "cohort": map[string]any{"type": "integer"},
		"subject_codes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"tags":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}}
	ids := mustFields(bodyA("ids"), "ids")
	ai := joined(bodyS("name base_url api_key default_model vision_model"), bodyA("models"), bodyB("enabled"), bodyI("priority"))
	ocr := joined(bodyS("name base_url token model"), bodyB("enabled"), bodyI("priority"))
	driveRoutes := map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"node_id", "priority"}, "properties": map[string]any{
		"node_id": map[string]any{"type": "string"}, "priority": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}, "enabled": map[string]any{"type": "boolean"},
	}}}
	drive := joined(bodyS("name root_path on_path_change"), bodyB("enabled"), bodyI("max_file_bytes read_order"), bodyA("allowed_exts"), customField("route_mode", "string", map[string]any{"enum": []string{"pool", "preferred", "exclusive"}}), customField("routes", "array", driveRoutes), bodyO("config secret"))
	strs := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	integer := map[string]any{"type": "integer"}
	boolean := map[string]any{"type": "boolean"}
	rules := map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "name", "enabled", "is_default", "conditions", "targets"}, "properties": map[string]any{
		"id": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "enabled": boolean, "is_default": boolean,
		"conditions": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"subjects": strs, "doc_types": strs, "grades": strs, "regions": strs, "tags": strs, "parts": strs, "media_types": strs,
			"cohorts": map[string]any{"type": "array", "items": integer}, "min_size": integer, "max_size": integer,
		}},
		"targets": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"drive_id", "path_template"}, "properties": map[string]any{
			"drive_id": map[string]any{"type": "string"}, "path_template": map[string]any{"type": "string"}, "share": boolean, "required": boolean,
		}}},
	}}}
	str := map[string]any{"type": "string"}
	nodeSlots := map[string]any{
		"type": "object", "additionalProperties": map[string]any{"type": "integer", "minimum": 0, "maximum": 64},
	}
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
		op("questions_search", "按题干和条件搜索题目；academic_year 表示届别。", "POST", "/search/questions", model.ScopeMaterialsRead, mustFields(joined(bodyS("query subject grade"), bodyI("academic_year limit offset")), "query")),
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
		op("ai_settings_get", "读取 OCR 与页面图像设置。编目始终由 AI 完成，没有开关。", "GET", "/ai/settings", model.ScopeAIRead),
		op("ai_settings_update", "更新 OCR 与页面图像设置；保存后等待 AI 的文件会立即继续。", "PUT", "/ai/settings", model.ScopeAIWrite, bodyS("ocr_provider_id"), bodyI("max_pages max_images")),
		op("ai_rerun", "让已有文件重新经过 AI：mode=decide 只重新编目套卷，mode=full 重新阅读（复用已缓存正文）、分组和编目；scope=attention|all；dry_run=true 只返回影响范围和预计调用次数。人工锁定的字段和部件保持不变。", "POST", "/ai/rerun", model.ScopeAIWrite, mustFields(bodyS("mode scope"), "mode scope"), bodyB("dry_run")),
		op("entities_list", "查询名称词典（kind=organization 学校/联盟，kind=tag 标签），返回规范名称、别名和使用次数。", "GET", "/entities", model.ScopeMaterialsRead, mustFields(queryS("kind q"), "kind"), page),
		op("entities_update", "修改名称词典条目：改名（同步所有套卷）、地区、锁定，或移除别名；人工修改后 AI 不再改动该条目。", "PATCH", "/entities/:id", model.ScopeMaterialsWrite, id, bodyS("name region"), bodyB("locked"), bodyA("remove_aliases")),
		op("entities_merge", "把一个名称条目并入另一个（into），别名和套卷一起迁移。", "POST", "/entities/:id/merge", model.ScopeMaterialsWrite, id, mustFields(bodyS("into"), "into")),
		op("packs_search", "查询资料库中的套卷：按关键词、学科、年级、类型、学期、届别、地区、机构、标签、整理状态、缺答案或专题筛选；content=true 时同时匹配正文。", "GET", "/packs", model.ScopeMaterialsRead, queryS("q subject grade doc_type term region organization tag stage missing collection_id material_id sort"), queryI("cohort"), queryB("content trashed"), page),
		op("packs_facets", "读取资料库中已有的学科、年级、类型、届别、地区、机构和标签及数量。", "GET", "/packs/facets", model.ScopeMaterialsRead),
		op("packs_get", "读取套卷详情、文件部件与齐全度。", "GET", "/packs/:id", model.ScopeMaterialsRead, id),
		op("packs_ai_get", "读取套卷最近一次 AI 编目决定、校验问题和每个文件的阅读记录。", "GET", "/packs/:id/ai", model.ScopeMaterialsRead, id),
		op("packs_ai_redecide", "让 AI 重新确定该套卷的编目；人工锁定的字段保持不变。", "POST", "/packs/:id/ai/redecide", model.ScopeMaterialsWrite, id),
		op("packs_update", "编辑套卷编目字段；修改的字段会被锁定，AI 之后不再改动；title 为空字符串时恢复自动命名。机构和标签会归入名称词典。", "PATCH", "/packs/:id", model.ScopeMaterialsWrite, id, packMeta),
		op("packs_bulk_update", "对多个套卷应用相同的编目修改。", "POST", "/packs/bulk", model.ScopeMaterialsWrite, ids, mustFields(customField("patch", "object", packPatch), "patch")),
		op("packs_merge", "把其他套卷的文件并入该套卷。", "POST", "/packs/:id/merge", model.ScopeMaterialsWrite, id, mustFields(bodyA("source_ids"), "source_ids")),
		op("packs_reorder", "设置套卷内文件顺序，需包含全部文件。", "PUT", "/packs/:id/order", model.ScopeMaterialsWrite, id, mustFields(bodyA("material_ids"), "material_ids")),
		op("packs_trash", "把套卷移入回收站，30 天内可恢复。", "POST", "/packs/trash", model.ScopeMaterialsWrite, ids),
		op("packs_restore", "从回收站恢复套卷。", "POST", "/packs/restore", model.ScopeMaterialsWrite, ids),
		destructive(op("packs_purge", "彻底删除回收站中的套卷及其本地原件。", "POST", "/packs/purge", model.ScopeMaterialsWrite, ids)),
		special(op("packs_download", "取得整套 ZIP 的流式下载路径，使用当前密钥直接请求。", "GET", "/packs/:id/download", model.ScopeMaterialsRead, id), "pack_download"),
		op("materials_search", "查询文件：按关键词、整理状态（processing/waiting_ai/attention/ready）、部件、归档状态、同步状态、需关注原因、套卷或内容指纹筛选。", "GET", "/materials", model.ScopeMaterialsRead, queryS("q stage part status sync_status reason pack_id sha256"), page),
		op("materials_review_packs", "按套卷分页读取需关注（默认）、处理中或等待 AI 的文件；每套只返回一次，适合批量核对。", "GET", "/materials/review-packs", model.ScopeMaterialsRead, queryS("stage reason"), page),
		op("materials_stats", "统计套卷、文件整理状态、缺答案与存储健康。", "GET", "/materials/stats", model.ScopeMaterialsRead),
		op("materials_accept", "人工确认需关注的文件，按当前编目入库并开始同步。", "POST", "/materials/accept", model.ScopeMaterialsWrite, ids),
		op("materials_get", "读取文件详情、来源记录和存储副本。", "GET", "/materials/:id", model.ScopeMaterialsRead, id),
		op("materials_update", "修改文件部件、答案起始页（人工设定的部件会被锁定），或移入其他套卷（pack_id）/拆成独立套卷（new_pack）；移动后 AI 会重新确定相关套卷的编目。", "PATCH", "/materials/:id", model.ScopeMaterialsWrite, id, bodyS("part pack_id"), bodyI("answer_start_page"), bodyB("new_pack")),
		special(op("materials_download", "取得流式下载路径，使用当前密钥直接请求。", "GET", "/materials/:id/download", model.ScopeMaterialsRead, id), "download"),
		op("materials_pull_cache", "从可用云端副本拉取文件到临时缓存并校验 SHA-256，返回有时效的下载路径。", "POST", "/materials/:id/pull-cache", model.ScopeMaterialsRead, id),
		special(op("materials_pull_cache_download", "取得已拉取临时文件的流式下载路径，使用当前密钥直接请求。", "GET", "/materials/:id/pull-cache", model.ScopeMaterialsRead, id), "pull_cache_download"),
		special(op("materials_preview", "取得 PDF 预览路径，使用当前密钥直接请求。", "GET", "/materials/:id/preview", model.ScopeMaterialsRead, id), "preview"),
		op("materials_delete", "把单个文件移入回收站。", "DELETE", "/materials/:id", model.ScopeMaterialsWrite, id),
		op("material_analysis_enqueue", "让 AI 重新阅读该文件，之后重新确定所在套卷的编目。", "POST", "/materials/:id/analysis", model.ScopeMaterialsWrite, id),
		op("material_analysis_get", "读取 AI 对该文件的阅读记录（部件、标题、证据摘录与校验问题）。", "GET", "/materials/:id/analysis", model.ScopeMaterialsRead, id),
		op("material_questions_list", "分页读取文件中的题目。", "GET", "/materials/:id/questions", model.ScopeMaterialsRead, id, page),
		op("material_index_get", "读取文件题目索引状态。", "GET", "/materials/:id/index", model.ScopeMaterialsRead, id),
		op("material_index_enqueue", "创建文件题目索引任务。", "POST", "/materials/:id/index", model.ScopeMaterialsWrite, id, bodyB("force")),
		op("collections_list", "查询专题，可按所含套卷筛选。", "GET", "/collections", model.ScopeCollectionsRead, queryS("q pack_id"), page),
		op("collections_create", "创建专题，可同时放入套卷。", "POST", "/collections", model.ScopeCollectionsWrite, mustFields(joined(bodyS("title description"), bodyA("pack_ids")), "title")),
		op("collections_get", "读取专题和其中的套卷。", "GET", "/collections/:id", model.ScopeCollectionsRead, id),
		op("collections_update", "编辑专题标题或说明。", "PATCH", "/collections/:id", model.ScopeCollectionsWrite, id, bodyS("title description")),
		op("collections_items_add", "向专题追加套卷，已在其中的会跳过。", "POST", "/collections/:id/items", model.ScopeCollectionsWrite, id, mustFields(bodyA("pack_ids"), "pack_ids")),
		destructive(op("collections_items_replace", "替换专题中的全部套卷及顺序。", "PUT", "/collections/:id/items", model.ScopeCollectionsWrite, id, mustFields(bodyA("pack_ids"), "pack_ids"))),
		op("collections_delete", "删除专题，套卷保留。", "DELETE", "/collections/:id", model.ScopeCollectionsWrite, id),
		op("uploads_list", "分页读取导入批次。", "GET", "/uploads", model.ScopeUploadsWrite, page),
		op("uploads_create", "创建文件上传清单；提供 sha256 时已存在的文件无需再传（状态 existing）。hints 是可选的批次提示。", "POST", "/uploads", model.ScopeUploadsWrite, mustFields(joined(bodyS("title"), customField("hints", "object", hints), customField("entries", "array", entries)), "entries")),
		op("uploads_get", "读取当前用户的导入进度。", "GET", "/uploads/:id", model.ScopeUploadsWrite, id),
		special(op("uploads_file_put", "上传不超过 512 KiB 的 Base64 文件；较大文件使用流式上传。", "PUT", "/uploads/:id/files/:entry_id", model.ScopeUploadsWrite, pathFields("id entry_id"), mustFields(bodyS("data_base64"), "data_base64")), "upload"),
		special(op("uploads_transfer_info", "取得导入条目的流式传输路径。", "PUT", "/uploads/:id/files/:entry_id", model.ScopeUploadsWrite, pathFields("id entry_id")), "upload_info"),
		op("uploads_finalize", "完成上传并创建归档任务；归档后 AI 会阅读每个文件、分组并确定编目。", "POST", "/uploads/:id/finalize", model.ScopeUploadsWrite, id),
		op("jobs_list", "按状态、类型、执行节点或关联资料、上传、副本筛选任务。", "GET", "/jobs", model.ScopeJobsRead, queryS("status kind material_id upload_id replica_id node_id"), page),
		op("jobs_stats", "统计任务总数及状态。", "GET", "/jobs/stats", model.ScopeJobsRead),
		op("jobs_get", "读取任务进度与日志。", "GET", "/jobs/:id", model.ScopeJobsRead, id),
		op("jobs_retry", "重试任务。", "POST", "/jobs/:id/retry", model.ScopeJobsWrite, id),
		op("jobs_retry_failed", "重新排队所有失败任务，可用 kind 只重试一类（analyze/group/decide/entity/replicate/index/ingest）。", "POST", "/jobs/retry-failed", model.ScopeJobsWrite, bodyS("kind")),
		op("storage_drives_list", "读取网盘及其能力、副本统计。", "GET", "/storage/drives", model.ScopeStorageRead),
		op("storage_drives_create", "添加网盘；网盘只描述连接方式，上传什么由同步规则决定。", "POST", "/storage/drives", model.ScopeStorageWrite, mustFields(joined(drive, bodyS("kind")), "name kind")),
		op("storage_drives_update", "更新网盘设置；config 与 secret 提供时整体替换。", "PATCH", "/storage/drives/:id", model.ScopeStorageWrite, id, drive),
		op("storage_drives_test", "测试网盘连接。", "POST", "/storage/drives/:id/test", model.ScopeStorageWrite, id),
		op("storage_rules_list", "读取有序同步规则，最后一条为兜底规则。", "GET", "/storage/rules", model.ScopeStorageRead),
		op("storage_rules_preview", "预览草稿规则会产生的上传、移动、规则外和跳过动作，返回保存所需的 preview_token。", "POST", "/storage/rules/preview", model.ScopeStorageWrite, mustFields(customField("rules", "array", rules), "rules"), page),
		destructive(op("storage_rules_replace", "整体替换同步规则；必须带预览返回的 preview_token。", "PUT", "/storage/rules", model.ScopeStorageWrite, mustFields(joined(customField("rules", "array", rules), bodyS("preview_token")), "rules preview_token"))),
		op("storage_rules_explain", "解释某个文件命中哪条规则，以及各网盘的期望路径、实际路径和状态。", "GET", "/storage/rules/explain", model.ScopeStorageRead, mustFields(queryS("material_id"), "material_id")),
		op("storage_replicas_list", "按状态（含 drift 路径偏离）或网盘筛选副本。", "GET", "/storage/replicas", model.ScopeStorageRead, queryS("status drive_id"), page),
		destructive(op("storage_replicas_bulk", "批量处理副本：retry 重新计划；delete_remote 删除规则外副本的远端文件；forget 只移除规则外副本记录。", "POST", "/storage/replicas/bulk", model.ScopeStorageWrite, mustFields(joined(bodyA("ids"), bodyS("action")), "ids action"))),
		op("materials_sync", "按当前编目立即为文件生成同步计划。", "POST", "/materials/:id/sync", model.ScopeStorageWrite, id),
		op("storage_reconcile", "按当前规则重新计划所有已编目文件。", "POST", "/storage/reconcile", model.ScopeStorageWrite),
		op("storage_replica_share", "创建或刷新副本分享。", "POST", "/storage/replicas/:id/share", model.ScopeStorageWrite, id),
		op("nodes_list", "读取主端和从端节点的状态、心跳、CPU、内存、网络与任务数。", "GET", "/nodes", model.ScopeNodesManage),
		op("nodes_create", "登记一个从端节点；槽位、缓存和 OCR 服务由主端配置，返回只显示一次的接入令牌。", "POST", "/nodes", model.ScopeNodesManage, mustFields(joined(bodyS("name"), bodyI("cache_max_bytes"), bodyA("ocr_provider_ids"), customField("slots", "object", nodeSlots)), "name")),
		op("nodes_update", "更新节点名称、启停、排空状态或主端下发的运行配置。", "PATCH", "/nodes/:id", model.ScopeNodesManage, id, joined(bodyS("name"), bodyB("enabled draining"), bodyI("cache_max_bytes"), bodyA("ocr_provider_ids"), customField("slots", "object", nodeSlots))),
		op("nodes_metrics", "读取节点最近 1 小时的资源指标。", "GET", "/nodes/:id/metrics", model.ScopeNodesManage, id),
		op("cluster_settings_get", "读取主端控制的集群调度及默认运行参数。", "GET", "/cluster/settings", model.ScopeNodesManage),
		op("cluster_settings_update", "更新主端并发、可执行任务类型、租约、心跳、领取等待和轮询参数；从端在下一次心跳应用。", "PATCH", "/cluster/settings", model.ScopeNodesManage, bodyA("master_kinds"), bodyI("master_ingest_concurrency master_replicate_concurrency master_analyze_concurrency master_index_concurrency lease_seconds heartbeat_seconds claim_wait_seconds poll_seconds")),
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
	if o.special == "download" || o.special == "pull_cache_download" {
		view, err := s.svc.GetMaterial(c.Request.Context(), args["id"].(string))
		if err != nil {
			return nil, err
		}
		return map[string]any{"method": "GET", "path": path, "file_name": view.Material.FileName, "media_type": view.Material.MediaType, "size_bytes": view.Material.SizeBytes}, nil
	}
	if o.special == "pack_download" {
		view, err := s.svc.GetPack(c.Request.Context(), args["id"].(string))
		if err != nil {
			return nil, err
		}
		return map[string]any{"method": "GET", "path": path, "file_name": view.Pack.Title + ".zip", "media_type": "application/zip"}, nil
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
