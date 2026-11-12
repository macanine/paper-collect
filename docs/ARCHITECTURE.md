# 高中资料归档系统技术架构

版本 1.7.0 · 2026-11-05。本文描述当前 `backend/` 与 `web/` 代码的真实分层、数据流、状态机、运行方式和边界。接口字段以 [API.md](API.md) 为准，表结构与备份以 [DATA.md](DATA.md) 为准，启动步骤见 [DEPLOYMENT.md](DEPLOYMENT.md)，文档索引见 [README.md](README.md)。

本文不把协议测试、模拟上游或本地 SQLite 测试写成真实云盘、MinerU、PaddleOCR、模型网关或 PostgreSQL 的联调结论。自建 RapidOCR 曾于 2026-10-28 在远端 Compose 用真实扫描 PDF 验证（套卷重构前）。

## 1. 系统定位

面向高中资料的上传、归档、多点存储、AI 辅助编目和纯文本搜题系统：

- **归档**：PDF、DOC、DOCX、PPTX、文件夹和 ZIP；资料是一份逻辑文件，原件按 SHA-256 去重。
- **网盘同步**：原件只以本机 CAS 为准；本地目录、WebDAV、蓝奏云、OneDrive 和 S3 网盘按有序同步规则接收副本，路径由模板生成为可浏览的目录树，先预览再保存（123 云盘仅探测连接）。
- **编目**：套卷聚合同一来源的文件，编目字段（学科、年级、届别、地区、机构、标签）在套卷上；文件只记录部件（试题、答案、教师版、答题卡、听力、正文）。专题把套卷按复习主题重新组合；删除的套卷进回收站保留 30 天。
- **AI**：可切换的自建 RapidOCR、MinerU/PaddleOCR 解析文档，OpenAI 兼容模型生成编目建议；通过白名单校验后自动应用并更新文件名，人工可覆盖。
- **搜题**：索引任务把试卷切成题目并建立可移植倒排表，按题干文本检索并返回页码。
- **Agent**：用 API 密钥直接访问 REST 和 `/mcp` 中按 scope 授权的工具。

非目标：在线编辑文档、执行宏、病毒扫描、跨请求分片续传、自动删除网盘文件、通用全文检索、跨资料同题聚合。

## 2. 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go（`go.mod` 声明 1.26；最近一次在 Go 1.27.3 上验证）、Gin 1.10、GORM 1.31 |
| 数据库 | 默认 SQLite（`glebarez/sqlite` 纯 Go 驱动）；可切换 PostgreSQL（`gorm.io/driver/postgres`） |
| 后端基础库 | `golang-jwt/jwt/v5`、`golang.org/x/crypto`（argon2id）、`golang.org/x/text`（ZIP 文件名解码） |
| 前端 | React 19、Mantine 9、React Router 8、TanStack Query 5、Vite 8 |
| 前端语言/样式/测试 | TypeScript 7 strict、Vitest 5 |
| 运行形态 | 主端一个 Go 进程：HTTP/MCP + 后台 worker；从端是同一二进制的 `ROLE=worker` 模式，不提供 HTTP 服务；前端构建为 `web/dist` 静态文件，由同一进程从磁盘托管 |
| 外部依赖 | OpenAI 兼容端点、MinerU v4/PaddleOCR 异步解析 API、各网盘；RapidOCR 为 Compose 内自建依赖 |

前端只使用 HTTP API，不直接访问数据库、本地文件或存储凭据。

## 3. 运行时拓扑

```
浏览器（React SPA）
  │  /api/v1/*、/mcp（同源；开发模式由 Vite 代理到 127.0.0.1:8080）
  ▼
┌──────────────── 单个 Go 进程 ─────────────────────────────────────┐
│ Gin Router                                                         │
│  ├─ 认证 / scope / Agent API Key                                  │
│  ├─ REST handlers ──┐                                              │
│  └─ MCP JSON-RPC ───┤                                              │
│                     ▼                                              │
│              service（业务与事务边界）                             │
│                ├─ GORM ─────────── SQLite / PostgreSQL             │
│                ├─ blob Store ───── DATA_DIR/blobs（CAS）           │
│                ├─ storage ──────── local / webdav / lanzou / 123pan / onedrive / s3 │
│                └─ ai ─────── OpenAI / MinerU / PaddleOCR / RapidOCR  │
│                                                                    │
│ 单个调度协程 ← jobs 表（pending + lease）                          │
│   ├─ 按类型空闲容量批量领取，每个任务一个协程，结束即退出          │
│   ├─ PostgreSQL SKIP LOCKED；SQLite BEGIN IMMEDIATE                │
│   └─ 合并续租 + 过期回收 + 同资料 job_locks 互斥                    │
└────────────────────────────────────────────────────────────────────┘
```

没有消息队列或 Redis。任务状态、重试、租约和启动恢复全部在主端数据库中完成，从端通过 HTTP 领取任务（见第 18 节）。调度模型见 18.3。Compose 额外运行一个 RapidOCR 内网服务，它只负责文件 OCR，不参与业务任务租约。

## 4. 代码分层

### 4.1 后端

| 包/目录 | 职责 | 约束 |
| --- | --- | --- |
| `cmd/server` | 读取配置、打开数据库、构造依赖、启动 worker 和 HTTP 服务、优雅退出；`ROLE=worker` 时启动从端 | 只做装配，不放业务规则 |
| `cmd/migrate` | 创建 schema，可把现有 SQLite 一次性复制到空 PostgreSQL | 在应用实例启动前运行 |
| `cmd/apigen` | 由 DTO 生成前端 `contract.gen.ts`（`make contract`） | 生成文件不得手改 |
| `internal/config` | 环境变量校验；错误只指出变量名，不回显值 | 缺 `JWT_SECRET`/`MASTER_KEY` 时启动失败 |
| `internal/db` | 打开 SQLite/PostgreSQL、设置 pragma/连接池、`AutoMigrate` | 无独立版本化迁移；迁移失败启动失败 |
| `internal/model` | GORM 表定义、枚举、JSON 文本编解码、固定学科字典 | 列表字段用 JSON 文本，保证 SQLite/PostgreSQL 通用 |
| `internal/service` | 业务用例、事务、任务状态机、同步规则与副本规划、AI 和检索；文件按领域分组，见 `service/doc.go` | 事务内使用传入的 `tx`；网络调用不放在数据库事务里 |
| `internal/jobexec` | 复制、阅读、索引、分组、编目、名称任务的无数据库执行部分；主端与从端共用 | 不得访问数据库；输入自包含，结果由主端在一个事务中提交 |
| `internal/worker` | 从端进程：领取任务、用 `jobexec` 执行、上报结果、本机原件缓存 | 凭据只随任务下发、只存内存 |
| `internal/catalogai` | AI 编目的提示词、回答解析、校验与名称归一 | 不访问数据库；主从使用同一套校验规则 |
| `internal/searchtext` | 题目文本归一、n-gram 词项、公式规范化与片段高亮 | 纯文本处理，不访问存储 |
| `internal/sysstat` | 主机与进程资源采样，用于节点心跳 | 只读本机指标 |
| `internal/server` | Gin 路由、JSON envelope、鉴权/scope、DTO、MCP | 薄适配层；字段以 API.md 为准 |
| `internal/storage` | 存储适配器和稳定错误分类 | 不依赖 GORM/HTTP 路由；远端对象只通过 `Adapter` 端口访问 |
| `internal/ai` | OpenAI 兼容聊天、模型探测、MinerU/PaddleOCR/RapidOCR 解析 | 上游错误必须脱敏，不返回密钥或完整 URL |
| `internal/ooxml` | OPC/ZIP 包校验、内部路径与关系解析 | 读取前完成路径、加密、外部引用和总量校验 |
| `internal/docx` | 直接解析 Word 正文、标题、表格、文本框、公式、分页和嵌入图片 | 不依赖 LibreOffice、PDF 或 OCR 文本层；输出有界 |
| `internal/office` | DOCX→PDF 预览转换与内容寻址缓存 | 只服务预览；单进程单转换槽，复用 `ooxml` 校验 |
| `internal/blob` | 本地 CAS、临时文件、文件签名嗅探 | 扩展名不是格式判定的唯一依据 |
| `internal/archive` | ZIP 全量校验、编码还原、成员提取 | 先 `InspectWithSkipped` 后 `Extract`；拒绝不安全包 |
| `internal/auth` | JWT、密码哈希、refresh/API Key 原语、登录节流、角色 scope | 不在日志或响应中输出秘密 |
| `internal/cryptobox` | AES-256-GCM 加解密封装 | 密文绑定 AAD（Provider ID 或 `ai.settings`） |
| `internal/apperr` | 领域错误到 HTTP 状态/机器码的映射 | 对外 message 必须可安全展示 |
| `internal/paths` | 相对路径校验、自然排序、忽略系统文件 | 上传清单与 ZIP 成员共用 |

依赖方向大致为 `server → service → {jobexec, catalogai, searchtext, model, db, blob, storage, ai, archive, auth, apperr, ooxml, docx, office}`，`worker → jobexec`；`docx` 只依赖 `ooxml`，`office` 只依赖 `ooxml`，`storage` 和 `ai` 不反向依赖 `service` 或 `server`。HTTP/MCP 只通过 service 执行业务，不直接拼 SQL 或访问存储。

### 4.2 前端

```
web/src/
  app/            路由清单 routes.tsx、查询键 queryKeys.ts、QueryClient、主题令牌
  layouts/        登录后的侧栏/顶栏布局、命令面板
  features/<领域>/ 页面组件 + api.ts（HTTP 调用）+ queries.ts（TanStack Query 钩子）+ 纯函数与测试
                  领域：packs、materials、inbox、collections、uploads、trash、jobs、storage、
                  nodes、ai、search、auth、apiKeys、users、audit、dashboard、settings
  shared/api/     client、session、errors、请求体类型 types.ts、生成的 contract.gen.ts
  shared/lib/     跨领域的格式化、标签与选项、列表查询参数
  shared/ui/      跨领域的状态组件、页头、标签
  styles/         全局样式（只处理布局与响应式）
```

