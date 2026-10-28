# 高中资料归档系统技术架构

版本 1.3.0 · 2026-10-28。本文描述当前 `backend/` 与 `web/` 代码的真实分层、数据流、状态机、运行方式和边界。接口字段以 [API.md](API.md) 为准，表结构与备份以 [DATA.md](DATA.md) 为准，启动步骤见 [README](../README.md)，文档索引见 [README.md](README.md)。

本文不把协议测试、模拟上游或本地 SQLite 测试写成真实云盘、MinerU、PaddleOCR、模型网关或 PostgreSQL 的联调结论。

## 1. 系统定位

面向高中资料的上传、归档、多点存储、AI 辅助编目和纯文本搜题系统：

- **归档**：PDF、DOC、DOCX、PPTX、文件夹和 ZIP；资料是一份逻辑文件，原件按 SHA-256 去重。
- **存储**：本地、WebDAV、蓝奏云、123 云盘适配器；一个 Blob 可在多个端点各保存一份副本。
- **编目**：学科、年级、学年、标签、角色和合集摆放；角色来自资料本身，不来自合集条目。
- **AI**：可切换的 MinerU/PaddleOCR 解析文档，OpenAI 兼容模型生成编目建议；建议必须人工应用。
- **搜题**：索引任务把试卷切成题目并建立可移植倒排表，按题干文本检索并返回页码。
- **Agent**：用受限 API Key 换取短期 JWT，再以单一 Bearer JWT 访问 REST 和 `/mcp` 只读工具。

非目标：在线编辑文档、执行宏、病毒扫描、跨请求分片续传、云盘物理删除、本地 OCR、通用全文检索、跨资料同题聚合。

## 2. 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go（`go.mod` 声明 1.26；本项目在 Go 1.27.2 上验证）、Gin 1.10、GORM 1.31 |
| 数据库 | 默认 SQLite（`glebarez/sqlite` 纯 Go 驱动）；可切换 PostgreSQL（`gorm.io/driver/postgres`） |
| 后端基础库 | `golang-jwt/jwt/v5`、`golang.org/x/crypto`（argon2id）、`golang.org/x/text`（ZIP 文件名解码） |
| 前端 | React 19、Ant Design 6、React Router 8、TanStack Query 5、Vite 8 |
| 前端语言/样式/测试 | TypeScript 7 strict、UnoCSS 66（Tailwind v4 兼容 preset）、Vitest 5 |
| 运行形态 | 每实例一个 Go 进程：HTTP/MCP + 后台 worker；PostgreSQL 支持多实例；前端构建为 `web/dist` 静态文件，由同一进程从磁盘托管 |
| 外部依赖 | OpenAI 兼容端点、MinerU v4/PaddleOCR 异步解析 API、各存储端点 |

前端只使用 HTTP API，不直接访问数据库、本地文件或存储凭据。

## 3. 运行时拓扑

```
浏览器（React SPA）
  │  /api/v1/*、/mcp（同源；开发模式由 Vite 代理到 127.0.0.1:8080）
  ▼
┌──────────────── 单个 Go 进程 ─────────────────────────────────────┐
│ Gin Router                                                         │
│  ├─ 认证 / scope / Agent JWT                                      │
│  ├─ REST handlers ──┐                                              │
│  └─ MCP JSON-RPC ───┤                                              │
│                     ▼                                              │
│              service（业务与事务边界）                             │
│                ├─ GORM ─────────── SQLite / PostgreSQL             │
│                ├─ blob Store ───── DATA_DIR/blobs（CAS）           │
│                ├─ storage ──────── local / webdav / lanzou / 123pan │
│                └─ ai ─────── OpenAI 兼容 / MinerU / PaddleOCR        │
│                                                                    │
│ 按类型的有界 worker 池 ← jobs 表（pending + lease）                 │
│   ├─ ingest / replicate / analyze / index                          │
│   ├─ PostgreSQL SKIP LOCKED；SQLite BEGIN IMMEDIATE                │
│   └─ 心跳续租 + 过期回收 + 同资料 job_locks 互斥                    │
└────────────────────────────────────────────────────────────────────┘
```

没有消息队列、Redis 或独立 worker 服务。任务状态、重试、租约和启动恢复全部在数据库中完成。PostgreSQL 支持多实例领取；SQLite 仍是单实例。

## 4. 代码分层

### 4.1 后端

