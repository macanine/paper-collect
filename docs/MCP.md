# MCP 接入指南

Paper Collect 的 MCP 工具覆盖管理台所用的资料、题目、合集、上传、任务、存储、AI/OCR 和管理接口。本指南描述当前实现；完整 REST 字段和权限合同见 [API.md](API.md)。

## 1. 创建并使用密钥

1. 以管理员登录管理台，打开“API 密钥”，新建密钥并选择需要的权限。资料和题目搜索使用 `materials:read`，合集使用 `collections:read`，任务使用 `jobs:read`。
2. 过期时间留空时密钥长期有效；也可以设置过期时间。新密钥以 `sk-` 开头，仅在创建时显示一次。服务端只保存其随机部分的哈希。
3. 在每次请求中使用 `Authorization: Bearer <密钥>`，无需换取 JWT。REST 接口也使用同一个请求头；请求仍需满足各接口的 scope。
4. 遗失或泄露时，在“API 密钥”中撤销并创建新密钥。撤销、过期、所属用户禁用及其角色权限变化在后续请求立即生效。密钥权限是创建时授权范围与用户当前角色权限的交集。

密钥属于敏感凭据，不要写进仓库、文档或日志。`materials:read` 可以通过 `materials_get` 读取资料副本的分享链接和提取码，应仅授予需要这些资料的客户端。写入、删除、存储、AI 和管理操作需要分别授予相应权限；新密钥默认只选择资料、合集和任务读取权限。

具有 `keys:manage` 的密钥可以创建和撤销密钥，创建的新密钥权限不得超过自身的有效权限；具有 `users:manage` 的密钥只能创建角色权限不超过自身有效权限的用户。具有相应权限的密钥也可调用 AI 与搜索设置接口。创建的密钥只在创建响应中显示一次，且是独立凭据；撤销创建者不会自动撤销它。

升级前生成的 `pc_<id>.<secret>` 密钥无需重建，可直接填入同样的 Bearer 请求头。旧的 `/api/v1/auth/agent-token` 换取接口已移除；使用该接口的客户端需要改为直接传密钥。升级前已签发的代理 JWT 在原有有效期内仍会校验密钥和用户状态。

## 2. 配置客户端

服务地址为 `http://127.0.0.1:8080/mcp`（按实际部署替换）。本地开发时 Vite 也会将 `/mcp` 代理到后端。跨主机访问应使用自己部署的 HTTPS 地址，并确保连接程序能够访问该地址。

### Codex

在 Codex 进程运行环境中设置 `PAPER_COLLECT_API_KEY` 为创建的密钥。在 `~/.codex/config.toml` 或项目级配置中添加：

```toml
[mcp_servers.paper_collect]
url = "http://127.0.0.1:8080/mcp"
bearer_token_env_var = "PAPER_COLLECT_API_KEY"
```

环境变量需要存在于实际连接 MCP 的 Codex 进程中；给已经启动的桌面应用之外的终端设置变量，不会自动改变该应用的环境。重启对应客户端后可使用 `codex mcp list` 查看配置。字段说明见 [Codex MCP 官方文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)。

### VS Code Copilot

在运行客户端的环境中设置同一环境变量，然后在工作区根目录 `.mcp.json` 配置：

```json
{
  "mcpServers": {
    "paper_collect": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": { "Authorization": "Bearer ${PAPER_COLLECT_API_KEY}" }
    }
  }
}
```