- 路由与导航在 `app/routes.tsx` 中声明一次（路径、标题、scope、按需加载的页面、侧栏分组），`App.tsx`、侧栏、命令面板和设置页都从它派生；`ScopeGuard` 按角色 scope 隐藏/拦截页面，服务端仍逐请求重新授权。
- 只被一个领域使用的代码放在该领域目录；被两个以上领域使用时才放进 `shared/`。
- 所有 API 调用经过 `shared/api/client.ts`：统一 envelope、`ApiError`、401 单飞刷新并重试一次、Blob 下载、XHR 二进制上传进度；请求、下载和上传完成后向浏览器内网络遥测采样。
- TanStack Query 负责服务端状态；上传、任务、分析和索引按状态轮询，终态停止。
- 浏览器 access token 只存内存；refresh token 与用户摘要存 `localStorage`，刷新页面后先经 `/auth/me` 校验。浏览器不保存 Agent Secret Key。

### 4.3 数据下钻与可观测

- 仪表盘资料/任务统计卡直接进入对应状态筛选；文件按 SHA-256 精确查找同内容记录（唯一），重复上传表现为来源追加而非新文件。
- 套卷、文件、任务列表及仪表盘最近记录使用整行点击导航；移动端卡片空白区域同样可点击。导航忽略行内链接、按钮、表单控件和下拉触发器，标题链接仍保留原生语义与可访问性。
- 套卷详情返回 `members`、`collections` 和最多 5 个关联任务，可进入文件详情、专题详情或完整任务筛选；专题列表支持 `pack_id` 反查，资料库支持 `material_id` 反查所属套卷。
- 需关注收件箱读取 `stage=attention` 的文件并按套卷聚合，显示 AI 编目与未通过的校验，一套资料只确认一次；回收站按套卷恢复或彻底删除，保留期 30 天。
- 任务列表支持 `upload_id`、`material_id`、`replica_id` 精确筛选；文件详情的副本可回到所属网盘，任务详情保留上传、文件和副本入口。
- 浏览器网络遥测只保存在内存，最多保留最近 100 次采样；展示 P95/平均/中位延迟、成功率、传输字节、在途数量、请求耗时、响应大小和 request ID。采样不含请求正文、响应正文、凭据或查询参数；清空会重置历史聚合，但保留仍在途的请求。
- 后端对 `/api/*` 与 `/mcp` 输出一行结构化请求日志：method、path、status、duration_ms、response_bytes、request_id。日志不记录查询参数、请求正文、凭据或响应内容。

## 5. 请求生命周期与鉴权

### 5.1 HTTP 处理顺序

```
请求
 → request_id 中间件（UUID，写响应头 X-Request-ID）
 → request_log 中间件（API/MCP 耗时与响应字节）
 → panic 恢复（500 envelope）
 → auth 中间件（浏览器 Bearer JWT 或 Agent Bearer API Key）
 → requireScope / agentOnly
 → handler（严格 JSON 解码、参数校验）
 → service（业务/事务）
 → {"data": ..., "request_id": ...} 或 {"error": {...}, "request_id": ...}
```

- `/healthz` 不鉴权，返回裸 `{"status":"ok"}`；其余业务接口均鉴权。
- API/MCP 请求结束后记录 `method/path/status/duration_ms/response_bytes/request_id`；静态文件和 SPA 回退不进入该日志。查询参数和请求体不记录。
- JSON 请求体上限 1 MiB；未知字段、非对象根、尾随数据、非声明字段的 `null` 会被拒绝。
- `NoRoute` 对非 API 的 GET/HEAD 从 `WEB_DIST` 提供 SPA，找不到文件时回退 `index.html`；API 路径返回 JSON 404。
- HTTP 服务器设置 `ReadHeaderTimeout=10s`、`IdleTimeout=120s`，没有设置全局 Read/WriteTimeout；长文件通过流式接口和适配器超时控制，而不是整体写超时。
- MCP 走同一个 auth 中间件，但 `agentOnly` 只接受 API 密钥身份；`GET/PUT/DELETE /mcp` 返回 405。

### 5.2 凭据

| 调用方 | 凭据 | 校验 |
| --- | --- | --- |
| 浏览器 | 用户名/口令 → 15 分钟 access JWT + 7 天 refresh token | JWT HS256，固定 issuer/audience；每次请求查 Session/User |
| Agent | `Authorization: Bearer sk-<随机密钥>` | 索引查询密钥哈希并关联用户，每次请求检查撤销、过期和当前角色 |
| MCP | 接受代理 Bearer API 密钥 | 无浏览器会话、无 SSE、无 session id |

- 角色：`admin`（全部 scope）、`editor`（套卷与文件写、专题写、上传、任务读）、`viewer`（套卷、专题、任务读）。
- Agent scope 是“用户角色 scope ∩ 密钥 scope”。有管理权限的密钥可创建其他密钥或用户，但只能授予不超过自身有效权限的 scope 或角色。新密钥是独立凭据，撤销创建者的密钥不会自动撤销它。
- refresh token 轮换：旧 token 立即失效；检测到已轮换 token 被重放时撤销该 Session。refresh 的 7 天从登录起算，轮换不延长。
- 登录节流按用户名和客户端 IP 分别计数：5 次失败 / 15 分钟窗口，锁定 15 分钟；内存实现，进程重启清空。
- 密码使用 argon2id；API Key 只存 secret 的 SHA-256，创建时显示一次。
- 存储/AI 秘密用 `MASTER_KEY` 做 AES-256-GCM；数据库只存密文。
- 审计只写元数据（user_id/action/target_id），不写正文、令牌、Cookie、密钥或分享链接。

## 6. 领域模型与不变量

| 实体 | 表 | 关键不变量 |
| --- | --- | --- |
| User / APIKey / Session / Audit | `users`、`api_keys`、`sessions`、`audits` | 用户和密钥每次请求重新校验；Session 只存 refresh 哈希；审计只存元数据 |
| Blob | `blobs` | `sha256` 唯一；内容寻址；一份内容对应一个 Blob |
| Pack / PackSubject | `packs`、`pack_subjects` | 套卷是浏览单位；编目字段在套卷；`(pack_id, code)` 学科连接；软删除进回收站 |
| Material | `materials` | 一个文件 = 一份 SHA-256（唯一）；`pack_id` 归属唯一套卷；`part`/`stage`/`status`/`sync_status` 索引；`status` 只反映本机原件，`sync_status` 由副本事实决定 |
| MaterialSource | `material_sources` | 每次到达一行；重复内容只追加来源（`duplicate=true`），不新建文件 |
| Collection / CollectionItem | `collections`、`collection_items` | 专题只收套卷；`(collection_id, pack_id)` 唯一；`position` 显式排序；无父子层级 |
| Upload / UploadEntry | `uploads`、`upload_entries` | 清单先声明；条目 `received` 需字节数、SHA、真实格式和 CAS 全部通过 |
| Job | `jobs` | 持久化队列；`kind` 为 ingest/replicate/analyze/index/group/decide/entity；`next_attempt_at` 控制退避重试；租约领取，`lease_owner` 为节点 ID |
| Drive | `storage_drives` | kind 不可改；secret 只写不读；local 根目录由服务端固定；只描述连接，不含策略 |
| Rule / RuleTarget | `sync_rules`、`sync_rule_targets` | 有序、命中第一条；唯一兜底规则 `is_default`；`(rule_id, drive_id)` 唯一 |
| Replica | `replicas` | `(blob_id, drive_id)` 唯一；`desired_path` 与 `remote_path` 分开；上传与分享状态分离；规则外副本保留 |
| ClusterSettings / Node | `cluster_settings`、`nodes` | 主端保存调度策略；从端保存主端期望的槽位、缓存上限、OCR 服务和配置修订号，心跳回报实际运行状态与已应用修订 |
| AIProvider / OCRProvider / AISettings | `ai_providers`、`ocr_providers`、`ai_settings` | API Key/OCR 令牌加密；端点没有物理删除，只能停用；设置保存当前 OCR provider |
| FileCard | `file_cards` | 每资料一行；AI 阅读记录与校验问题；图片不落盘 |
| PackDecision | `pack_decisions` | 每套卷一行；最近一次 AI 编目（含对应 revision）与校验问题 |
| Entity / EntityAlias | `entities` / `entity_aliases` | 名称词典：学校、联盟、标签的规范名称与归一化写法 |
| SearchSettings | `search_settings` | 单例；默认启用、500 页、10 页/批、12000 字/批 |
| MaterialText | `material_texts` | 每资料一行；按文件 SHA + 解析器地址/缓存版本校验 |
| QuestionBlock / QuestionTerm | `question_blocks`、`question_terms` | 题目带页码和文档顺序；倒排主键 `(term, question_id)` |
| MaterialIndexState | `material_index_states` | 每资料一行；成功/失败/重建状态，不阻塞归档 |

### 6.1 文件、Blob 与副本

- 已归档文件的权威内容在 `DATA_DIR/blobs/<sha[2]>/<sha>`；普通上传和 ZIP 成员先写入 CAS，ZIP 原件暂存，随后在一个数据库事务中发布文件、来源记录和 AI 阅读任务；套卷由 AI 分组后建立。
- 一份 SHA-256 只对应一个 Material 和一个 Blob；重复上传只追加 `MaterialSource`，因此副本、解析和题目索引天然共享。
- Material 的 `file_name` 由「套卷标题 + part 标签」派生，是下载名和路径模板中的 `{文件名}`；套卷人工改名、改编目或改 part 后同步成员文件名并重新规划副本，`on_path_change=move` 的网盘随之移动，`keep` 的网盘标记路径偏离。