| 包/目录 | 职责 | 约束 |
| --- | --- | --- |
| `cmd/server` | 读取配置、打开数据库、构造依赖、启动 worker 和 HTTP 服务、优雅退出 | 只做装配，不放业务规则 |
| `internal/config` | 环境变量校验；错误只指出变量名，不回显值 | 缺 `JWT_SECRET`/`MASTER_KEY` 时启动失败 |
| `internal/db` | 打开 SQLite/PostgreSQL、设置 pragma/连接池、`AutoMigrate` | 无独立版本化迁移；迁移失败启动失败 |
| `internal/model` | GORM 表定义、枚举、JSON 文本编解码、固定学科字典 | 列表字段用 JSON 文本，保证 SQLite/PostgreSQL 通用 |
| `internal/service` | 业务用例、事务、任务状态机、存储规划、AI 和检索 | 事务内使用传入的 `tx`；网络调用不放在数据库事务里 |
| `internal/server` | Gin 路由、JSON envelope、鉴权/scope、DTO、MCP | 薄适配层；字段以 API.md 为准 |
| `internal/storage` | 存储适配器和稳定错误分类 | 不依赖 GORM/HTTP 路由；远端对象只通过 `Adapter` 端口访问 |
| `internal/ai` | OpenAI 兼容聊天、模型探测、MinerU/PaddleOCR 解析 | 上游错误必须脱敏，不返回密钥或完整 URL |
| `internal/blob` | 本地 CAS、临时文件、文件签名嗅探 | 扩展名不是格式判定的唯一依据 |
| `internal/archive` | ZIP 全量校验、编码还原、成员提取 | 先 `Inspect` 后 `Extract`；拒绝不安全包 |
| `internal/auth` | JWT、密码哈希、refresh/API Key 原语、登录节流、角色 scope | 不在日志或响应中输出秘密 |
| `internal/cryptobox` | AES-256-GCM 加解密封装 | 密文绑定 AAD（Provider ID 或 `ai.settings`） |
| `internal/apperr` | 领域错误到 HTTP 状态/机器码的映射 | 对外 message 必须可安全展示 |
| `internal/paths` | 相对路径校验、自然排序、忽略系统文件 | 上传清单与 ZIP 成员共用 |

依赖方向大致为 `server → service → {model, db, blob, storage, ai, archive, auth, apperr}`；`storage` 和 `ai` 不反向依赖 `service` 或 `server`。HTTP/MCP 只通过 service 执行业务，不直接拼 SQL 或访问存储。

### 4.2 前端

```
web/src/
  app/            QueryClient 等应用级配置
  layouts/        登录后的侧栏/头部布局与路由壳
  features/       按领域组织：materials、collections、uploads、jobs、storage、
                  ai、search、auth、apiKeys、users、audit、dashboard
  shared/         api client/session/errors/types、UI 状态组件、通用 lib
  styles/         全局样式
```

- 路由在 `App.tsx` 声明；`ScopeGuard` 按角色 scope 隐藏/拦截页面，服务端仍逐请求重新授权。
- 所有 API 调用经过 `shared/api/client.ts`：统一 envelope、`ApiError`、401 单飞刷新并重试一次、Blob 下载、XHR 二进制上传进度。
- TanStack Query 负责服务端状态；上传、任务、分析和索引按状态轮询，终态停止。
- 浏览器 access token 只存内存；refresh token 与用户摘要存 `localStorage`，刷新页面后先经 `/auth/me` 校验。浏览器不保存 Agent Secret Key。

## 5. 请求生命周期与鉴权

### 5.1 HTTP 处理顺序

```
请求
 → request_id 中间件（UUID，写响应头 X-Request-ID）
 → panic 恢复（500 envelope）
 → auth 中间件（Bearer JWT）
 → requireScope / rejectAgent / agentOnly
 → handler（严格 JSON 解码、参数校验）
 → service（业务/事务）
 → {"data": ..., "request_id": ...} 或 {"error": {...}, "request_id": ...}
```

- `/healthz` 不鉴权，返回裸 `{"status":"ok"}`；其余业务接口均鉴权。
- JSON 请求体上限 1 MiB；未知字段、非对象根、尾随数据、非声明字段的 `null` 会被拒绝。
- `NoRoute` 对非 API 的 GET/HEAD 从 `WEB_DIST` 提供 SPA，找不到文件时回退 `index.html`；API 路径返回 JSON 404。
- HTTP 服务器设置 `ReadHeaderTimeout=10s`、`IdleTimeout=120s`，没有设置全局 Read/WriteTimeout；长文件通过流式接口和适配器超时控制，而不是整体写超时。
- MCP 走同一个 auth 中间件，但 `agentOnly` 只接受代理 JWT；`GET/PUT/DELETE /mcp` 返回 405。

### 5.2 凭据

| 调用方 | 凭据 | 校验 |
| --- | --- | --- |
| 浏览器 | 用户名/口令 → 15 分钟 access JWT + 7 天 refresh token | JWT HS256，固定 issuer/audience；每次请求查 Session/User |
| Agent | `X-API-Key: pc_<id>.<secret>` → 15 分钟 JWT（`key_id`），后续只用 Bearer JWT | JWT HS256；每次请求按 `key_id` 查 Key/User 状态，撤销/过期立即失效 |
| MCP | 只接受代理 Bearer JWT | 无浏览器会话、无 SSE、无 session id |

- 角色：`admin`（全部 scope）、`editor`（资料/合集写、上传、任务读）、`viewer`（资料/合集/任务读）。
- Agent scope 是“用户角色 scope ∩ 密钥 scope”，且永远不能包含 `keys:manage`、`users:manage`、`ai:read`、`ai:write`；AI 与搜索设置接口另行拒绝 Agent。
- refresh token 轮换：旧 token 立即失效；检测到已轮换 token 被重放时撤销该 Session。refresh 的 7 天从登录起算，轮换不延长。
- 登录节流按用户名和客户端 IP 分别计数：5 次失败 / 15 分钟窗口，锁定 15 分钟；内存实现，进程重启清空。
- 密码使用 argon2id；API Key 只存 secret 的 SHA-256，创建时显示一次。
- 存储/AI 秘密用 `MASTER_KEY` 做 AES-256-GCM；数据库只存密文。
- 审计只写元数据（user_id/action/target_id），不写正文、令牌、Cookie、密钥或分享链接。