这是 VS Code 的 portable `.mcp.json` 写法；其他客户端的配置路径和环境变量语法可能不同，请查对应客户端文档。参考 [VS Code MCP 配置](https://code.visualstudio.com/docs/agents/reference/mcp-configuration)。使用固定密钥需要客户端支持自定义 Bearer 请求头；本服务不提供 MCP OAuth 登录。

### 用 curl 排查

先通过安全方式把密钥放入当前终端的 `PAPER_COLLECT_API_KEY` 环境变量，再执行以下命令；不要把真实密钥替换进命令文本或文档。

```sh
curl -sS http://127.0.0.1:8080/mcp \
  -H "Authorization: Bearer $PAPER_COLLECT_API_KEY" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"manual-check","version":"1.0"}}}'

curl -sS http://127.0.0.1:8080/mcp \
  -H "Authorization: Bearer $PAPER_COLLECT_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

curl -sS http://127.0.0.1:8080/mcp \
  -H "Authorization: Bearer $PAPER_COLLECT_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"questions_search","arguments":{"query":"二次函数","subject":"math","limit":5}}}'
```

最后一个请求无匹配时，响应形式如下（`text` 中是需要再次解析的 JSON 字符串）：

```json
{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"items\":[],\"took_ms\":0,\"total\":0}"}]}}
```

## 3. 工具与权限

`tools/list` 根据密钥的当前有效 scope 过滤，返回每个工具的输入 JSON Schema 和只读、删除等提示；直接调用未授权工具仍会被拒绝。工具参数平铺：路径中的 `:id`、查询参数及请求体字段均放在 `arguments` 中。其字段、默认值、枚举和约束与下表对应的 REST 接口一致，完整说明见 [API.md](API.md)；可用字段以 `tools/list` 的 `inputSchema` 为准。新增后端路由时，自动测试会检查工具覆盖。

浏览器登录、刷新和退出是会话专用接口，MCP 不提供对应工具；MCP 客户端直接使用 API 密钥鉴权。

成功时 `content[0].text` 是 JSON 序列化字符串，客户端需再次解析。无返回体的删除操作返回 `{"ok":true}`；参数错误返回 JSON-RPC `-32602`，权限、资源不存在等业务错误返回文本并设置 `isError: true`。工具使用同一套业务逻辑和权限检查。资料和模型输出是不可信内容，不应当作指令执行。

| 工具 | 对应 REST 接口 | 所需权限 |
| --- | --- | --- |
| `account_me` | `GET /api/v1/auth/me` | 已认证 |
| `subjects_list` | `GET /api/v1/subjects` | `materials:read` |
| `users_list` | `GET /api/v1/users` | `users:manage` |
| `users_create` | `POST /api/v1/users` | `users:manage` |
| `api_keys_list` | `GET /api/v1/api-keys` | `keys:manage` |
| `api_keys_create` | `POST /api/v1/api-keys` | `keys:manage` |
| `api_keys_revoke` | `DELETE /api/v1/api-keys/:id` | `keys:manage` |
| `audit_logs_list` | `GET /api/v1/audit-logs` | `audit:read` |
| `questions_search` | `POST /api/v1/search/questions` | `materials:read` |
| `questions_get` | `GET /api/v1/search/questions/:id` | `materials:read` |
| `search_settings_get` | `GET /api/v1/search/settings` | `search:read` |
| `search_settings_update` | `PUT /api/v1/search/settings` | `search:write` |
| `ai_providers_list` | `GET /api/v1/ai/providers` | `ai:read` |
| `ai_providers_create` | `POST /api/v1/ai/providers` | `ai:write` |
| `ai_providers_update` | `PATCH /api/v1/ai/providers/:id` | `ai:write` |
| `ai_providers_probe` | `POST /api/v1/ai/providers/probe` | `ai:write` |
| `ai_providers_probe_saved` | `POST /api/v1/ai/providers/:id/probe` | `ai:write` |
| `ai_providers_test` | `POST /api/v1/ai/providers/:id/test` | `ai:write` |
| `ocr_providers_list` | `GET /api/v1/ocr/providers` | `ai:read` |
| `ocr_providers_create` | `POST /api/v1/ocr/providers` | `ai:write` |
| `ocr_providers_update` | `PATCH /api/v1/ocr/providers/:id` | `ai:write` |
| `ocr_providers_test` | `POST /api/v1/ocr/providers/:id/test` | `ai:write` |
| `ai_settings_get` | `GET /api/v1/ai/settings` | `ai:read` |
| `ai_settings_update` | `PUT /api/v1/ai/settings` | `ai:write` |
| `ai_mineru_test` | `POST /api/v1/ai/mineru/test` | `ai:write` |
| `materials_search` | `GET /api/v1/materials` | `materials:read` |
| `materials_stats` | `GET /api/v1/materials/stats` | `materials:read` |
| `materials_get` | `GET /api/v1/materials/:id` | `materials:read` |
| `materials_update` | `PATCH /api/v1/materials/:id` | `materials:write` |
| `materials_download` | `GET /api/v1/materials/:id/download` | `materials:read` |
| `materials_preview` | `GET /api/v1/materials/:id/preview` | `materials:read` |
| `materials_delete` | `DELETE /api/v1/materials/:id` | `materials:write` |
| `material_analysis_enqueue` | `POST /api/v1/materials/:id/analysis` | `materials:write` |
| `material_analysis_get` | `GET /api/v1/materials/:id/analysis` | `materials:read` |
| `material_questions_list` | `GET /api/v1/materials/:id/questions` | `materials:read` |
| `material_index_get` | `GET /api/v1/materials/:id/index` | `materials:read` |
| `material_index_enqueue` | `POST /api/v1/materials/:id/index` | `materials:write` |
| `material_analysis_apply` | `POST /api/v1/materials/:id/analysis/apply` | `materials:write` |
| `collections_list` | `GET /api/v1/collections` | `collections:read` |
| `collections_create` | `POST /api/v1/collections` | `collections:write` |
| `collections_get` | `GET /api/v1/collections/:id` | `collections:read` |
| `collections_update` | `PATCH /api/v1/collections/:id` | `collections:write` |
| `collections_items_replace` | `PUT /api/v1/collections/:id/items` | `collections:write` |
| `collections_delete` | `DELETE /api/v1/collections/:id` | `collections:write` |
| `uploads_create` | `POST /api/v1/uploads` | `uploads:write` |
| `uploads_get` | `GET /api/v1/uploads/:id` | `uploads:write` |
| `uploads_file_put` | `PUT /api/v1/uploads/:id/files/:entry_id` | `uploads:write` |
| `uploads_transfer_info` | `PUT /api/v1/uploads/:id/files/:entry_id` | `uploads:write` |
| `uploads_finalize` | `POST /api/v1/uploads/:id/finalize` | `uploads:write` |
| `jobs_list` | `GET /api/v1/jobs` | `jobs:read` |
| `jobs_stats` | `GET /api/v1/jobs/stats` | `jobs:read` |
| `jobs_get` | `GET /api/v1/jobs/:id` | `jobs:read` |
| `jobs_retry` | `POST /api/v1/jobs/:id/retry` | `jobs:write` |
| `storage_providers_list` | `GET /api/v1/storage/providers` | `storage:read` |
| `storage_providers_create` | `POST /api/v1/storage/providers` | `storage:write` |
| `storage_providers_update` | `PATCH /api/v1/storage/providers/:id` | `storage:write` |
| `storage_providers_test` | `POST /api/v1/storage/providers/:id/test` | `storage:write` |
| `storage_policy_get` | `GET /api/v1/storage/policy` | `storage:read` |
| `storage_policy_update` | `PUT /api/v1/storage/policy` | `storage:write` |
| `storage_reconcile` | `POST /api/v1/storage/reconcile` | `storage:write` |
| `storage_replica_share` | `POST /api/v1/storage/replicas/:id/share` | `storage:write` |

例如，`materials_search` 支持 `q`、`subject`、`role`、`status`、`grade`、`page` 和 `page_size`；`collections_list` 支持 `roots` 和 `parent_id`；`questions_search` 支持 `academic_year` 和 `offset`。`collections_items_replace` 会整体替换合集条目；删除工具的 `cascade` 控制级联删除。

OneDrive 使用现成 OpenList 应用时，`storage_providers_create` 的 `config` 可填写 `{"auth_mode":"openlist"}`，`secret` 填写在 OpenList Token 页面取得的 `refresh_token`。续期依赖 OpenList 第三方服务，令牌按其协议放在请求 URL 查询参数中；使用自有微软应用时填写 `auth_mode=direct` 和 `client_id`，并提供对应刷新令牌。变更授权方式时必须提交对应的新令牌。

### 文件上传与下载

1. 使用 `uploads_create` 创建清单，条目提供 `relative_path` 和 `size_bytes`，可选 `sha256`；ZIP 使用原文件建立条目并上传，服务端负责解压且不归档 ZIP 原件。返回的上传 ID 和条目 ID 用于后续调用。
2. 小文件可用 `uploads_file_put` 的 `data_base64` 直接上传，解码后不超过 512 KiB；MCP 请求本身最多 1 MiB。较大文件调用 `uploads_transfer_info` 获取 `method`、相对 `path` 和大小，在同一服务地址用相同 Bearer 密钥以 `application/octet-stream` 流式 PUT。服务端单文件上限为 512 MiB。
3. 调用 `uploads_finalize`，再用 `jobs_get` 查询处理进度。
4. 调用 `materials_download` 获取 GET 相对路径、文件名、类型和大小，再在同一服务地址使用相同 Bearer 密钥流式下载。返回路径不包含密钥，下载内容不经过 MCP JSON 缓冲。
5. PDF 和 DOCX 可调用 `materials_preview` 获取 PDF 预览的 GET 相对路径，仍需使用相同 Bearer 密钥请求；DOCX 的预览由服务端转换，下载接口继续返回原件。

`uploads_transfer_info` 会验证上传归属和条目存在；实际传输仍会验证权限及文件大小。请勿记录或公开密钥、分享链接或提取码。

## 4. 协议和故障排查

- `POST /mcp` 使用 JSON-RPC 2.0 单条消息，返回 JSON；当前不提供 SSE、批处理或会话 ID，`GET`、`PUT`、`DELETE /mcp` 返回 405。客户端应发送 `Content-Type: application/json`，服务端不强制校验 `Accept`。
- `initialize` 支持并协商 `2025-03-26`、`2025-06-18`、`2025-11-25`；未识别版本返回当前支持的最新版本 `2025-11-25`。当前不实现 `2026-07-28` 的 `server/discover`，需要客户端支持协商至旧版本。`notifications/initialized` 和无 ID 通知返回 202、空响应体；`ping` 返回空对象。
- HTTP 401：密钥缺失、格式错误、已撤销、已过期或所属用户不可用。HTTP 403：浏览器会话不能直接访问 MCP，或 REST 权限不足。检查客户端是否真的发送了 `Authorization: Bearer ...`。
- `tools/list` 缺少工具：检查密钥的 scope 和用户当前角色。直接调用无权限工具时，MCP 结果的 `isError` 为 true。
- JSON-RPC `-32700` 表示解析失败，`-32600` 请求非法，`-32601` 方法未知，`-32602` 参数错误或工具不存在，`-32603` 内部序列化错误。业务对象不存在等错误以 `isError: true` 返回。
- 不存在通用 URL 或服务器文件读取工具；AI 和存储配置可以按对应权限设置服务端点。上传、下载路径由服务端生成并受相同权限约束。