### 6.2 套卷、文件与专题

- 套卷是编目和浏览的单位：学科、年级、届别、地区、机构、类型、标签、描述都在套卷上；文件只保存 `part`、`answer_start_page`、`position`。
- 套卷标题在 `locked_json` 不含 title 时自动生成：考试类（高考、中考、联考、月考等）按「届 + 地区 + 机构 + 类型 + 年级 + 学科」组合，高考真题不写机构（全国统一命题，机构固定）；专题练习、讲义、学案、课件、作业、其他不是一场考试，直接用 AI 为这份资料起的名字（`exam_name`），名字缺失时才退回编目字段和 AI 读到的首页标题。人工修改的字段写入 `locked_json`，AI 编目不再覆盖。
- 需关注的文件 `stage=attention`，`attention_json` 记录原因；人工确认后置为 `ready` 并生成同步计划。套卷 stage 取成员中最靠后者（ready < processing < waiting_ai < attention）。
- 专题只存放套卷引用，替换接口整体替换收录关系，不删除套卷或文件；`position` 显式保存，追加接口放在末尾。

### 6.3 状态机

```
Upload:   pending ──finalize──> processing ──ingest ok──> completed
                                     └──── ingest fail ──> failed

UploadEntry: pending ──PUT 校验通过──> received

Job:      pending ──claim──> running ──成功──> succeeded
                               └────失败──> failed ──retry──> pending（同 id）

          分享单独记录：pending | ready | unsupported | failed

Replica:  pending ──claim──> uploading（未写入）| moving（已写入、路径变化）──> ready | failed
          另有 skipped（停用/超限/扩展名）、orphaned（规则外，保留远端）

Material.status:      archived（CAS 存在）| missing（只能从网盘读取）
Material.sync_status: waiting_catalog ──规划──> not_required | syncing | synced | attention

Analysis: pending ──> running ──> succeeded | failed
Index:    pending ──> running ──> succeeded | failed
Eligible: part in (questions, full); other parts are not_applicable
```

`RecoverInterrupted` 在 SQLite 启动时把遗留的 `running` Job、`uploading` Replica、`running` Analysis 和索引状态改回 `pending`；PostgreSQL 只回收租约已过期的任务，不干扰其他健康实例。随后才启动 worker。进程取消（关机）时四类任务都会在租约仍然有效时回到 pending。

任务行保存当前阶段、当前阶段的计数、本次执行开始/结束时间和 `duration_ms`，并保存最多 200 条带时间的处理记录；领取时开始计时，终态或重新排队时结束或清零。日志可带相对执行开始的 `elapsed_ms`、步骤 `duration_ms` 和有界 `metrics`（数据量、吞吐、页数、文件数、模型、校验级别、分享状态等），传输进度最多约每 5 秒写一次。进度更新受租约约束，旧 worker 不能覆盖新执行者的记录。列表省略历史日志，详情读取历史记录；旧任务允许没有新字段。日志不写令牌、分享链接、URL 或文件内容。

## 7. 上传与归档流水线

```
选择文件（浏览器）
  → POST /uploads（相对路径、字节数、可选 SHA、hints）
  → 逐文件 PUT /uploads/{id}/files/{entry_id}（ZIP 原文件直接上传）
      流写 DATA_DIR/tmp → 字节数校验 → 格式嗅探 → SHA 计算
      → 普通文件进入 CAS；ZIP 写入独立临时区
  → POST /uploads/{id}/finalize（幂等，创建 ingest Job）
  → worker：展开 ZIP 成员到 CAS → 按目录 + 文件名主干分组
  → 单个数据库事务：Pack/Material/MaterialSource/PackSubject/
                     analyze 阅读任务/
                     Upload 状态与 results/Job 状态/审计
      同 SHA 已存在时只追加 MaterialSource（duplicate=true），不新建文件
  → 成功后清理临时 ZIP，复制任务按同步规则异步上传各网盘
```

### 7.1 清单与单文件接收

- 清单 1..2000 条；JSON 请求最多 1 MiB；单文件 1 字节..512 MiB；清单总大小 ≤2 GiB；相对路径 ≤1024 UTF-8 字节。
- 路径必须为正斜杠分隔的相对路径，拒绝反斜杠、绝对路径、盘符、`.`/`..`、空段和控制字符。浏览器文件夹使用 `webkitRelativePath`。
- 允许 `.pdf .doc .docx .ppt .pptx .xls .xlsx .jpg .jpeg .png .webp .mp3 .m4a .wav .zip`（不区分大小写），其他扩展名 415；ZIP 原文件先上传到服务端。创建清单时先按扩展名过滤，接收时再嗅探真实内容。
- 条目按自然文件名顺序分配 position；清单可带 SHA，服务器不信任客户端 SHA，仍会重新计算并比对。
- `PUT` 支持 `Content-Length` 或 chunked；声明长度大于条目大小直接 413。字节数不符 422，真实格式与扩展名不符 415，已 finalize 的清单 409。
- 相同内容重复 PUT 返回原条目；已 received 的条目不允许替换为不同内容。
- CAS 发布和数据库标记不是同一事务；极端情况下可能留下未被引用的 CAS 文件，当前没有自动 GC。

### 7.2 ZIP 安全展开

服务端 `archive.InspectWithSkipped` 在提取前检查完整中央目录，`archive.Extract` 再按成员顺序展开：

- 拒绝路径穿越、绝对路径/盘符、反斜杠、控制字符、空段、`.`/`..`、符号链接、特殊文件、加密条目和重复路径；
- 每个 ZIP 限制声明展开总大小 2 GiB、非忽略文件 5000 条、单条压缩比 200；忽略 `.DS_Store`、`._*` 和 `__MACOSX`；
- 按整包候选编码（UTF-8、GB18030、Big5、Shift-JIS）打分还原文件名；提取 PDF/DOC/DOCX/PPTX 与 ZIP，嵌套 ZIP 最多递归展开 3 层，超过上限或其他不支持格式跳过；
- 其他格式跳过，任务日志记录总数并在完成页提示；没有可归档成员时任务失败。每个提取成员的字节数和真实格式必须与声明一致。

新上传的 ZIP 原件只保存在 `tmp/upload-archives/`，处理成功且数据库提交后删除；失败或取消时保留供重试。ZIP 原件不会创建资料或副本。处理前已在 CAS 的历史 ZIP 仍可使用，但不会自动删除。文件系统和数据库无法原子提交，崩溃或清理失败可能留下临时文件，当前没有自动清理。
### 7.3 套卷分组

| 输入 | 结果 |
| --- | --- |
| 单个普通文件，路径无 `/` | 一个套卷 + 一个文件 |
| 文件夹或多文件 | 同一个目录内文件名主干相同（如 `数学-试卷` 与 `数学-答案`）的文件合成一个套卷 |
| 多个互不相干的文件 | 每个主干各自成组，分别生成套卷 |
| ZIP | 展开成员后按成员路径分组；ZIP 原件不创建文件、Blob 记录或副本 |

- ZIP 成员记录 `source_path = ZIP 相对路径 + "/" + 成员路径`；不存在 ZIP 容器资料。
- 套卷标题：考试类用「届 + 地区 + 机构 + 类型 + 年级 + 学科」编目字段组合（高考真题不含机构），专题、讲义等非考试资料用 AI 给出的名字；都没有时退回清单 `title`、考试名或到达文件名。学科、年级、届别、地区、机构、标签来自清单 hints，服务端不从文件名推断学科。
- `part` 有 8 类：questions（试题）、answers（答案）、full（题目+答案同文件）、teacher（教师版）、answer_sheet（答题卡）、listening（听力）、main（单文档正文）、other（无法判断）。归档时按文件名关键词给出初始 part，AI 自动应用与人工修改走同一校验；文件名按「套卷标题 + part 标签」生成。
- 同一事务中还写入：每个新文件的 analyze 阅读任务（文件保持 `waiting_catalog`，AI 编目后再规划同步）、Upload 的 `results`/`new_count`/`duplicate_count`、ingest Job 成功状态和系统审计。分组（group）在同批文件读完后由阅读任务的提交入队，编目（decide）由分组或成员变化入队。

## 8. 网盘同步

原件只以本机 CAS 为准；网盘副本由「网盘 + 有序同步规则」决定，与原件安全状态分开报告。

### 8.1 网盘与规则

- **网盘**（`storage_drives`）只描述连接：kind、凭据、`root_path`、`max_file_bytes`、`allowed_exts`、`read_order`、`on_path_change`。不再有主备、权重或全局副本策略，也不默认创建本地网盘。
- **规则**（`sync_rules` + `sync_rule_targets`）自上而下匹配，命中第一条启用规则为止；条件字段之间为「且」、同字段多值为「或」。最后一条固定为启用、无条件的兜底规则，空库时创建且没有目标。
- 每个目标给出 `drive_id`、路径模板、`share`、`required`。模板变量来自套卷编目和文件部件，每段清理非法字符并截断到 100 字符，再拼到 `root_path` 之下；蓝奏云只取文件名。同一网盘上不同文件撞到同一路径时，后来者追加 ` (sha8)`。路径占用表同时包含所有副本的期望路径和实际路径（含规则外副本）。

### 8.2 计划（期望 vs 实际）

`planMaterialTx` 在事务中为单个文件运行规则，得到期望目标，并与该 Blob 现有副本比较：