## 6. 领域模型与不变量

| 实体 | 表 | 关键不变量 |
| --- | --- | --- |
| User / APIKey / Session / Audit | `users`、`api_keys`、`sessions`、`audits` | 用户和密钥每次请求重新校验；Session 只存 refresh 哈希；审计只存元数据 |
| Blob | `blobs` | `sha256` 唯一；内容寻址；一个 Blob 可有多个 Material |
| Material | `materials` | 一份资料 = 一个原文件；标题与文件名合一；`status` 由副本事实决定 |
| MaterialSubject | `material_subjects` | `(material_id, code)` 主键；用于列表/搜索筛选 |
| Collection / CollectionItem | `collections`、`collection_items` | 合集中同一资料唯一；条目角色读取时取资料角色；父级禁止循环 |
| Upload / UploadEntry | `uploads`、`upload_entries` | 清单先声明；条目 `received` 需字节数、SHA、真实格式和 CAS 全部通过 |
| Job | `jobs` | 持久化队列；`kind` 为 ingest/replicate/analyze/index；租约领取，PostgreSQL 多实例、SQLite 单实例 |
| StorageProvider | `storage_providers` | kind 不可改；secret 只写不读；local 根目录由服务端固定 |
| Replica | `replicas` | `(blob_id, provider_id)` 唯一；目标在规划时冻结；上传与分享状态分离 |
| StoragePolicy | `storage_policies` | 单例 `id=1`；模式与 `minimum_replicas(1..20)` |
| AIProvider / OCRProvider / AISettings | `ai_providers`、`ocr_providers`、`ai_settings` | API Key/OCR 令牌加密；端点没有物理删除，只能停用；设置保存当前 OCR provider |
| MaterialAnalysis | `material_analyses` | 每资料一行；建议永不自动应用；图片不落盘 |
| SearchSettings | `search_settings` | 单例；默认启用、500 页、10 页/批、12000 字/批 |
| MaterialText | `material_texts` | 每资料一行；按文件 SHA + 解析器地址/缓存版本校验 |
| QuestionBlock / QuestionTerm | `question_blocks`、`question_terms` | 题目带页码和文档顺序；倒排主键 `(term, question_id)` |
| MaterialIndexState | `material_index_states` | 每资料一行；成功/失败/重建状态，不阻塞归档 |

### 6.1 资料、Blob 与副本

- 原件的权威内容在 `DATA_DIR/blobs/<sha[2]>/<sha>`；上传/ZIP 展开先写入 CAS，再在一个数据库事务中发布资料、合集、副本计划和任务。
- 多个 Material 可以共享同一 Blob。副本属于 Blob，因此共享同一 Blob 的资料会看到同一组 Replica；资料 `status` 也按 Blob 的副本事实刷新。
- Material 的 `file_name` 是下载名和本地副本名；修改 `title` 会同步 `file_name` 并重命名本地端点副本。云盘适配器没有改名能力，远端对象保留上传时的名字。
- 新副本使用工作时的 `file_name`；远端对象 key 仍是内容寻址前缀 + 安全化文件名。

### 6.2 合集条目

- 合集只负责摆放；条目没有独立角色。写入时忽略客户端 `role`，读取时用资料当前角色覆盖。
- 资料角色变化时，服务层会把仍等于旧默认角色的条目同步为新角色；显式不同于旧值的条目不会被覆盖。读取端仍以资料角色为准。
- `position` 显式保存，上传和 ZIP 展开按自然文件名顺序生成；合集替换接口整体替换条目，不删除资料或副本。

### 6.3 状态机

```
Upload:   pending ──finalize──> processing ──ingest ok──> completed
                                     └──── ingest fail ──> failed

UploadEntry: pending ──PUT 校验通过──> received

Job:      pending ──claim──> running ──成功──> succeeded
                               └────失败──> failed ──retry──> pending（同 id）

Replica:  pending ──claim──> uploading ──上传成功──> ready
                               └────失败──> failed ──retry──> pending
          分享单独记录：pending | ready | unsupported | failed

Material: pending_storage ──ready 副本数 >= minimum_replicas──> archived
                    └── 0 ready、0 pending/uploading、至少 1 failed ──> degraded
                    其他不足情况保持 pending_storage

Analysis: pending ──> running ──> succeeded | failed
Index:    pending ──> running ──> succeeded | failed
```

`RecoverInterrupted` 在 SQLite 启动时把遗留的 `running` Job、`uploading` Replica、`running` Analysis 和索引状态改回 `pending`；PostgreSQL 只回收租约已过期的任务，不干扰其他健康实例。随后才启动 worker。进程取消（关机）时四类任务都会在租约仍然有效时回到 pending。

## 7. 上传与归档流水线

```
选择文件（浏览器）
  → POST /uploads（清单：相对路径、字节数、可选 SHA）
  → 逐文件 PUT /uploads/{id}/files/{entry_id}（原始字节流）
      流写 DATA_DIR/tmp → 字节数校验 → 真实格式嗅探 → SHA 计算 → CAS
  → POST /uploads/{id}/finalize（幂等，创建 ingest Job）
  → worker：ZIP 校验/展开 → 构造资料与合集计划
  → 单个数据库事务：Blob/Material/Subject/Collection/Item/Replica plan/
                     analyze/index 自动任务/Upload 状态/Job 状态/审计
  → 复制任务异步上传各端点
```

### 7.1 清单与单文件接收

- 清单 1..2000 条；单文件 1 字节..512 MiB；清单总大小 ≤2 GiB；相对路径 ≤1024 UTF-8 字节。
- 路径必须为正斜杠分隔的相对路径，拒绝反斜杠、绝对路径、盘符、`.`/`..`、空段和控制字符。浏览器文件夹使用 `webkitRelativePath`。
- 允许 `.pdf`、`.doc`、`.docx`、`.pptx`、`.zip`（不区分大小写）；创建清单时先按扩展名过滤，接收时再嗅探真实内容。
- 条目按自然文件名顺序分配 position；清单可带 SHA，服务器不信任客户端 SHA，仍会重新计算并比对。
- `PUT` 支持 `Content-Length` 或 chunked；声明长度大于条目大小直接 413。字节数不符 422，真实格式与扩展名不符 415，已 finalize 的清单 409。
- 相同内容重复 PUT 返回原条目；已 received 的条目不允许替换为不同内容。
- CAS 发布和数据库标记不是同一事务；极端情况下可能留下未被引用的 CAS 文件，当前没有自动 GC。

### 7.2 ZIP 安全展开

`archive.Inspect` 在提取任何字节之前检查完整中央目录：

- 拒绝路径穿越、绝对路径/盘符、反斜杠、控制字符、空段、`.`/`..`、符号链接、特殊文件、加密条目、重复路径；
- 限制展开总大小 2 GiB、非忽略文件 5000 条、单条压缩比 200；
- 忽略 `.DS_Store`、`._*` 和 `__MACOSX`；
- 按整包候选编码（UTF-8、GB18030、Big5、Shift-JIS）打分还原文件名，而不是写死 GBK；
- 只提取 `.pdf`、`.doc`、`.docx`、`.pptx`、`.zip`；其他文件保留在原 ZIP 中，不执行/不提取；
- 嵌套 ZIP 作为成员保留，不递归展开；
- 每个提取成员的字节数、真实格式和扩展名必须一致；任何一个成员不合格就拒绝整个压缩包。

ZIP 校验/展开失败会使 ingest Job 失败，不发布任何资料；原 ZIP 仍留在 CAS 中（供重试或人工处理）。

### 7.3 合集拓扑

| 输入 | 结果 |
| --- | --- |
| `collection_kind=custom`，单个普通文件，路径无 `/` | 不创建合集，只创建一份 Material |
| `collection_kind=zip`，清单只有一个 ZIP | 创建一个合集；条目包含 ZIP 原件和展开成员（无子合集） |
| 其他批次（文件夹、多文件、或 ZIP 与其他文件混合） | 创建清单合集；每个 ZIP 另建 `kind=zip` 的子合集，ZIP 成员放进子合集；清单合集放顶层文件（及 ZIP 原件），不重复放 ZIP 成员 |
| 文件夹 | 保留浏览器给出的相对路径；成员按自然顺序排列 |

- ZIP 成员记录 `source_path = ZIP 相对路径 + "/" + 成员路径`、`parent_material_id = ZIP 原件资料 ID`。
- 资料标题由文件名去掉扩展名生成；批次/合集的标题使用清单 `title`。学科、年级、学年、标签来自清单，服务端不从文件名推断学科。
- 角色由服务端按文件名关键词给出建议，默认 `other`；归档后可在资料详情修改。合集条目自动跟随资料角色。
- 归档角色只由服务端 `suggestRole` 决定；清单不提交 `role`，上传页也不显示角色预览，避免前端提示与最终结果不一致。
- 同一事务中还写入：每个 Blob 的 Replica 目标、每个 pending Replica 的 replicate Job、自动 analyze/index 任务（若启用）、Upload 的最终 `material_id`/`collection_id`、ingest Job 成功状态和系统审计。

## 8. 多点存储与复制

### 8.1 策略

| 模式 | 写入目标 | 读取顺序 |
| --- | --- | --- |
| `mirror_all`（默认） | 所有启用的主端和备端，各一份 | 主端优先，再按 priority 升序 |
| `primary_backup` | 与 mirror_all 相同的全量双写 | 主端优先，备端兜底 |
| `balanced` | 从主端中按权重选 `minimum_replicas` 个不同端点（主端不足时取全部主端），再加所有备端 | 同上 |

- `minimum_replicas` 1..20，默认 1；本地副本也计入最低数量。
- 规划结果冻结在 Replica 行中（含 `plan_mode`）；重试/补齐不会重新随机选择已存在的目标。
- `mirror_all` 与 `primary_backup` 的写入目标完全相同，区别在读取偏好；备端同样保存完整副本。
- 目标端点不足时不会伪造 archived；`ready` 副本数达到最低值才把资料标为 `archived`。
- 停用端点不再接收新任务，但历史副本和分享信息保留；系统没有“删除唯一可用副本”的强制保护。

### 8.2 复制与分享