| 动作 | 条件 | 结果 |
| --- | --- | --- |
| upload | 期望有、尚未写入网盘 | 副本 pending，入队 replicate |
| move | 已写入，期望路径 ≠ 实际路径，网盘 `on_path_change=move` | 副本 pending，入队 replicate（执行时为 moving） |
| drift | 同上但网盘为 `keep` | 副本保持 ready，资料同步状态为 attention |
| share | 需要分享但尚无可用分享 | 副本 pending，入队 replicate |
| skip | 网盘停用、超过大小上限或扩展名不允许 | 副本 skipped，撤销等待中的任务，不算失败 |
| orphan | 实际有、期望没有，且已写入网盘 | 副本 orphaned、清空 `rule_id`，不删除远端 |
| cancel | 实际有、期望没有，且从未写入 | 删除副本行并结束等待中的任务 |

- 正在 uploading/moving 的副本保留当前状态；worker 完成后若发现期望字段已变化，会在同一事务内重新规划。
- 所有规划路径（单文件规划、预览、保存、reconcile、副本管理、worker 完成）先获取同步锁：PostgreSQL 用事务级 advisory lock，SQLite 依赖单连接立即事务。
- **等待编目**：新文件 `sync_status=waiting_catalog`。AI 编目提交时为套卷全部成员规划（需关注的套卷也按当前编目规划，编目失败的套卷除外），人工确认需关注文件时也会规划。`POST /materials/{id}/sync` 可强制规划。已规划文件在改名、改编目或改部件后自动重新规划。
- **预览与保存**：`POST /storage/rules/preview` 用同一套计算逻辑对全部已编目文件生成动作统计和明细，不写库，并签发用 `MASTER_KEY` 加密的 `preview_token`（绑定用户、草稿摘要、当前规则/目标/网盘摘要，15 分钟过期）。`PUT /storage/rules` 校验令牌后在一个事务中替换规则并重新规划全部已编目文件；任一绑定项变化返回 409。
- `POST /storage/reconcile` 按当前规则重新规划全部已编目文件，修改网盘设置后用它让已有文件生效。

### 8.3 状态

- `Material.status`：`archived`（本机 CAS 存在）或 `missing`（只能从网盘读取），每次刷新时检查 CAS。
- `Material.sync_status`：忽略 skipped/orphaned 副本；没有其他副本为 `not_required`；必需目标全部 ready 为 `synced`；仍有未完成目标为 `syncing`（没有必需目标时，任一副本 pending/uploading/moving 也算）；必需目标失败、必需目标分享失败或已写入副本路径偏离为 `attention`；尚未规划为 `waiting_catalog`。
- `Replica.status`：pending → uploading/moving → ready/failed，另有 skipped、orphaned。

### 8.4 复制、移动与分享

- 每个 `(blob_id, drive_id)` 至多一个副本；`queueReplicaJob` 保证同一副本至多一个 pending/running 任务，且只为 pending 副本入队。
- worker 在锁定租约的事务中把副本置为 uploading（未写入）或 moving（已写入）；随后在事务外调用 `Adapter.Put` 或 `Mover.Move`，成功后记录 `remote_id`、`remote_path`、大小和 `verification`。网盘已停用、副本已 skipped/orphaned/ready 时直接结束任务。
- **适配器不覆盖**：`Put` 遇到同路径已有对象时，只在大小一致（能校验时 SHA 也一致）的情况下接管，否则返回 `conflict`；`Move` 同样不覆盖目标。因此「远端已写入但数据库未记录」的情况在重试时会被接管。
- `verification` 只声明真正做过的检查：`uploaded`（仅收到成功响应）、`size`、`sha256`。
- 目标要求分享时在上传/移动后调用 `Share`；分享失败不回滚副本，`share_status=failed` 且副本仍 ready；不支持分享记为 `unsupported`。不要求分享时清空分享字段。
- 分享 URL、提取码、过期时间只出现在有 `materials:read` 的文件详情响应；不写日志/审计。
- 下载先看本地 CAS；缺失时按网盘 `read_order`、Drive ID、创建时间稳定排序，尝试所有已写入网盘（`remote_id` 非空）的副本，含规则外副本和已停用网盘；全部失败返回 503。
- 按需拉取接口同样按此顺序只从网盘读取，不复用 CAS；流式写入 `tmp/material-pulls` 并校验大小与 SHA-256，原子提交后按 `PULL_CACHE_TTL` 提供鉴权下载，不写入永久 CAS。

### 8.5 规则外副本与删除

- 规则变化或资料彻底删除后，已写入网盘的副本转为 orphaned，系统从不自动删除远端文件。彻底删除时保留该 Blob 行，以便再次上传相同内容时重新接管；`local` 网盘文件和从未写入的副本行直接删除。
- `POST /storage/replicas/bulk`：`retry` 重新规划；`delete_remote` 仅对 orphaned 副本调用 `Deleter.Delete`，执行期间创建一条 running 的 replicate 任务作为租约占位并心跳，完成后若副本仍是 orphaned 则删除记录，若期间又被规则需要则清空远端字段、重新排队上传；`forget` 只删除 orphaned 副本记录。有 pending/running 任务的副本返回 409。

### 8.6 适配器能力

| kind | 上传 | 下载 | 分享 | 子目录/移动/删除 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `local` | 可用 | 可用 | 不支持 | 支持 | 根目录固定为 `DATA_DIR/drives/<driveID>`；硬链接提交不覆盖；移动/删除后清理空目录；提供 SHA 时 verification=sha256 |
| `webdav` | 可用 | 可用 | 条件可用 | 支持（MKCOL/MOVE/DELETE） | 配置 `public_base_url` 才返回公开目录拼接链接，不代表匿名可访问；提供 SHA 时上传后完整下载重算 |
| `lanzou` | 可用（非官方协议） | 可用 | 可用 | 不支持 | Cookie 会话；`root_path` 为文件夹 ID；单文件 100 MiB；默认 verification=uploaded；未对真实服务联调 |
| `123pan` | 不可用 | 不可用 | 不可用 | — | 只实现 access_token 与 Health；规则不能把它作为目标 |
| `onedrive` | 可用 | 可用 | 可配置 | 支持 | Graph 委派授权；自有应用或 OpenList 在线应用；`conflictBehavior=fail`，小文件直传、大文件分段上传，PATCH 移动；刷新令牌加密轮换；未对真实服务联调 |
| `s3` | 可用 | 可用 | 可用 | 支持（移动为复制后删除，非原子） | AWS SDK 分段并行上传，HEAD 比对大小，签名链接按配置过期；兼容服务需实际验证 |

适配器把失败归类为稳定 `CloudError`（config/auth/timeout/network/unavailable/quota/size/hash/link/unsupported/conflict 等）；存储错误写入 Job/Replica 前会脱敏。OneDrive 对冲突、限流和 5xx 做有限重试，并在上传会话冲突时清理中断留下的 0 字节占位文件；蓝奏有内部重试，S3 使用 SDK 自带的请求重试；服务层没有自动重试循环。

## 9. AI 编目

编目只由模型决定；`internal/catalogai` 只负责提示词、回答解析、校验与一致性推算，不做任何猜测。同一套代码在主端和从端运行，同一个回答在哪里都按相同规则判断。

```
ingest 事务
  └─ 新文件 stage=processing、ai_step=read、batch_id=upload_id，pack_id 为空
     并创建 analyze Job + pending FileCard（不按文件名分组、不写入提示、不猜部件）

analyze（READ，可在从端执行）
  → 取原件：本地 CAS 优先，否则从可读副本下载（≤256 MiB）
  → 不需要图片且 material_texts 命中（SHA + parse key）：复用解析文本
  → 否则按 ai_settings.ocr_provider_id（空则 priority 最小的启用项）：
       MinerU：POST /api/v4/file-urls/batch → PUT 预签名 URL
         → DOC/DOCX/PPT/PPTX 先经 LibreOffice 转 PDF
         → 轮询 /api/v4/extract-results/batch/{batch_id}（默认 15 分钟）
         → 下载 full_zip_url，读 content_list.json/full.md/页面图
       PaddleOCR：multipart POST /api/v2/ocr/jobs（model=PP-OCRv6）
         → DOC/DOCX/PPT/PPTX 先经 LibreOffice 转 PDF
         → 轮询 /api/v2/ocr/jobs/{jobId}（默认 15 分钟）
         → 下载 resultUrl.jsonUrl JSONL，提取 rec_texts
       RapidOCR：multipart POST /api/v1/ocr/jobs
         → DOCX 由 Go 直接解析 OOXML（正文、表格、文本框、公式、分页）
         → 嵌入位图逐张提交同一 OCR 接口（最多 20 张、并发 2）
         → DOC/PPT/PPTX 先经 LibreOffice 转 PDF
         → PDF/图片仍按页上传
         → 轮询 /api/v1/ocr/jobs/{id}（默认 15 分钟）
         → 下载 result_url JSONL，按页提取 texts；也可接收有序正文/公式 blocks
  → 写 material_texts（Markdown + 逐页文本）
  → 无法解析正文的文件（音频、表格、损坏、无文字）只凭文件名与路径阅读，标记 metadata_only
  → 模型输出 Card：部件、首页标题、学科、年级、学年、届别、考试年月、文中机构/地区写法、题目指纹
  → 校验：枚举、页码范围、关键字段必须附可在材料中逐字找到的摘录，否则剔除
  → 主端用同一校验重算并保存 file_cards；文件进入 group（或已在套卷中时进入 decide）

group（主端，全局互斥）
  → 同批文件读完后按来源路径取最多 80 个（同文件夹可延伸到 120 个）
  → 候选套卷：文件当前所在、本批已建、学科相同且年级/届别相同的已有套卷（按相似度取前 40）
  → 模型分组；校验每个文件恰好一次、只引用候选、同套部件不重复、学科不混杂
  → 写 pack_id，新套卷为空编目；套卷 revision+1 并入队 decide；剩余文件继续下一批

decide（主端，按套卷互斥）
  → 成员仍在阅读/分组时推迟；输入全部成员的 Card、正文摘录、人工锁定字段、机构候选、常用标签
  → 校验 doc_type、试卷类必须有学科与年级；学年与届别一致（由一者推算另一者）；
    考试年月落在学年内、学期由月份推算；地区取固定列表；关键字段需原文依据；
    机构优先选候选、新机构须为完整全称、与已知别名相同的“新名称”映射到已有条目；
    标签最多 5 个（新建不超过 2 个）；每个文件恰好一个部件且唯一部件不重复；锁定值原样保留
  → revision 已变化则丢弃并重新入队；否则写套卷字段、规范名称、成员部件、标题与文件名，
    写 pack_decisions，生成同步计划，按部件入队题目索引，新名称入队 entity

entity（主端，全局互斥）
  → 新建的机构/标签与相近条目（附使用它的套卷标题）交给模型判断是否同一对象，确认后合并
```