- 每个 `(blob_id, provider_id)` 至多一个 Replica；`queueReplicaJob` 保证同一 Replica 至多一个 pending/running Job。
- worker 打开本地 CAS 原件，调用 `Adapter.Put`；成功后才标记 ready 并记录 `verification`。
- `verification` 只声明真正做过的检查：`uploaded`（仅收到成功响应）、`size`（字节数比对）、`sha256`（完整重算）。
- 上传成功后再尝试 `Share`；分享失败不回滚副本，`share_status=failed` 且副本仍 ready；不支持分享记为 `unsupported`。
- 分享 URL、提取码、过期时间只出现在有 `materials:read` 的资料详情响应；不写日志/审计。
- 下载先看本地 CAS；缺失时按“主端优先、priority 升序、Provider ID/创建时间稳定排序”尝试 ready 副本，全部失败返回 503。
- `POST /storage/reconcile` 只按当前策略补建缺少的副本/任务，不删除额外副本；历史副本不自动迁移。
- 远端上传成功但数据库更新失败时可能残留远端对象；本地/WebDAV 使用固定对象 key，重试通常覆盖同一对象，其他适配器不保证远端幂等。

### 8.3 适配器能力

| kind | 上传 | 下载 | 分享 | 说明 |
| --- | --- | --- | --- | --- |
| `local` | 可用 | 可用 | 不支持 | 根目录固定为 `DATA_DIR/providers/<providerID>`；提供 SHA 时 verification=sha256 |
| `webdav` | 可用 | 可用 | 条件可用 | PUT/MKCOL/HEAD/GET/PROPFIND；配置 `public_base_url` 才返回公开目录拼接链接，不代表匿名可访问；提供 SHA 时上传后完整下载重算 |
| `lanzou` | 可用（非官方协议） | 可用 | 可用 | Cookie 会话；单文件 100 MiB；默认 verification=uploaded，`verify_remote_hash=true` 才做远端校验；未对真实服务联调 |
| `123pan` | 不可用 | 不可用 | 不可用 | 只实现 access_token 与 Health；Put/Open/Share 返回 unsupported（如 `123pan_upload_unverified`），Capabilities 全 false |

适配器把失败归类为稳定 `CloudError`（config/auth/timeout/network/unavailable/quota/size/hash/link/unsupported 等）；存储错误写入 Job/Replica 前会脱敏。`Retryable` 只在蓝奏内部重试逻辑中使用，服务层没有自动重试循环。

## 9. AI 识别

```
ingest 事务
  └─(analysis_enabled) 为每个非 ZIP Material 创建 analyze Job + pending Analysis

worker RunAnalyze
  → 读取 AISettings / Material
  → 取原件：本地 CAS 优先，否则从可读副本下载（≤256 MiB）
  → 不需要图片且 material_texts 命中（SHA + parse key）：复用解析文本
  → 否则按 ai_settings.ocr_provider_id（空则 priority 最小的启用项）：
       MinerU：POST /api/v4/file-urls/batch → PUT 预签名 URL
         → 轮询 /api/v4/extract-results/batch/{batch_id}（默认 15 分钟）
         → 下载 full_zip_url，读 content_list.json/full.md/页面图
       PaddleOCR：multipart POST /api/v2/ocr/jobs（model=PP-OCRv6）
         → 轮询 /api/v2/ocr/jobs/{jobId}（默认 15 分钟）
         → 下载 resultUrl.jsonUrl JSONL，提取 rec_texts
  → 写 material_texts（Markdown + 逐页文本）
  → 写 DATA_DIR/analysis/<materialID>/document.md（截断 2 MiB）
  → 按 max_pages/max_images 限制图片；图片只在内存中传给视觉模型
  → 按 priority 尝试启用的模型端点；有视觉模型时发 image_url，否则纯文本
  → 严格解析 JSON、校验字段枚举/长度/置信度，写 suggestions_json
  → 人工 POST /materials/{id}/analysis/apply 才修改资料并写审计
```

- 自动分析只对非 ZIP 容器入队；ZIP 展开成员各自入队，避免对容器重复付费。
- 分页/图片限制：`max_pages` 1..10，`max_images` 0..4；`max_pages` 限制送入视觉模型的页面图片数，OCR 仍解析全文；文本提示另有 12000 字符级别上限。PaddleOCR 不返回持久化页面图，因此视觉模型只收到文本。
- `analysis_enabled` 只控制归档后的自动入队；手动触发不依赖它。
- 图片不落盘、不提供图片读取接口；`image_count` 恒为 0。数据库里的 `images_json` 只保留空数组。
- 解析缓存按文件 SHA + `sha256("ocr-text-v2:<kind>:<base_url>:<model>")` 校验；切换 OCR 服务商、地址或模型会自然失效。MinerU 需要视觉图片时会重新解析，因为图片没有缓存。
- 模型输出是不可信数据：提示中明确要求忽略文档内指令；服务端只接受白名单字段，非法值被丢弃；建议永不自动应用。
- 失败写回 Analysis 和 Job 的脱敏错误；不影响已完成的归档、下载和副本。模型 provider 按 priority 故障切换；没有可用模型或 OCR 令牌时任务明确失败。
- 关键超时：OCR 单次解析默认 15 分钟，分析外层 16 分钟；单次模型对话 120 秒；模型探测/最小对话 30 秒；OCR 健康检查 20 秒。

## 10. 题目索引与搜题

### 10.1 索引任务

```
RunIndex
  → material_texts 命中（SHA + parse key）则复用逐页文本
  → 否则当前 OCR provider 解析一次，保存 material_texts
  → 取前 max_pages 页；超过 batch_chars/2 的长页按段落切块（重叠 400 字）
  → 每批 batch_pages（默认 10）且不超过 batch_chars（默认 12000）调用 LLM
       → 输出 page_no/question_no/text/confidence
       → 失败时对半拆批重试（保留一页重叠）
  → 校验页码属于本批、题干 5..8000 字、去重；同页同题号保留更长版本
  → 事务内删除旧题目/词项并写入新 question_blocks + question_terms
  → material_index_states 标记 succeeded 与统计
```