- 每次模型调用都先校验回答；不通过时把问题清单发回同一模型修正一次，再按 priority 尝试下一个端点。选择结果的顺序是：无问题 → 无关键问题 → 问题最少。关键问题仍存在时，分组退回“每个文件单独成套”并标记 group_unverified，编目把套卷置为 attention 并记录校验代码；非关键问题只剔除对应值。
- 模型自报的置信度不参与判断，也不保存。
- 失败分类：缺少模型/OCR、密钥被拒属于配置问题，文件进入 waiting_ai，任务每 15 分钟再试且不计次数，保存 AI 设置、模型端点或 OCR 服务商时立即唤醒；限流、超时、服务不可用、网络错误按 1 分钟到 12 小时退避重试，最多 6 次；其余为永久失败。重试用尽后阅读失败的文件按文件名继续，分组失败的文件单独成套，编目失败的套卷进入 attention（ai_failed）且暂不同步。租约多次过期的任务（含复制、索引、归档）达到上限后停止重试，防止崩溃任务无限循环。
- 人工修改的编目字段写入 `packs.locked_json`、人工修改的部件置 `materials.part_locked`；AI 把它们作为固定值输入，提交时也不会覆盖。移动文件、拆分、合并或把文件移入回收站会让相关套卷 revision+1 并重新编目。
- 名称词典：`entities` 保存规范名称，`entity_aliases` 以归一化写法（全半角、大小写、空白、标点、“省/市”统一）为主键映射到条目。归一化只用于精确匹配和候选排序，缩写与全称是否同一机构由模型判断。人工修改或合并的条目锁定，AI 不会自动改动或合并锁定条目。
- 分页/图片限制：`max_pages` 1..10，`max_images` 0..4；`max_pages` 限制送入视觉模型的页面图片数，OCR 仍解析全文。阅读时文本取样上限 8000 字、编目时每个文件最多 12000 字（整套共享 48000 字，每个文件至少 1500 字）（最多 8 个文件），都保留开头约 2/3 与末尾约 1/3 并标注中间省略，确保卷末答案区能参与部件判断。RapidOCR/PaddleOCR 不返回持久化页面图，因此视觉模型只收到文本。
- 题目索引按 `batch_pages`/`batch_chars` 分组后最多 3 个批次并发调用，结果再按原页码顺序去重合并。切题模型使用 128000 输出 token 上限和 `reasoning_effort=high`；只有空响应或非法 JSON 才二分降级重试，上游 5xx/限流最多额外重试一次，超时、鉴权等错误立即失败，避免错误重试倍增调用量。
- 解析出的独立页面图片不落盘、不提供图片读取接口。DOCX 生成的 PDF 预览缓存可能包含文档原有图片，应按原件保护。
- 解析缓存按文件 SHA + `sha256("ocr-text-v2:<kind>:<base_url>:<model>")` 校验；切换 OCR 服务商、地址或模型会自然失效。MinerU 需要视觉图片时会重新解析，因为图片没有缓存。RapidOCR 的 DOCX 原生解析另含 `docx-native-v2` 版本标记，避免复用旧的公式文本缓存。
- DOCX 原生解析由 `internal/docx` 完成：读取 `word/document.xml` 与关系表，按段落/表格/文本框/公式生成 Markdown 和逐页文本，识别显式分页与 `lastRenderedPageBreak`；常见 OMML 分式、上下标、根号、求和/积分、括号、上下限、横线和矩阵保留为 LaTeX，其他结构尽量保留文本。嵌入的 PNG/JPEG/GIF/BMP/TIFF/WebP 按引用顺序记录，最多 20 张、单张 12 MiB、总计 64 MiB，图片 OCR 失败只记录日志，不影响正文。页码依赖文档保存的分页提示，连续文档没有提示时按第 1 页处理。自建 RapidOCR 的 DOCX 不经过 LibreOffice 转换或 OCR 文字层，因此分析/搜题不依赖 `soffice`，也不依赖 RapidOCR 镜像版本同步。MinerU/PaddleOCR 接收的 DOCX，以及所有服务接收的 DOC/PPT/PPTX 会先由 LibreOffice 输出 PDF，再交给统一 OCR 协议。
- DOCX 原生解析器不会读取外部关系目标；外部图片、模板、OLE 对象和数据源只作为关系元数据跳过。送入 LibreOffice 前，DOCX/PPTX 的临时副本会移除这些可能触发网络访问的外部关系，保留 inert 超链接，避免渲染器拉取外部资源。
- RapidOCR 结果接收端支持 `page_no` + `texts` 旧格式，也支持有序 `text`、`inline_formula`、`display_formula` 内容块并保留 LaTeX。现有自建 OCR 进程仍只输出 `texts`，并未接入扫描件公式识别模型。
- Office 转换由 `internal/office` 调用 LibreOffice 生成 PDF，使用独立配置、最高宏安全级别、外部关系清理和进程级并发限制。DOCX 预览缓存改为 `renders/office-v1/<sha前两位>/<sha>.pdf`，转换任务在独立临时目录中处理并清理；Word 使用 Writer PDF 过滤器，PPT/PPTX 使用 Impress PDF 过滤器。非容器部署需自行安装 LibreOffice Writer、Impress 和所需字体；预览排版受渲染器和字体限制，公式、图文顺序及表格结构不保证与 Word 完全一致。
- 模型输出是不可信数据：提示中明确要求忽略文档内指令；服务端只接受通过校验的值，模型原文不落库；不执行模型生成的指令，也不允许改 Blob、SHA、来源路径或状态。
- 失败写回 FileCard/PackDecision 和 Job 的脱敏错误；不影响已完成的归档、下载和副本。
- PaddleOCR 明确返回“任务提交队列已满”时，提交阶段最多尝试 6 次，并按 2、4、8、16、30 秒退避；其他提交错误不盲目重试，避免生成重复远端任务。
- 关键超时：OCR 单次解析默认 15 分钟，阅读外层 16 分钟；单次模型对话 120 秒；模型探测/最小对话 30 秒；OCR 健康检查 20 秒。

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

- 索引默认启用；只有 `part` 为 `questions`/`full` 的文件会在归档、AI 应用 part 或人工改 part 后入队。关闭索引期间归档的试卷文件，在服务启动时由 `EnqueueMissingIndexes` 补建（仅当索引启用）；非试卷类旧索引和未完成任务会在启动时清理。
- 自动入队和启动补建共用按资料去重的入队逻辑，索引状态与任务在同一事务创建；分析任务也依靠唯一约束去重，避免逐条预查询。
- ZIP 原件不索引；展开成员各自索引。
- 手动 `POST /materials/{id}/index` 不依赖索引开关；`force=true` 会清空 `material_texts.source_hash` 以强制重新解析。
- 失败不会删除上一次成功的索引；旧题目仍可搜索，状态显示 failed。重建成功才原子替换题目和词项。
- `question_terms` 使用 CJK unigram + bigram、连续字母/数字词和保留运算符的公式二元/三元词项，不依赖 SQLite FTS5、PostgreSQL 扩展或向量库。题干原文和 LaTeX 不被归一化覆盖；切题提示要求保留公式，输入中已有 LaTeX 命令的单反斜杠 JSON 输出会先修复，危险控制字符会使切题失败。
- 新题目在同一事务写入公式词项与 `math_index_version=1`；已有题目由后台分批幂等补建，公式查询也会先补完剩余项。补建只读取已保存题干，不调用 OCR/模型；PostgreSQL 对题目行加锁并按同一顺序协调替换/删除，SQLite 依赖单连接与立即事务。
- 补建无法恢复旧题干已经丢失的结构；旧 DOCX 可重建题目索引，从原件按 `docx-native-v2` 重新解析并调用切题模型。
- 题目和词项在事务内分批写入，词项缓冲有界，减少逐行写入的数据库往返；套卷成员使用稳定自然排序。
- `material_index_states` 记录页数、题目数、LLM 调用数、失败批次数和 indexed_at。

### 10.2 搜索

- 普通文本归一化仍为全角转半角、去空白/标点/符号、转小写；公式另行保留运算符和变量大小写，将上标、常见符号、简单分式及对应 LaTeX 写法统一。最多优先取 16 个公式词项，总计最多 24 个词项。
- SQL 先按词项召回；公式查询要求候选题目包含所有选中的公式词项，再限制最多 2000 个候选，避免纯文字相似题目挤占公式结果。只返回 `materials.status in (archived, missing)` 且未删除的资料。
- 过滤条件：学科（`pack_subjects`）、年级、届别；可选。
- 普通文本按词项覆盖率、词频、短语和长度打分；公式查询先校验所有公式片段，混合文字要求选中词项至少 35% 匹配，再按覆盖率、词频及公式命中打分。不同运算符不会因为去标点而合并，但不进行通用代数等价运算。
- 搜索结果返回 `snippet`（约 180 字）和纯文本字符区间 `highlights`，不返回 HTML；普通文本优先截取归一化后最长的连续匹配文段，公式查询围绕命中公式截取。截断处附省略号，区间相对于包含省略号的摘要；前端自行渲染 `<mark>` 并支持分页。单题详情没有查询词，高亮为空。
- `limit` 默认 20，最大 50；`offset` 从 0 开始；`total` 是过滤后的命中数，不是 SQL 候选数。
- 每条结果对应一个题目块；同一题出现在多份试卷就是多条结果，不做跨资料聚合。结果带所属套卷 `pack`，并用 `also_in` 列出同一题干出现的其他套卷。
- 搜索接口使用 `materials:read`；搜索设置使用 `search:read`/`search:write`，默认仅管理员，可授权给 API 密钥。
- 搜索不检查 `material_index_states.status`，因此重建失败时旧索引仍可查询；这与“没有索引就没有结果”并不矛盾。

## 11. MCP

`POST /mcp` 是无状态 Streamable HTTP 的 JSON 子集，只接受 API 密钥 Bearer。支持 `initialize`、`ping`、`tools/list` 和 `tools/call`，单条 JSON-RPC 请求，不提供 SSE、batch 或会话 ID。协议和客户端示例见 [MCP.md](MCP.md)。

工具目录映射固定的 REST 路由：参数按 schema 校验后交给同一套 HTTP handler、权限和业务逻辑；内部请求复用已认证的身份并再次检查 scope，无需重复查询密钥。不存在通用 URL 或本地文件读取工具。`tools/list` 按当前 scope 过滤并区分查询和写入；工具调用再次检查权限。后端测试检查每个业务路由均有工具，避免前端与 MCP 功能漂移。

`uploads_file_put` 支持最多 512 KiB 的 Base64 内容；更大的上传和资料下载返回受鉴权的相对路径，客户端使用同一密钥直接流式请求。参数错误返回 `-32602`，领域错误返回 `isError: true`；成功结果是 JSON 文本。

## 12. 前端交互架构

### 12.1 页面与权限

| 路由 | 页面 | 前端 scope 守卫 |
| --- | --- | --- |
| `/login` | 登录 | 公开 |
| `/` | 工作台（统计、最近套卷、上传入口） | 登录 |
| `/inbox` | 需关注收件箱（AI 编目、校验问题、按套卷确认；AI 整理中、等待 AI） | materials:write |
| `/library` | 资料库（套卷列表与筛选） | materials:read |
| `/packs/:packId` | 套卷详情（文件、齐全度、专题、下载） | materials:read |
| `/materials/:materialId` | 文件详情（AI 阅读记录、题目索引、归档与同步状态、副本） | materials:read |
| `/search` | 搜题 | materials:read |
| `/collections`、`/collections/:collectionId` | 专题列表/详情 | collections:read |
| `/imports`、`/imports/:uploadId` | 导入中心/传输与归档进度 | uploads:write |
| `/trash` | 回收站（恢复、彻底删除） | materials:write |
| `/jobs`、`/jobs/:jobId` | 任务列表/详情与重试 | jobs:read |
| `/storage` | 网盘、同步规则（编辑、预览并保存）、副本（筛选、批量重试/删除远端/移除记录）、补齐副本 | storage:read |
| `/nodes`、`/nodes/:nodeId` | 节点与集群调度 | nodes:manage |
| `/settings/:section` | 设置中心：`ai-models`、`ai-parsing`、`entities`（名称词典）、`api-keys`、`users`、`audit`；只显示有权限的分区 | ai:read / materials:read / keys:manage / users:manage / audit:read |

`web/src/app/routes.tsx` 是路由、侧栏、面包屑、页面标题、命令面板和站内链接（`href.*`）的唯一来源；页面按路由懒加载，侧栏和命令面板在悬停或选中时预加载目标页面。侧栏分「资料」「运维」「系统」三组，桌面端可收起为图标栏（记在本机），1024px 以下改为抽屉。`⌘K`/`Ctrl+K` 或 `/` 打开命令面板：跳转页面与设置分区、导入资料、按名称打开套卷、搜题。

前端权限只是可见性镜像；服务端是唯一授权来源。角色权限表与响应类型由后端生成（`contract.gen.ts`）。

服务端状态全部经 TanStack Query 管理，查询键只在 `web/src/app/queryKeys.ts` 定义：第一段是资源名，文件级数据（AI 阅读、题目索引、题目列表）挂在 `materials` 下，`invalidateLibrary` 按前缀刷新资料、套卷、专题、导入和仪表盘。顶栏的「导入资料」是默认按钮，每屏唯一的实心主按钮留给页面自身的主操作。搜索设置可通过 HTTP 或 MCP 访问，目前没有独立管理台页面。

OneDrive 新建表单默认引导使用 OpenList 应用；服务端旧配置缺省 `auth_mode` 为 `direct`，以免切换已有账户。OpenList 模式通过第三方 HTTPS 接口续期，按上游协议把刷新令牌放在查询参数中；界面告知用户该数据流，本服务不记录请求 URL，且拒绝重定向。自有应用仍使用微软直连端点。不同应用签发的刷新令牌不能混用，切换模式必须提交新令牌。

### 12.2 上传会话

- 选择文件/文件夹/ZIP 后立即自动 `POST /uploads`，导入中心只展示传输、处理中、需要确认和完成/失败状态，不要求用户手动提交归档。
- 传输使用 XHR 以获得进度，最多 3 个并发；网络错误、429、5xx 自动重试 1 秒/3 秒，415/422/409/413 不重试。
- 全部条目 received 后自动 finalize；失败只保留恢复和重试入口。
- 刷新页面后 `File` 对象丢失，需要重新选择相同相对路径和字节数的文件；已接收条目跳过。
- `GET /uploads` 提供导入中心的批次分页；`GET /uploads/{id}` 用于恢复中断会话。

### 12.3 轮询与缓存

- 任务列表/详情：3 秒，终态停止。
- 上传会话：2 秒，完成/失败停止。
- 分析、索引：3 秒，pending/running 时轮询。
- 仪表盘：资料与任务各使用一次按状态聚合的统计接口；最近记录和统计在页面可见时每 30 秒更新，手动刷新也会重新获取统计、最近记录及有权限的存储信息。各区域独立查询，单项失败不影响整页。
- 所有写操作成功后按 query key 失效相关列表/详情；不使用本地假数据。

### 12.4 界面约定

- 主题由 `web/src/app/theme.ts` 的 Mantine 主题统一定义（默认蓝色主色、`md` 圆角），不硬编码品牌色：字号 13/14/16 与标题 18/20/28px，字重只有 400 和 600，间距 8/12/16/24/32px，按钮、输入框和分页高度不小于 40px，次要文字用 gray.7 保证对比度。列表、详情、表单弹窗等通过 `shared/ui` 共享组件保持对齐；数值指标用 `Metric`；卡片内的小节用 `Section bare`，不再嵌套边框。状态标签只有中性、成功、警告、错误、进行中五种语义色（`StatusTags` 的 `TagColor`），部件、类型等类别信息一律中性。
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
 → Bootstrap（空库时管理员、兜底同步规则、ai_settings、search_settings）
 → RecoverInterrupted（running → pending）
 → EnqueueMissingIndexes（索引启用时）
 → StartWorker（单 goroutine）
 → HTTP ListenAndServe（托管 web/dist）
```

- `Bootstrap` 只在条件满足时创建默认行，可重复调用；已有用户时不会重置管理员密码。
- 不再默认创建本地网盘；兜底规则只在不存在任何 `is_default` 规则时创建，没有目标。`local` 网盘根目录固定为 `DATA_DIR/drives/<driveID>`。
- 优雅退出信号为 SIGINT/SIGTERM；HTTP Shutdown 超时 10 秒。进程停止时正在运行的 Job 由下次启动的 `RecoverInterrupted` 处理。
- 服务不自动加载 `.env`；`DATA_DIR`、`WEB_DIST` 都以进程工作目录求绝对路径。

### 13.2 数据目录

```
DATA_DIR/
  paper.db                 默认 SQLite（可用 DATABASE_DSN 指向别处）
  blobs/<sha[0:2]>/<sha>   原件 CAS
  tmp/                     上传/展开临时文件
  tmp/upload-archives/     已接收、等待处理的 ZIP；成功后清理
  tmp/material-pulls/      云端副本按需拉取缓存，按 SHA 命名并受 TTL 限制
  analysis/<materialID>/document.md   识别 Markdown（绝对路径写入数据库）
  drives/<driveID>/...                `local` 网盘的副本（按路径模板分层）
```

数据库还保存凭据密文、refresh/密钥哈希、任务、副本和分析/索引状态。数据库与文件系统/云端不是同一事务；备份和恢复见 [DATA.md](DATA.md)。

### 13.3 配置摘要

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `ADDR` | `:8080` | 监听地址 |
| `DATA_DIR` | `./data` | 数据库默认位置、CAS、临时文件、`local` 网盘 |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DATABASE_DSN` | `DATA_DIR/paper.db` | PostgreSQL 时必填 |
| `WEB_DIST` | `../web/dist` | SPA 静态目录；不存在时仅 API |
| `ADMIN_USERNAME`/`ADMIN_PASSWORD` | 空 | 仅空库自举；密码至少 12 字符 |
| `JWT_SECRET` | 必填 | ≥32 字符，HS256 签名 |
| `MASTER_KEY` | 必填 | 32 字节 base64，AES-256-GCM |
| `PULL_CACHE_TTL` | `15m` | 云端副本拉取到临时目录后的可下载时长 |

完整说明见 [DEPLOYMENT.md](DEPLOYMENT.md)；`MASTER_KEY` 丢失后已保存的存储/AI 凭据无法解密。

## 14. 安全边界