- 索引默认启用；归档事务内自动入队。关闭索引期间归档的资料，在服务启动时由 `EnqueueMissingIndexes` 补建（仅当索引启用）。
- ZIP 容器本身不索引；展开成员各自索引。
- 手动 `POST /materials/{id}/index` 不依赖索引开关；`force=true` 会清空 `material_texts.source_hash` 以强制重新解析。
- 失败不会删除上一次成功的索引；旧题目仍可搜索，状态显示 failed。重建成功才原子替换题目和词项。
- `question_terms` 使用 CJK unigram + bigram 和连续字母/数字词，不依赖 SQLite FTS5、PostgreSQL 扩展或向量库。
- `material_index_states` 记录页数、题目数、LLM 调用数、失败批次数和 indexed_at。

### 10.2 搜索

- 请求归一化：全角转半角、去空白/标点/符号、转小写；查询词项最多取 24 个。
- SQL 先按词项召回，限制最多 2000 个候选；只返回 `materials.status in (archived, degraded)` 的资料。
- 过滤条件：学科（`material_subjects`）、年级、学年；可选。
- 打分：词项覆盖率 ×10 + `log1p(tf 总和)` + 精确短语加分（查询 ≥4 字且命中 +6）− 长度惩罚；覆盖率低于 0.5（查询词项 >12 时 0.35）且无精确短语的候选被丢弃。
- 返回 `snippet`（约 180 字）和纯文本字符区间 `highlights`，不返回 HTML；前端自行渲染 `<mark>`。
- `limit` 默认 20，最大 50；`offset` 从 0 开始；`total` 是过滤后的命中数，不是 SQL 候选数。
- 每条结果对应一个题目块；同一题出现在多份试卷就是多条结果，不做跨资料聚合。结果可带第一个包含该资料的合集 ID。
- 搜索接口使用 `materials:read`；搜索设置使用 `search:read`/`search:write`，默认仅管理员，且拒绝 Agent。
- 搜索不检查 `material_index_states.status`，因此重建失败时旧索引仍可查询；这与“没有索引就没有结果”并不矛盾。

## 11. MCP

`POST /mcp` 是无状态 Streamable HTTP 的 JSON 子集：

- 只接受代理 Bearer JWT；`Content-Type: application/json`；服务端不强制校验 Accept，成功响应为 `application/json`，不提供 SSE。
- 单条 JSON-RPC 2.0；不支持 batch、SSE、session id。
- `initialize` 协商 2025-03-26 / 2025-06-18 / 2025-11-25；`notifications/initialized` 返回 202；`ping` 返回 `{}`；`tools/list` 按当前 scope 过滤；`tools/call` 再次检查权限。
- 工具：

| 工具 | scope | 参数 | 返回 |
| --- | --- | --- | --- |
| `materials_search` | materials:read | `q?`、`subject?`、`page?`、`page_size?` | Page\<Material> |
| `materials_get` | materials:read | `id` | Material + replicas |
| `questions_search` | materials:read | `query`、`subject?`、`grade?`、`limit?`(1..50) | 题目文本、页码、原资料 |
| `collections_list` | collections:read | `q?`、`page?`、`page_size?` | Page\<Collection> |
| `collections_get` | collections:read | `id` | Collection + items |
| `jobs_get` | jobs:read | `id` | Job |

- 工具 schema `additionalProperties=false`；缺少/未知/类型错误参数返回 -32602；解析失败 -32700、请求非法 -32600、方法未知 -32601。
- 领域失败（不存在、权限等）返回 `content=[{type:"text",...}]` 且 `isError=true`；成功 content 是 JSON 文本。
- MCP 不提供上传、删除、修改或任意 URL/路径读取；工具不能绕过 HTTP 授权。

## 12. 前端交互架构

### 12.1 页面与权限

| 路由 | 页面 | 前端 scope 守卫 |
| --- | --- | --- |
| `/login` | 登录 | 公开 |
| `/` | 仪表盘（服务端 total 汇总） | 登录 |
| `/search` | 搜题 | materials:read |
| `/materials`、`/materials/:id` | 资料列表/详情（AI 识别、题目索引、副本） | materials:read |
| `/collections`、`/collections/:id` | 合集列表/详情 | collections:read |
| `/uploads/new`、`/uploads/:id` | 上传选择/传输与归档进度 | uploads:write |
| `/jobs`、`/jobs/:id` | 任务列表/详情与重试 | jobs:read |
| `/storage` | 端点、策略、补齐副本、分享刷新 | storage:read |
| `/admin/ai-models`、`/admin/ai-settings` | AI 模型/解析设置 | ai:read |
| `/admin/api-keys`、`/admin/users`、`/admin/audit` | 密钥、用户、审计 | keys:manage / users:manage / audit:read |

前端权限只是可见性镜像；服务端是唯一授权来源。搜索设置目前只有 HTTP 接口，没有独立管理台页面。

### 12.2 上传会话