- **认证**：浏览器 JWT 只接受 HS256，校验 issuer/audience/exp/nbf；Agent 直接使用 API 密钥。每次请求重新查 Session/User/Key，撤销立即生效；升级前已签发的旧代理 JWT 在原有效期内仍可校验。
- **授权**：HTTP scope 与 MCP 工具二次检查；Agent scope 取角色与密钥交集，委派的密钥或用户权限不得超过调用者；MCP 仅 Agent。
- **秘密**：存储/AI/OCR 凭据加密；API Key/refresh 只存哈希；日志和审计不写秘密、Cookie、分享链接或完整 URL；`redact` 处理存储与上游错误。
- **请求**：严格 JSON、1 MiB 限制、未知字段/非法 null 拒绝；MCP 无通用 URL/本地文件读取工具。
- **文件**：相对路径校验；ZIP 在服务端检查后解压成员；拒绝遍历/符号链接/加密/炸弹/格式伪装；`local` 网盘根目录由服务端固定，路径模板逐段清理并拒绝 `.`/`..`，适配器再次校验路径不逃逸；上传和移动不覆盖已有对象。
- **AI 输入**：文档内容按不可信数据处理；提示要求忽略其中指令；模型输出经白名单校验后自动写入编目字段，人工可覆盖。
- **浏览器**：同源部署，未实现 CORS；access token 只在内存，refresh 存 localStorage；浏览器不保存 Agent 密钥。
- **限流**：登录按用户名/IP 内存节流；API Key 与上传没有全局限流，依赖部署层或反向代理。

## 15. 一致性、失败与运维事实

- **事务**：套卷与文件发布、专题收录替换、回收站/恢复、同步规则替换与副本计划/状态刷新、分析/索引最终写入、密钥更新均在数据库事务中；网络上传、OCR、模型调用不在事务内。
- **非事务副作用**：CAS、临时文件、远端对象和数据库更新不能原子提交。失败可能留下孤儿 CAS 或已上传但未记库的远端对象（重试时按「同内容接管」处理）；当前没有自动 GC，远端文件只在人工 `delete_remote` 时删除。
- **幂等**：finalize 对同一上传返回同一 Job；ingest 发布是单事务；副本规划按 `(blob_id, drive_id)` 复用已有行，可重复执行；FileCard/PackDecision/Index 重跑覆盖自身结果；同一批次、同一套卷最多一个待执行的 group/decide 任务，过期 revision 的编目被丢弃。
- **恢复与领取**：SQLite 启动时将遗留 `running` 改回 `pending`；PostgreSQL 仅回收过期租约。worker 按类型领取 pending Job，`attempts` 递增。
- **租约与并发**：心跳续租；终态更新必须匹配 `lease_owner`，过期任务的旧执行者写入会被拒绝。同一资料的 analyze/index、同一套卷的 decide 由 `job_locks` 串行化；group 与 entity 全局串行，避免两次分组为同一场考试各建一套或并发合并名称。
- **取消**：ingest/replicate/analyze/group/decide/entity 在进程 context 取消时回到 pending（uploading 副本回到 pending）；index 取消当前记为 failed（可重建）。HTTP 客户端断开只取消对应的上传请求，不影响已入队任务；远端请求受适配器自身超时约束。
- **部署边界**：主端只有一个实例，SQLite 单连接、单 writer、WAL，PostgreSQL 为可选数据库；从端没有数据库和共享磁盘（见第 18 节）。schema 由一次性 `migrate` 服务创建，应用设置 `DB_AUTO_MIGRATE=false`。RapidOCR 容器与 PostgreSQL 一样只在 Compose 内网监听，不直接暴露公网；其默认 CPU 上限为 1 核，推理线程数为 1，避免 OCR 长时间挤占应用和数据库资源。
- **增长**：`material_texts`、`question_terms`、`file_cards`、`audits`、`jobs` 和孤儿 CAS 会持续增长；没有保留期/清理任务。`AutoMigrate` 不会删除旧表/旧列。
- **迁移**：没有版本化 migration；升级前先备份，启动时由 `AutoMigrate` 增量建表/加列。已删除功能留下的表和列不会自动清理。

## 16. 验证状态

最近更新：2026-11-11，基于提交 `b7df9e7` 之后的工作区。以下结论只针对实际执行过的检查。

### 本地实际执行并通过（2026-11-11）

环境：Linux arm64、Go 1.26.10、Node 24.22.2。

```bash
(cd backend && go vet ./... && staticcheck ./... && go test -count=1 ./...)   # 全量，含慢测试
(cd backend && go test -race -count=1 ./internal/service/ -run 'Purge|Trash|CommitDecide')
(cd web && npm run build && npm test)   # tsc 类型检查与构建；15 个测试文件、50 个用例
```

另在临时 `DATA_DIR` 中启动真实服务（SQLite，未配置模型），用无头 Chromium 在桌面（1366px）与移动端（390px）检查套卷详情：只修改备注不锁定字段、「交还 AI」解除锁定并重新排队编目；以及需关注收件箱：取消修改会丢弃改动、保存并确认只发出一次事务请求且部件修改被保存。

### 早期验证（2026-11-05，网盘同步重构）

在临时 `DATA_DIR` 中启动真实服务（SQLite、`local` 网盘），用 HTTP 脚本走完：空库只有兜底规则 → 上传 → 添加网盘 → 未预览保存被拒 → 预览并保存 → 上传落盘且 SHA-256 一致 → explain → 套卷改名后远端移动 → 统计与 `sync_status` 筛选 → 移除目标后转为规则外且文件保留 → `delete_remote`/`forget` → 下载原件 → MCP 列出并调用存储工具。随后在浏览器中检查桌面（1253px）与移动端（390px）的网盘、同步规则（编辑 → 预览 → 应用）、副本和文件详情页面。

### GitHub Actions（提交 `51208ab`，网盘同步重构之前）

- `CI` 通过：后端测试、`vet` 与构建，前端测试、类型检查与构建，本地和生产 Compose 的 `config --quiet` 校验。
- `Docker Build and Deploy` 的 `Publish images` 通过，应用和 RapidOCR 镜像已推送 GHCR。由 push 触发，`Deploy production` 跳过，因此本提交未部署到云端。

### 自动化测试覆盖范围

- **后端单元/协议测试**：归档编码、存储适配器（local/WebDAV/蓝奏/OneDrive/S3；local/WebDAV/OneDrive/S3 的不覆盖接管、移动与删除），同步规则（条件匹配、路径模板渲染与校验、规则列表结构校验），AI、MinerU、PaddleOCR、RapidOCR 协议，DOCX 原生解析与图片 OCR，数学公式规范化与检索，搜索文本与高亮，迁移幂等。
- **后端集成测试**（临时 SQLite + httptest 模拟上游）：
  - 上传（`server/direct_upload_integration_test.go`、`integration_test.go`）：单文件直传生成单个套卷且不建合集，编辑编目后改名并锁定字段，删除唯一文件使套卷进回收站；已知内容秒传只追加来源；文件夹与嵌套 ZIP 展开为套卷并列出不支持成员；路径穿越 ZIP 失败且不发布；并发 ZIP 上传不覆盖已胜出的内容。
  - 副本：损坏的云端副本在拉取时被拒绝，不生成缓存、不恢复永久 CAS。
  - 网盘同步（`service/sync_rules_test.go`，真实 `local` 网盘）：命中第一条规则且停用规则被跳过；预览不写库，令牌绑定草稿、用户、网盘与过期时间；同路径追加 sha8 后缀且重新规划不漂移；超限/扩展名跳过不算失败；改名后 move 网盘远端移动、keep 网盘记为路径偏离；规则变化转规则外且保留文件，`delete_remote`/`forget`/非规则外拒绝；从未上传的副本撤销；等待编目直到置为 ready 或强制同步；同路径不同内容不覆盖、需处理后重试恢复；停用网盘跳过但仍可读取；彻底删除时云端副本转规则外并保留 Blob。对规划代码做的 7 处故意破坏（去掉首条命中、去掉路径后缀、令牌不校验草稿、keep 当作 move、忽略大小上限、规则外直接删除、忽略等待编目）均被这些测试捕获。
  - AI 与搜题（`server/ai_integration_test.go`）：AI 识别端到端、白名单字段自动应用、人工锁定字段不被重新决策覆盖；错误密钥不回显；搜题索引、页码、公式检索与重建索引幂等。
  - 权限与 MCP：API 密钥直连 REST、作用域与撤销；MCP 工具按 scope 可见、参数校验、非法 JSON 帧与超大请求被拒、委派管理不能越权、网盘密钥不回显；MCP 工具目录覆盖全部业务路由；未鉴权返回 401。
  - 节点（`server/node_integration_test.go`，`service/nodes_test.go`、`jobs_concurrency_test.go`）：从端无数据库执行副本任务、节点令牌与用户令牌隔离、只能上报自己租约的任务；租约竞争、过期租约回收与资料锁；从端崩溃、排空、停用和主端重启由真实进程场景覆盖。
  - 重复或只校验实现细节的端到端用例已删除，相应行为由 `service`、`jobexec`、`worker` 包的单元测试覆盖（公式转义修复、补建索引、DOCX 预览、本地网盘副本只在主端执行等）。
- **前端**：上传清单、路径校验与文件匹配、二进制上传、API 客户端与遥测、路由清单与查询键、网盘表单字段、API 密钥表单、节点表单、AI 权限与格式化、搜题页与高亮。

### 本次未重新验证（沿用重构前结论，需在套卷模型上复测）

- 2026-10-28：`docker compose build` / `up -d`；`postgres:16-alpine` 一次性迁移、两个应用实例、Nginx 单端口网关均健康，登录与资料列表实测成功。本机当前没有 Docker，CI 只校验 Compose 配置，不启动容器。
- 2026-10-28：RapidOCR 在远端 Compose 对真实 6 页扫描 PDF 完成 OCR 与分析全链路。

模型网关若与项目同机，可在部署时叠加 `deploy/docker-compose.new-api.yml`，让应用经不暴露端口的内部 socat 桥接访问网关，避开公网入口对长请求的中断；默认 Compose 不依赖该外部网络。