- 选择文件/文件夹/ZIP 后立即 `POST /uploads`，按文件名推断标题、合集类型、共同学科；不等待预检表单。
- 传输使用 XHR 以获得进度，最多 3 个并发；网络错误、429、5xx 自动重试 1 秒/3 秒，415/422/409/413 不重试。
- 全部条目 received 后自动 finalize；失败保留手动提交/重试入口。
- 刷新页面后 `File` 对象丢失，需要重新选择相同相对路径和字节数的文件；已接收条目跳过。
- 服务端没有上传清单列表接口，`GET /uploads/{id}` 需要 ID。

### 12.3 轮询与缓存

- 任务列表/详情：3 秒，终态停止。
- 上传会话：2 秒，完成/失败停止。
- 分析、索引：3 秒，pending/running 时轮询。
- 仪表盘：30 秒 staleTime；每个汇总独立查询，单项失败不影响整页。
- 所有写操作成功后按 query key 失效相关列表/详情；不使用本地假数据。

### 12.4 界面约定

- 主题使用 Ant Design 默认 token（默认蓝色主色、6px 圆角、默认布局背景），不硬编码品牌色；状态色经 `Tag` 的语义色表达。
- 侧栏桌面宽 240、折叠宽 64；品牌区和顶栏统一 56px 高，边框对齐；菜单按“业务/系统”分组，顶部提供折叠按钮。
- 顶栏左侧面包屑（首页 / 当前栏目），右侧用户菜单；页面标题只由内容区的 `PageHeader` 提供，不在顶栏重复。
- 内容区最大宽度 1440，桌面内边距 24、窄屏 16；列表表格统一放在 `Card` 内，表体使用无内边距容器。
- 页面只保留标题、操作、数据与必要的错误/状态提示；不放置说明性小字、页脚免责声明或重复的统计说明。

## 13. 运行、配置与数据目录

### 13.1 启动顺序

```
config.Load（校验环境变量）
 → db.Open + db.Migrate（AutoMigrate）
 → blob.NewStore（DATA_DIR/blobs、DATA_DIR/tmp）
 → cryptobox.New(MASTER_KEY)
 → auth.NewSigner(JWT_SECRET) + auth.NewLimiter
 → service.New
 → Bootstrap（空库时管理员、storage_policies、ai_settings、search_settings、默认本地端点）
 → RecoverInterrupted（running → pending）
 → EnqueueMissingIndexes（索引启用时）
 → StartWorker（单 goroutine）
 → HTTP ListenAndServe（托管 web/dist）
```

- `Bootstrap` 只在条件满足时创建默认行，可重复调用；已有用户时不会重置管理员密码。
- 默认本地端点仅在没有任何存储端点时创建，根目录 `DATA_DIR/providers/<providerID>`。
- 优雅退出信号为 SIGINT/SIGTERM；HTTP Shutdown 超时 10 秒。进程停止时正在运行的 Job 由下次启动的 `RecoverInterrupted` 处理。
- 服务不自动加载 `.env`；`DATA_DIR`、`WEB_DIST` 都以进程工作目录求绝对路径。

### 13.2 数据目录

```
DATA_DIR/
  paper.db                 默认 SQLite（可用 DATABASE_DSN 指向别处）
  blobs/<sha[0:2]>/<sha>   原件 CAS
  tmp/                     上传/展开临时文件（成功发布后移走）
  analysis/<materialID>/document.md   识别 Markdown（绝对路径写入数据库）
  providers/<providerID>/...          本地存储端点的副本
```

数据库还保存凭据密文、refresh/密钥哈希、任务、副本和分析/索引状态。数据库与文件系统/云端不是同一事务；备份和恢复见 [DATA.md](DATA.md)。

### 13.3 配置摘要

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `ADDR` | `:8080` | 监听地址 |
| `DATA_DIR` | `./data` | 数据库默认位置、CAS、临时文件、本地端点 |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DATABASE_DSN` | `DATA_DIR/paper.db` | PostgreSQL 时必填 |
| `WEB_DIST` | `../web/dist` | SPA 静态目录；不存在时仅 API |
| `ADMIN_USERNAME`/`ADMIN_PASSWORD` | 空 | 仅空库自举；密码至少 12 字符 |
| `JWT_SECRET` | 必填 | ≥32 字符，HS256 签名 |
| `MASTER_KEY` | 必填 | 32 字节 base64，AES-256-GCM |

完整说明见 [README](../README.md)；`MASTER_KEY` 丢失后已保存的存储/AI 凭据无法解密。

## 14. 安全边界

- **认证**：JWT 只接受 HS256，校验 issuer/audience/exp/nbf；每次请求重新查 Session/User/Key，撤销立即生效。
- **授权**：HTTP scope 与 MCP 工具二次检查；Agent scope 取角色与密钥交集，并禁止管理/AI scope；MCP 仅 Agent。
- **秘密**：存储/AI/OCR 凭据加密；API Key/refresh 只存哈希；日志和审计不写秘密、Cookie、分享链接或完整 URL；`redact` 处理存储与上游错误。
- **请求**：严格 JSON、1 MiB 限制、未知字段/非法 null 拒绝；MCP 无任意 URL/路径工具。
- **文件**：相对路径校验；ZIP 先全量检查再提取；拒绝遍历/符号链接/加密/炸弹/格式伪装；本地端点根目录由服务端固定，适配器再次校验路径不逃逸。
- **AI 输入**：文档内容按不可信数据处理；提示要求忽略其中指令；模型输出只作建议，人工应用才改资料。
- **浏览器**：同源部署，未实现 CORS；access token 只在内存，refresh 存 localStorage；浏览器不保存 Agent 密钥。
- **限流**：登录按用户名/IP 内存节流；API Key 与上传没有全局限流，依赖部署层或反向代理。

## 15. 一致性、失败与运维事实

- **事务**：资料发布、合集条目替换、副本计划/状态刷新、分析/索引最终写入、策略/密钥更新均在数据库事务中；网络上传、OCR、模型调用不在事务内。
- **非事务副作用**：CAS、临时文件、远端对象和数据库更新不能原子提交。失败可能留下孤儿 CAS、远端对象或已上传但未记库的副本；当前没有自动 GC/远端删除。
- **幂等**：finalize 对同一上传返回同一 Job；ingest 发布是单事务；Replica 规划复用已有行；Analysis/Index 重跑覆盖自身结果。
- **恢复与领取**：SQLite 启动时将遗留 `running` 改回 `pending`；PostgreSQL 仅回收过期租约。worker 按类型领取 pending Job，`attempts` 递增。
- **租约与并发**：心跳续租；终态更新必须匹配 `lease_owner`，过期任务的旧执行者写入会被拒绝。同一资料的 analyze/index 由 `job_locks` 串行化，不同资料可并行。
- **取消**：ingest/replicate/analyze 在进程 context 取消时回到 pending；index 取消当前记为 failed（可重建）。HTTP 客户端断开只取消对应的上传请求，不影响已入队任务；远端请求受适配器自身超时约束。
- **部署边界**：SQLite 单连接、单 writer、WAL，只支持单实例。PostgreSQL 支持多实例；内置 Compose 运行两个应用实例，并共享 `DATA_DIR`（CAS 与本地存储）。schema 由一次性 `migrate` 服务创建，应用设置 `DB_AUTO_MIGRATE=false`。
- **增长**：`material_texts`、`question_terms`、`material_analyses`、`audits`、`jobs` 和孤儿 CAS 会持续增长；没有保留期/清理任务。`AutoMigrate` 不会删除旧表/旧列。
- **迁移**：没有版本化 migration；升级前先备份，启动时由 `AutoMigrate` 增量建表/加列。已删除功能留下的表和列不会自动清理。

## 16. 验证状态（2026-10-28）

本次实际执行并通过：

```bash
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/service)
(cd backend && go build ./cmd/server)
(cd web && npm run typecheck)
(cd web && npm test)          # 10 个测试文件、32 个用例
(cd web && npm run build)
docker compose build
docker compose up -d
```

覆盖范围：

- 后端单元/协议测试：归档编码、存储适配器（local/WebDAV/蓝奏/123）、AI/MinerU/PaddleOCR 协议、搜索文本/高亮、迁移幂等。
- 后端集成测试（临时 SQLite + httptest 模拟上游）：上传→归档→ZIP 展开→合集→本地副本；恶意 ZIP 拒绝；直传单文件/文件夹/ZIP；分析/索引共享解析；Agent 单一 Bearer/MCP/撤销；未鉴权 401；删除级联。
- 前端：typecheck、10 个测试文件 32 个用例、生产构建。
- PostgreSQL 部署：`postgres:16-alpine` 一次性迁移、两个应用实例、Nginx 单端口网关均健康；旧 SQLite 的账号、资料、Blob、任务与设置已复制，登录、`/auth/me` 和资料列表实测成功。租约竞争自动化测试仍以临时 SQLite 执行，未在生产库注入合成任务。

未验证（不得写成已通过）：

- 真实蓝奏云、123 云盘、WebDAV 的联网上传/下载/分享；
- 真实 MinerU/PaddleOCR 账号、真实 OpenAI 兼容网关的质量、限流和费用；
- 浏览器逐页人工操作、真实 Agent 客户端、真实大文件/长文档；
- 任务运行指标、租约告警与按上游配额动态限流。

## 17. 已知边界与后续方向

1. **多实例运维**：为各实例设置唯一 `WORKER_INSTANCE_ID`，共享 `DATA_DIR`，监控过期租约和任务失败率。
2. **真实存储**：WebDAV/蓝奏需要真实端点联调；123 云盘在官方接口可核对前保持 unsupported，不应作为可上传端点启用。
3. **远端删除/改名**：适配器没有删除接口；改名只同步本地副本。需要远端生命周期管理时另设能力与审计。
4. **AI/索引质量**：真实试卷的 OCR 纠错、切题、页码准确率和大文件成本未系统验证；分析与索引可继续共享解析缓存。
5. **数据治理**：缺少孤儿 CAS 清理、过期任务/缓存保留期和版本化迁移。
6. **安全加固**：管理员可配置任意 http(s) 上游地址；未实现出站 SSRF 允许列表、全局 API 限流或密钥轮换流程。
7. **产品能力**：不提供独立本地 OCR、通用全文检索、Office 转换、病毒扫描、跨请求分片断点续传、Range 下载、跨资料同题聚合。
8. **搜索设置**：目前只有 HTTP 接口，没有独立管理台页面；索引状态只使用 pending/running/succeeded/failed。

## 18. 文档关系

- [README.md](../README.md)：上手、配置、能力边界。
- [README.md](README.md)：当前文档索引与维护约定。
- [API.md](API.md)：逐接口合同、字段、状态、错误。
- [DATA.md](DATA.md)：表清单、数据目录、备份/恢复、增长控制。