### 主从节点（2026-11-07，本机）

用真实二进制分别启动主端和从端进程，网盘、OCR 和模型网关为本机回环上的模拟服务。SQLite 与 PostgreSQL 17 各通过 29 项检查：云盘任务在从端执行、`local` 网盘任务在主端执行、节点监控、从端 `SIGKILL` 后任务由其他节点完成、`SIGTERM` 后立即回队列、排空不空转且恢复及时、停用立即拒绝并中止上传、主端重启后任务继续、AI 识别与索引在从端执行、日志中没有密钥。

未验证：真实云盘、MinerU、模型网关，跨机器的网络环境，Compose 启动（本机没有 Docker）。

### 未验证（不得写成已通过）

- 真实蓝奏云、123 云盘、WebDAV、OneDrive、S3 的联网上传、下载与分享；
- 真实 MinerU、PaddleOCR 账号，真实 OpenAI 兼容网关的质量、限流和费用；真实 Word 版式、PPT 和大规模并发；
- 生产部署（Compose、TLS 反向代理、跨机器的从端）；
- 存储页与文件详情以外页面的浏览器逐页人工操作、真实 Agent 客户端、真实大文件与长文档；
- 任务运行指标、租约告警与按上游配额动态限流。

## 17. 已知边界与后续方向

1. **节点运维**：监控过期租约和任务失败率，主从版本一起升级。
2. **真实网盘**：WebDAV/蓝奏/OneDrive/S3 的上传、移动、删除和分享需要真实服务联调；123 云盘在官方接口可核对前保持 unsupported，规则不能把它作为目标。
3. **网盘同步**：套卷级例外与全局附加目标尚未实现；蓝奏云不支持移动和删除；`forget` 只移除记录，远端文件需自行清理。
4. **AI/索引质量**：RapidOCR 已对真实 6 页扫描试卷完成识别，但 OCR 纠错、切题、页码准确率和大文件成本仍未系统验证；分析与索引可继续共享解析缓存。
5. **数据治理**：孤儿 CAS、预览缓存、临时文件和过期成功任务由每日保留期清理处理（见 DATA.md）；仍缺少版本化迁移，长期未完成的上传会话不会自动清理。
6. **安全加固**：管理员可配置任意 http(s) 上游地址；未实现通用出站 SSRF 允许列表、全局 API 限流或主密钥轮换流程。
7. **产品能力**：不提供通用全文检索、Office 转换、病毒扫描、跨请求分片断点续传、Range 下载、跨资料同题聚合。
8. **搜索设置**：目前只有 HTTP 接口，没有独立管理台页面；索引状态使用 pending/running/succeeded/failed，非试卷类资料显示 not_applicable。

## 18. 主从节点

一个主端、任意多个从端。主端持有数据库、CAS 和凭据，负责鉴权和调度；从端只领取任务、执行、回报，互不通信，并且只需要能主动连到主端。部署步骤见 [DEPLOYMENT.md](DEPLOYMENT.md#6-主从节点)，接口见 [API.md](API.md#节点)。

### 18.1 主端管理的运行配置

`cluster_settings` 单例保存主端可执行的 `replicate`/`analyze`/`index` 类型、主端归档/复制/分析/索引并发、租约、心跳、领取等待和空闲轮询。管理台保存后更新全局修订号并递增所有从端的 `config_revision`，同时唤醒主端调度协程，下一轮按新并发计算空闲容量；正在执行的任务继续完成，缩容只在它们结束后生效。

每个从端节点的期望槽位、缓存上限和可用 OCRProvider ID 保存在 `nodes`。首次同步心跳在领取任务前取得完整配置，之后根据修订号重启领取循环。`applied_config_revision` 由心跳回报，管理台据此区分“已应用”和“等待应用”。`ROLE=worker`、`MASTER_URL`、`NODE_TOKEN` 和本机 `CACHE_DIR` 是从端断网或首次启动前必须具备的接入信息；其余运行参数不读环境变量。

OCR 配置不使用按引擎写死的从端地址。节点只声明自己能访问哪些已注册的 RapidOCR/PaddleOCR/MinerU 服务；选择多个时按全局 `OCRProvider.priority` 调度。任务输入仍使用统一的 OCR 配置结构，分析、索引和解析缓存不需要识别引擎专用分支。

### 18.2 任务三段化

`replicate`、`analyze`、`index` 分成三段，单机和集群走同一条代码路径：

| 阶段 | 位置 | 内容 |
| --- | --- | --- |
| 准备 | 主端，读库 | `PrepareJob` 生成自包含的 `jobexec.Input`，包括解密后的凭据 |
| 执行 | 任意节点，不访问数据库 | `jobexec.Run` 通过 `Env` 取原件、写进度、回写 token |
| 提交 | 主端，单个事务 | `CommitJob` 校验租约后写库；AI 输出照旧走白名单 |

`ingest`、`group`、`decide`、`entity` 和 `local` 网盘的复制只在主端运行（分组、编目和名称去重需要读取资料库候选）。`local` 网盘的目录在主端 `DATA_DIR` 内，如果交给从端，从端会写到自己的磁盘，主端却记录副本就绪，所以领取时用 `claimer.RemoteOnly` 按网盘类型过滤。

### 18.3 调度与租约

主端和从端都用“一个领取者 + 按需协程”的模型，空闲成本与槽位数无关：

- **主端**：一个调度协程。每轮读取各类型空闲容量（并发上限减运行数），在一个事务里为所有有空位的类型批量领取，网盘路由只读一次；每个领到的任务起一个协程，任务结束即释放槽位并唤醒调度。下游阶段（entity、decide、group、index、analyze）先于 replicate、ingest 领取，已开始的链路先走完。
- **唤醒**：入队、提交结果（产生下一阶段任务）、任务结束和设置变更都会合并唤醒调度，链路各阶段之间不等待轮询。空闲时兜底轮询从「空闲轮询」间隔开始倍增，最长 30 秒，且不晚于最近一个 `next_attempt_at` 到期的重试。
- **续租**：主端本地运行的全部任务共用一个续租协程，每个心跳周期一个事务续租任务和 `job_locks`，失去租约或资源锁的任务被取消；没有运行任务时协程退出。
- **从端**：一个领取循环，以 `{"capacity":{kind:空闲数}}` 一次长轮询所有空闲槽位，主端在一个事务里批量领取并返回 `{"jobs":[...]}`；每个任务一个协程，槽位全满时不发请求，等任务结束再领取。空闲从端对主端只保持一个长轮询。
- 任务内部的扇出仍各自有界：Word 内嵌图片 OCR 并发 2，题目索引批次并发 3。

- `lease_owner` 是节点 ID。从端按主端下发的间隔心跳（默认 10 秒），主端在同一事务内为上报的任务续租，并返回已失去租约的任务。
- 从端断线超过租约时长后，reaper 回收任务；旧执行者的提交因 `lease_owner` 不符被拒绝。
- 同一节点可能在断线后重新领回同一任务，此时租约归属无法区分两次执行，所以由从端进程取消旧的执行。
- 领取请求长轮询，排空中或没有可执行类型的节点也会等满超时，从端不会空转；排空和停用状态在等待期间重新读取。
- 主端重启不会重置从端的任务，只恢复 `lease_owner` 为主端自身的任务。
- 取消任务（`POST /jobs/:id/cancel`）在一个事务里清除租约并按该类型的最终失败写入投影（与“多次中断”停止的任务共用 `stopJobTx`）；主端本地执行立即取消，从端经下一次心跳的 `lost_job_ids` 停止，迟到的结果被租约栅栏拒绝。

### 18.4 从端的原件与凭据

- 原件缓存在从端本机 `CACHE_DIR/blobs/<sha>`，读取和下载后都校验 SHA-256；断线时用 Range 续传，同一原件只下载一份。淘汰按 LRU，运行中任务用到的文件不会被淘汰；上限由主端节点配置下发。
- 主端为从端准备 replicate/analyze/index 任务时，在 `Blob.sources` 中附上该原件已就绪（ready 或 orphaned）的云端副本及其凭据，按网盘 `read_order` 排序。只包含启用的非 local 网盘，且与上传使用同一条路由规则：`pool` 网盘对所有从端开放；`preferred`/`exclusive` 网盘只对当前在线、优先级最高的路由节点开放，路由节点全部离线时 `preferred` 对所有从端开放，`exclusive` 不开放。每个从端只拿到它会上传的那些网盘的凭据。从端先直接从这些副本读取，校验大小和 SHA-256 后放入本机缓存；网盘不支持下载、读取失败或内容不符时依次尝试下一个副本，最后才从主端下载，主端缺少原件时再由主端从云端副本读取并转发。这样原件不必经过主端出口。
- 凭据只在任务输入里，从端不落盘，不写日志和进度。节点令牌只保存哈希。
- 结果内联在 `complete` 请求里（gzip，上限 32 MiB）；主端拒绝的结果会报告为任务失败，避免反复重排。

### 18.5 监控

每个节点按当前心跳周期采样 CPU、内存和网络。`nodes` 表只保存最新快照，历史在主端内存里，每节点 360 个点，重启后清空。状态由心跳时间推导：30 秒内在线，到租约时长为失联，超过为离线。

### 18.6 上传路由

每个远程网盘可以在管理台配置 `pool`、`preferred` 或 `exclusive` 路由，并为主端/从端设置不重复的优先级。领取复制任务时，主端会在数据库事务内根据网盘、节点状态和路由优先级过滤候选任务：首选节点在线时只有它领取；节点停用、排空或租约时间内未心跳时切换到下一级；`preferred` 在全部配置节点不可用时回退到普通节点池，`exclusive` 则保持 pending。上传完成后的副本提交和资料级 `sync_status` 汇总仍由主端事务完成。

### 18.7 尚未实现

- `ingest` 下放、按节点标签批量分配任务；
