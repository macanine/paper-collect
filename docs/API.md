# HTTP API v1 接口合同

版本 1.3.0 · 2026-10-28。基础路径 `/api/v1`；MCP 路径 `/mcp`。以下为实现合同，不是云盘真实联调完成声明。

## 通用规范

- JSON 使用 UTF-8、snake_case；时间 UTC RFC3339；ID 为不透明字符串（UUID），SHA-256 是 64 位小写十六进制。int64 字节数受 2 GiB 批次上限保护，可由 JS number 精确表达。
- 成功：`{"data":T,"request_id":"uuid"}`；列表 T 为 `{"items":[],"total":0,"page":1,"page_size":20}`。错误：`{"error":{"code":"validation_error","message":"说明"},"request_id":"uuid"}`。不得把秘密或堆栈写进 message。
- 默认分页 `page=1&page_size=20`，page >= 1，page_size 1..100，稳定排序 created_at DESC、id DESC。未声明的分页/过滤参数不作为业务条件。
- 写接口拒绝非法 JSON、未知字段、不支持的枚举、越界值。PATCH 是部分更新：省略=不改，空数组=清空，禁止 null（允许 null 的字段另写）。请求 JSON 默认 1 MiB。
- HTTP：200 读取/更新；201 创建；202 任务已入队；204 删除/通知；400 格式错误；401 凭据无效；403 权限不足；404 不存在；409 状态冲突/重复路径；413 超尺寸；415 不支持类型；422 文件内容与声明不一致；429 登录频率限制；503 暂不可用。
- GET /healthz 无鉴权返回 `{"status":"ok"}`。所有业务接口强制鉴权，仅 auth/login、auth/refresh、auth/agent-token 例外。
- 浏览器与 Agent 的后续 REST/MCP 请求统一使用 `Authorization: Bearer <access_token>`，不需要再提交 API Key。Agent 仅在换取 JWT 时提交一次 `X-API-Key: pc_<id>.<secret>`。MCP 仅接受代理 JWT。
- 角色：admin（全部）、editor（资料/合集写、上传、任务读）、viewer（资料/合集/任务读）。scope 包括 materials、collections、uploads、jobs、storage、keys、users、audit、ai 和 search 对应的读写或管理权限。Agent scopes 必须为用户权限子集，禁止 keys:manage、users:manage、ai:read、ai:write；搜索设置接口也拒绝 Agent。每次请求重新检查用户和密钥是否有效。

## 数据字典

`Subject.code`：chinese 语文、math 数学、english 英语、physics 物理、chemistry 化学、biology 生物、politics 政治、history 历史、geography 地理、information_technology 信息技术、general_technology 通用技术、comprehensive 综合。`GET /subjects` 返回 `Subject[]{code,name}`。

`Material`：`id:string,title:string(1..255),description:string(0..4000),subject_codes:string[](0..12),grade:string(""|grade10|grade11|grade12),academic_year:integer(0 或 1900..2200),tags:string[](<=30，每项1..64),role:Role,blob_id:string,file_name:string,media_type:string,size_bytes:integer,sha256:string,source_path:string,parent_material_id:string(无则空串),status:pending_storage|archived|degraded,created_at:date-time,updated_at:date-time`。**Role 有且只有 6 类**：exam=试卷，只有试题、没有答案；answer=答案，只有答案或答题卡；questions_answers=试卷+答案，前部是完整试卷、后部集中放答案；teacher_edition=教师版，每道题后紧跟该题答案或解析；lecture=讲义，属于教程型材料，知识讲解、例题、说明和练习混杂，不是纯试题；other=确实无法判断。详情额外含 `replicas:Replica[]`。

`Collection`：`id,title(1..255),description(<=4000),kind:series|exam|folder|zip|custom,parent_id:string(无则空串),subject_codes:string[],created_at,updated_at`。详情额外含 `items:CollectionItem[]`。`CollectionItem`：`id,collection_id,material_id,position:integer(>=0),role:Role,relative_path:string,material?:Material`。`role` 永远取自资料本身，写入时忽略客户端传值，读取时按资料角色返回；同合集同资料唯一；禁止父级循环。

`Upload`：`id,title,collection_kind,subject_codes,grade,academic_year,tags,status:pending|processing|completed|failed,collection_id,material_id,job_id,created_at,entries:UploadEntry[]`。`UploadEntry`：`id,relative_path,size_bytes,sha256:string(可空),status:pending|received`。status=received 表示字节数、内容签名和 SHA 已验证。单个普通文件归档后返回 `material_id`，批次返回 `collection_id`；folder 使用 `webkitRelativePath`，不上传操作系统路径。

`Job`：`id,kind:ingest|replicate|analyze|index,status:pending|running|succeeded|failed,upload_id:string,replica_id:string,material_id:string,attempts:integer,error:string,created_at,updated_at`。重试返回同一 job_id，已完成任务禁止重试；入队和启动恢复保证未完成工作不会仅存在内存中。PostgreSQL 支持带租约和心跳的多实例领取；SQLite 仅支持单实例。

`StorageProvider`：`id,name(1..128),kind:local|webdav|lanzou|123pan,role:primary|backup,enabled:boolean,priority:integer(0..1000),weight:integer(1..1000),config:object<string,string>,secret_configured:boolean,created_at,updated_at`。任何 GET 不返回 secret。`Replica`：`id,blob_id,provider_id,status:pending|uploading|ready|failed,size_bytes,verification:uploaded|size|sha256,error,share_status:pending|ready|unsupported|failed,share_url:string,share_password:string,share_expires_at:date-time|null,created_at,updated_at`。远端对象 ID 不开放给普通资料接口。

`StoragePolicy`：`mode:mirror_all|primary_backup|balanced,minimum_replicas:integer(1..20)`。默认 mirror_all/1。mirror_all 与 primary_backup 都写所有启用主备；balanced 在主端按权重选 minimum_replicas 个不同端点（主端不足时取全部主端），再加全部备端。读取按主端优先、priority 升序；失败切换。目标不足不能标 archived；一个 Blob/Provider 仅一个副本，复制重试不会重新选择已确定目标。

`APIKey`：`id,name,scopes:string[],expires_at:date-time|null,revoked:boolean,created_at`。`User`：`id,username,role:admin|editor|viewer,enabled:boolean,created_at`。`Audit`：`id,user_id,action,target_id,created_at`（仅存元数据，不存正文/凭据）。

## 认证与管理

### POST /auth/login（公开）

JSON `{"username":"admin","password":"至少12位的用户口令"}`。200 data=`{"access_token":"JWT","refresh_token":"opaque","expires_in":900,"user":User}`。错误凭据统一 401；登录有节流。默认没有固定管理员密码，启动时通过环境变量自举。

### POST /auth/refresh（公开）

JSON `{"refresh_token":"opaque"}`。200 同登录；旧 refresh token 立即失效，refresh 最长 7 天，服务端仅存哈希。

### POST /auth/logout（JWT）

JSON `{"refresh_token":"opaque"}`。204；注销关联会话，同时使其 access JWT 失效。无效 token 也返回 204，不泄露会话存在性。

### POST /auth/agent-token（X-API-Key）

无需请求体。200 data=`{"access_token":"JWT","expires_in":900}`。JWT 带 key_id；后续 REST/MCP 只提交该 Bearer JWT，不再提交 `X-API-Key`。每次请求会按 key_id 重查密钥撤销/过期状态与用户启用状态，因此密钥撤销或禁用用户后，已签发 JWT 立即 401。JWT 最长 15 分钟。

### GET /auth/me（JWT）

200 data=User。Agent 同样只使用 Bearer JWT。

### GET /users · POST /users（users:manage，禁止 Agent）

GET 分页，200 data=Page<User>。POST JSON `{"username":"alice","password":"至少12位","role":"editor"}`，201 data=User；username 3..64，只允许字母数字下划线连字符，唯一。

### GET /api-keys · POST /api-keys · DELETE /api-keys/{id}（keys:manage，禁止 Agent）

GET 分页，200 Page<APIKey>。POST `{"name":"检索 Agent","scopes":["materials:read","collections:read","jobs:read"],"expires_at":null}`；name 1..128，至少一个有效 scope，过期时间须在未来。201 data=`{"key":APIKey,"secret":"pc_<id>.<secret>"}`，secret 仅此一次返回。DELETE=撤销，204；旧 JWT 也立即失效。

### GET /audit-logs（audit:read）

分页，200 Page<Audit>。仅审计元数据。

## 资料与合集

### GET /subjects（materials:read）

200 data=`Subject[]{code,name}`，返回固定学科字典；顺序固定，不随资料变化。

### GET /materials（materials:read）

查询：page、page_size、`q`（标题/描述，<=200）、`subject`（Subject.code）、`role`（Role）、`status`、`grade`。200 Page<Material>，过滤在 DB 执行；综合按实际 subject_codes 匹配，不自动扩大到全部资料。

### GET /materials/{id}（materials:read）

200 Material + replicas。分享 URL/提取码只在此有权限响应中返回，不记录日志。404 不存在。

### PATCH /materials/{id}（materials:write）

JSON 可含 title/description/subject_codes/grade/academic_year/tags/role（约束见字典），至少一个字段；不允许覆盖 blob、SHA、上传路径、status。200 **Material + replicas**（与详情同形）。更新 title 会同步文件名和本地副本名（保留扩展名）；云端对象保留原名。文件名角色建议可在此纠正。

### GET /materials/{id}/download（materials:read）

200 二进制，Content-Type 取已验证媒体类型；Content-Disposition attachment，UTF-8 文件名；X-Content-Type-Options: nosniff。同源 JWT 通过 fetch Blob 下载，不将 Token 放入 URL。先检查本地原件，再按主备优先级尝试可读云副本；没有可用副本 503。Range 请求本期不承诺。

### GET /collections · POST /collections（分别 collections:read / collections:write）

GET page、page_size、`q`（标题/描述，<=200）、`roots=true`（仅顶层合集）、`parent_id=<合集ID>`（指定父合集的子合集），200 Page<Collection>。POST JSON `{"title":"高三数学月考","description":"","kind":"exam","parent_id":"","subject_codes":["math"]}`；201 Collection。parent_id 若非空必须存在。

### DELETE /materials/{id}（materials:write）

默认 204。若资料是 ZIP 容器且存在 `parent_material_id` 指向它的子资料，必须显式 `?cascade=true`，否则 409；级联删除会移除子资料、合集条目、分析记录、对应 `analyze` 任务，以及题目/倒排/解析缓存，并把引用它的上传记录 `material_id` 置空。无剩余资料引用的 Blob 会删除本地 CAS 文件与本地端点副本。**云盘适配器没有删除接口，远端对象不会被删除**，其副本行会被移除；需要删除远端文件时请自行处理。审计记录 `material.delete`。

### DELETE /collections/{id}（collections:write）

默认 204，仅删除合集及其条目，资料本身保留。存在子合集（如 ZIP 子合集）时必须 `?cascade=true`，级联删除所有后代合集；引用这些合集的上传记录 `collection_id` 置空。审计记录 `collection.delete`。

### GET /collections/{id} · PATCH /collections/{id}

GET 200 Collection + items；PATCH 只接收 title/description/kind/parent_id/subject_codes，禁止循环，200 Collection。对应 read/write scopes。

### PUT /collections/{id}/items（collections:write）

JSON `{"items":[{"material_id":"uuid","position":0,"role":"exam","relative_path":"试卷/学生版.pdf"}]}`；0..5000 条，禁止重复资料，所有资料必须存在。事务整体替换，200 Collection + items。空数组清空条目，不删除资料或副本。

## 清单上传与归档

### POST /uploads（uploads:write）

JSON：

```json
{"title":"2026 高三数学练习","collection_kind":"series","subject_codes":["math"],"grade":"grade12","academic_year":2026,"tags":["一轮复习"],"entries":[{"relative_path":"练习/1.pdf","size_bytes":12345,"sha256":""},{"relative_path":"练习/2.docx","size_bytes":54321}]}
```

201 data=Upload（包含服务器分配的 entry.id）。title、entries 必需，subject_codes 可为空；其余默认 custom/空/0。每条 relative_path 必须唯一，1..1024 UTF-8 字节，正斜杠分隔，拒绝绝对路径、反斜杠、..、空路径段。size_bytes 1..536870912；清单总和 <=2147483648；1..2000 条。支持 .zip/.pptx/.pdf/.docx/.doc（不区分大小写）。可选的 sha256 必须为 64 位十六进制；服务端在接收时仍会自行计算并比对。entries 按自然文件名顺序分配 position 后返回。新上传对用户隔离，非创建者仅 admin 可访问。此处只创建上传记录，不创建空资料。

### GET /uploads/{id}（uploads:write）

200 Upload，用 entries.status 恢复中断上传；已收到的文件无需重发。

### PUT /uploads/{id}/files/{entry_id}（uploads:write）

`Content-Type: application/octet-stream`，请求体为单文件原始字节，必须与声明字节数一致，允许 Content-Length 或 chunked。200 data=UploadEntry。验证真实格式和 SHA；重复上传相同已接收文件返回原结果；received 项不得改成不同内容。错误 409（会话已 finalize）、413（超声明/上限）、415（签名不符）、422（字节数/SHA 不符）。流写临时文件，校验通过后原子替换，失败不记 received。

### POST /uploads/{id}/finalize（uploads:write）

无需请求体。所有文件 received 才可完成，否则 409。202 data=`{"job_id":"uuid","upload_id":"uuid"}`；对同一上传重复 finalize 返回同一任务，禁止重复创建资料。

归档任务：`collection_kind=custom` 且清单只有一个无目录层级的普通文件时，直接创建资料，不创建合集；`collection_kind=zip` 且只有一个 ZIP 时，创建一个合集，包含 ZIP 原件和展开成员（无子合集）；其他批次创建清单合集，批次中的每个 ZIP 创建子合集，清单合集放顶层文件与 ZIP 原件，ZIP 成员放在子合集。`folder` 保留浏览器给出的相对路径。展开成员记录 source_path/parent_material_id。ZIP 内不支持的文件保留在原 ZIP，未执行/提取；嵌套 ZIP 本身作为成员保留，禁止递归。拒绝路径穿越/符号链接/重复规范化路径/加密 ZIP/炸弹；坏 ZIP 不产生部分已发布资料；任一成员真实格式与扩展名不符也会拒绝整个 ZIP。展开上限 2 GiB、5000 条（统计所有非忽略文件）、压缩比 200。成员按自然文件名顺序 position；角色仅文件名规则推测，默认为 other。

## 任务与存储

### GET /jobs · GET /jobs/{id}（jobs:read）

列表查询 page/page_size/status/kind，200 Page<Job>；详情 200 Job。

### POST /jobs/{id}/retry（jobs:write）

仅 failed 可重试，202 Job（同 id，status=pending）。其他状态 409。秘密错误须脱敏。

### GET /storage/providers · POST /storage/providers

GET storage:read，返回 `StorageProvider[]`（配置数量小，不分页）。POST storage:write，201 Provider。JSON：

```json
{"name":"学校 WebDAV","kind":"webdav","role":"backup","enabled":true,"priority":20,"weight":1,"config":{"base_url":"https://dav.example/dav","path_prefix":"paper-collect"},"secret":{"username":"user","password":"secret"}}
```

config 与 secret 均 object<string,string>。字段按适配器：

| kind | config | secret |
| --- | --- | --- |
| local | root 由服务端固定，API 不允许任意路径 | 空 |
| webdav | base_url 必需（http/https，无内嵌凭据）；path_prefix 可选；public_base_url 可选（真正公开目录的根 URL） | username/password 必需 |
| lanzou | base_url 可选默认 https://pc.woozooo.com；folder_id 默认 -1；verify_remote_hash 可选 "true"/"false"（默认 false，开启后上传会再下载重算 SHA，耗时且失败时远端可能残留文件） | cookie 必需（管理员有效登录会话） |
| 123pan | base_url 默认 https://open-api.123pan.com；parent_file_id 默认 0 | client_id/client_secret 必需（开放平台申请） |

省略 role/enabled/priority/weight 时默认 primary/true/100/1；config 或 secret 中出现该 kind 不支持的键会被拒绝。创建 local 限定到服务端数据目录。API 不返回传入 secret，`secret_configured` 只表示已保存，不代表已连通。

适配器能力边界（实现事实，非联调声明）：

- local：上传、下载可用；不支持分享（share_status=unsupported）。未提供 SHA 时 verification 只能是 size。
- webdav：上传、下载可用；未配置 public_base_url 时分享不支持，配置后返回公开目录拼接链接，不代表该 URL 可匿名访问。提供 SHA 时上传后会完整下载重算。
- lanzou：使用非官方接口，按 cookie 会话工作；单文件上限 100 MiB，超限返回 unsupported；默认 verification=uploaded，开启 verify_remote_hash 才可能为 sha256；分享返回 URL 与提取码。未对真实服务联调。
- 123pan：当前只实现 access_token 与 Health 探测；upload/download/share 返回 unsupported（错误码 123pan_upload_unverified 等），Capabilities 全为 false。**不应把 123pan 作为可上传端点启用**；复制任务会失败并记录该错误，而不是伪造成功。

### PATCH /storage/providers/{id}（storage:write）

可修改 name/role/enabled/priority/weight/config/secret；kind 不可改。config 若出现整体替换；secret 若出现整体替换，空对象清空，省略保留。200 Provider。停用不删除远端内容，无法删除唯一可用副本的保护由运维流程负责；系统不提供物理删除云盘接口。

### POST /storage/providers/{id}/test（storage:write）

执行真实 adapter.Health，200 data=`{"healthy":true|false,"message":"脱敏说明","capabilities":{"upload":true,"download":true,"share":false}}`；连接/认证失败 healthy=false，不伪造成功。适配器实现与实际云盘连通是两个独立事实。

### GET /storage/policy · PUT /storage/policy

GET storage:read，200 StoragePolicy；PUT storage:write，JSON 完整 policy，200 StoragePolicy。修改只影响新任务；历史副本通过 reconcile 补齐。

### POST /storage/reconcile（storage:write）

无需请求体。按当前策略为已有 Blob 补建缺少副本任务，不删除额外副本。202 data=`{"queued":integer}`。不会自动把失败任务当成功；失败副本可经任务重试处理。

### POST /storage/replicas/{id}/share（storage:write）

刷新/补建已有 ready 副本分享；200 Replica。provider 不支持分享时 share_status=unsupported、share_url 为空；远端分享失败返回 share_status=failed、脱敏 error，副本仍 ready。URL 不等于下载校验成功，verification 单独记录。

## 智能识别（LLM + OCR）

AI scope：`ai:read`（读取模型/OCR 配置与解析设置）、`ai:write`（修改配置、探测、测试）。两者默认只授予 admin；Agent 密钥不得包含。资料分析读取沿用 materials:read，触发与应用沿用 materials:write。密钥只写不读：响应只返回 `api_key_configured`、`token_configured`、`mineru_configured` 和 `ocr_configured`；服务端用 MASTER_KEY 加密，AAD 为 provider id（旧 `ai_settings` 字段 使用固定串 `ai:settings`，迁移时重新绑定到新 provider id）。

`AIProvider`：`id,name(1..128),base_url(http/https，OpenAI 兼容根，通常以 /v1 结尾),models:string[](0..200，每项 1..200 字符，自动去重),default_model:string(<=200),vision_model:string(<=200，可空),enabled:boolean,priority:integer(0..1000),api_key_configured:boolean,created_at,updated_at`。`POST /ai/providers` 请求 `{"name":"主模型","base_url":"https://api.example.com/v1","api_key":"sk-...","priority":10,"enabled":true,"default_model":"gpt-4o-mini","vision_model":"gpt-4o","models":["gpt-4o-mini","gpt-4o"]}`；创建时 `api_key` 可省略（表示没有密钥；需要鉴权的端点会失败），PATCH 时省略保留、空串清空。PATCH 部分更新，不允许改 id；不提供物理删除，使用 `enabled=false` 停用。

- `GET /ai/providers`（ai:read）：200 数组，按 priority 升序。
- `POST /ai/providers`（ai:write）：201 AIProvider。
- `PATCH /ai/providers/{id}`（ai:write）：200 AIProvider。
- `POST /ai/providers/probe`（ai:write）：请求 `{"base_url":"...","api_key":"..."}`；提供 `provider_id` 时 `base_url` 可省略（复用已存地址），`api_key` 可省略表示复用该 provider 的已存密钥；同时提供 api_key 时以请求中的密钥为准。调用 `GET {base_url}/models`，200 `{"models":["..."],"count":2}`；远端错误按 502/503 返回脱敏原因，不得回显密钥。
- `POST /ai/providers/{id}/probe`（ai:write）：使用已存密钥探测，只返回模型列表，**不修改**已保存的 models；需要保存列表时在 PATCH 请求中提交 models，200 `{"models":[],"count":0}`。
- `POST /ai/providers/{id}/test`（ai:write）：请求 `{"model":""}`，空则用 default_model；发一次最小 chat，200 `{"healthy":true|false,"model":"...","message":"脱敏说明"}`。远端失败返回 healthy=false，不伪造成功。

`OCRProvider`：`id,name(1..128),kind:mineru|paddleocr|rapidocr,base_url(http/https),model(<=200；PaddleOCR 默认 PP-OCRv6，RapidOCR 默认 PP-OCRv5，MinerU 可为空),enabled:boolean,priority:integer(0..1000),token_configured:boolean,created_at,updated_at`。请求字段为 `{"name":"RapidOCR（自建）","kind":"rapidocr","base_url":"http://rapidocr:9003","model":"PP-OCRv5","enabled":true,"priority":10}`；自建 RapidOCR 可以省略 `token`。外部 PaddleOCR/MinerU 的 `token` 只写不读，PATCH 省略保留、空串清除。kind 创建后不可修改；不提供物理删除，使用 `enabled=false` 停用。PaddleOCR token 从 [AI Studio PaddleOCR 任务页](https://aistudio.baidu.com/paddleocr/task) 获取。

- `GET /ocr/providers`（ai:read）：200 OCRProvider 数组，按 priority 升序。
- `POST /ocr/providers`（ai:write）：201 OCRProvider。缺少 `kind` 时按 paddleocr；PaddleOCR 缺少 model 时默认 `PP-OCRv6`，RapidOCR 缺少 model 时默认 `PP-OCRv5`。
- `PATCH /ocr/providers/{id}`（ai:write）：200 OCRProvider。
- `POST /ocr/providers/{id}/test`（ai:write）：用已保存配置探测可达性与令牌；200 `{"healthy":boolean,"message":"脱敏说明"}`，不伪造成功。

`AISettings`：`analysis_enabled:boolean`（默认 false）,`ocr_provider_id:string`（当前选中的 OCRProvider；为空时选择 priority 最小的启用服务商）,`mineru_base_url:string`（旧字段，仅兼容保留）,`max_pages:integer(1..10，默认 3；参与限制发送给视觉模型的图片数量，OCR 仍解析全文，文本另有长度上限)`,`max_images:integer(0..4，默认 2)`,`mineru_configured:boolean`（旧令牌状态）,`ocr_configured:boolean`（当前服务商令牌状态）,`updated_at`。

- `GET /ai/settings`（ai:read）：200 AISettings。
- `PUT /ai/settings`（ai:write）：请求完整设置，`ocr_provider_id` 可选：省略=保留，空串=清除选择；`mineru_token` 为旧 MinerU 兼容字段：省略=保留，空串=清除；200 AISettings。旧版仅提交 MinerU 字段的客户端会继续工作，并自动物化/更新 MinerU OCRProvider。
- `POST /ai/mineru/test`（ai:write）：兼容旧客户端，实际测试当前选中的 OCR 服务商；200 `{"healthy":boolean,"message":"脱敏说明"}`。新客户端应使用 `POST /ocr/providers/{id}/test`。

升级时若 `ocr_providers` 为空且旧 `ai_settings.mineru_token_cipher` 有值，启动/读取设置会幂等迁移为一条 MinerU 服务商并设为当前服务商。PaddleOCR 使用 `https://paddleocr.aistudio-app.com` 的 `POST /api/v2/ocr/jobs`、`GET /api/v2/ocr/jobs/{jobId}` 与 JSONL `jsonUrl`；`PP-OCRv6` 沿用官网 v5 兼容异步协议。自建 RapidOCR 使用 `POST /api/v1/ocr/jobs`、`GET /api/v1/ocr/jobs/{id}` 和 `result_url` JSONL；服务地址在 Compose 内为 `http://rapidocr:9003`，不对外暴露。

`MaterialAnalysis`：`id,material_id,status:pending|running|succeeded|failed,provider_id,provider_name,model,page_count,image_count(恒为 0，页面图不落盘),confidence:number(0..1),suggestions:{title?,subject_codes?,grade?,academic_year?,role?,tags?},markdown_excerpt:string(<=4000),error:string,applied_at:date-time|null,created_at,updated_at`。`applied_at` 表示最近一次自动或手动应用建议的时间。

- `POST /materials/{id}/analysis`（materials:write）：请求可空或 `{"force":false}`；pending/running 返回 202 现有分析；succeeded 且 force=false 返回现有；failed 或 force=true 重新入队。202 data=`{"job_id":"uuid","analysis":MaterialAnalysis}`。
- `GET /materials/{id}/analysis`（materials:read）：200 MaterialAnalysis；尚无分析时返回 404 `not_found`。
- `POST /materials/{id}/analysis/apply`（materials:write）：重新应用已成功完成的建议。请求可覆盖白名单字段 `{"title":"","subject_codes":[],"grade":"","academic_year":0,"role":"","tags":[]}`，省略字段取建议值；显式提供的白名单字段也可覆盖或补充建议。应用前与资料 PATCH 使用同一套校验；200 更新后的 **Material + replicas**，不能修改 blob/状态/路径。写审计 `material.ai_apply`。

分析链路：归档完成后若 `analysis_enabled` 自动入队（ZIP 容器本身不入队，展开成员各自入队）；任务读取原件 → 当前选中的 OCRProvider 解析（RapidOCR 自建异步任务、PaddleOCR 异步任务或 MinerU v4） → 统一 Markdown/逐页文本 → 有 vision_model 时以 Markdown + 前 max_images 张图片调用视觉模型，否则纯文本调用 default_model → 严格 JSON 校验建议 → **在同一事务中自动应用白名单字段、同步文件名和分析已应用标记**。归类以正文（或页面图像）为依据，文件名和来源路径只作辅助；提示明确禁止仅凭文件名判断，正文与文件名冲突时以正文为准。长文档最多发送 12000 字符，保留开头约 2/3 与末尾约 1/3，并标注中间省略，以免漏掉卷末答案区；未解析出任何正文或页面图像时任务失败，不退回文件名猜测。自动应用只接受 `parseSuggestions` 校验后的字段，标题会同步为下载名和本地副本名；模型原文、未知字段和非法值不会写入。自动应用写审计 `material.ai_auto_apply`，人工重应用写 `material.ai_apply`。模型 provider 按 priority 故障切换；失败错误必须脱敏。未配置 OCR 服务商或外部服务令牌时任务失败并给出明确原因，不伪造结果。RapidOCR/PaddleOCR 不保存页面图，因此视觉模型只会收到文本。

Job 的 `material_id` 关联 analyze/index 等任务资料；`kind` 包含 `analyze`、`index`。

## 搜题（纯文本）

`search:read`（读取搜索设置）、`search:write`（修改搜索设置）默认仅管理员；即使 Agent 密钥带有这些 scope，搜索设置接口仍拒绝 Agent。搜索接口本身使用 materials:read。

索引由 `index` 任务完成：默认启用；归档后自动入队，索引启用时启动为关闭期间归档的资料补建。任务复用命中“文件 SHA + OCR 类型/地址/模型 + 缓存版本”的 `material_texts`，未命中时调用当前 OCRProvider → LLM 默认每批 10 页/12000 字（可配置）完成 OCR 纠错与切题 → 保存题目和页码 → 建立 CJK unigram+bigram 与连续字母/数字词倒排。不做跨资料同题聚合；同一题出现在多份试卷就是多条结果。索引不保存页面图/bbox；需要视觉图片时 MinerU 会重新解析。

- `POST /search/questions`（materials:read）：请求 `{"query":"已知函数...","subject":"math","grade":"grade12","academic_year":0,"limit":20,"offset":0}`。`query` 必填 1..2000 字；`subject`/`grade`/`academic_year` 按字典校验；`limit` 默认 20、最大 50；`offset` >= 0。只返回资料状态为 archived/degraded 且已有题目的记录。200 `{"items":[{"question_id":"...","question_no":"12","text":"完整题干（含选项/小问）","snippet":"...","highlights":[[0,6]],"score":12.3,"page_no":3,"material":{"id":"...","title":"...","file_name":"..."},"subject_codes":["math"],"collection_id":"..."}],"total":1,"took_ms":23}`。`highlights` 是纯文本字符区间，客户端自行渲染，禁止返回 HTML。
- `GET /search/questions/{id}`（materials:read）：单题详情（同上字段，score 为 0）。
- `GET /materials/{id}/questions`（materials:read）：分页 `{id,material_id,page_no,seq,question_no,text,confidence,created_at}`，按 `seq` 升序。
- `GET /materials/{id}/index`（materials:read）：`{status:pending|running|succeeded|failed,job_id,page_count,question_count,llm_calls,llm_failed_batches,error,indexed_at}`；尚无索引记录时返回 `status=pending`、`job_id=""`；后端只返回这四种状态。
- `POST /materials/{id}/index`（materials:write）：`{"force":true}` 重新索引；`force=true` 会清空解析缓存（`material_texts.source_hash`）以强制重新解析；202 `{"job_id":"...","state":{...}}`。ZIP 容器拒绝索引。
- `GET /search/settings`（search:read）：`{enabled,max_pages,batch_pages,batch_chars,provider_id,model}`。
- `PUT /search/settings`（search:write）：完整替换；默认 enabled=true、max_pages=500、batch_pages=10、batch_chars=12000；max_pages 1..5000、batch_pages 1..50、batch_chars 1000..100000。未指定 provider/model 时使用第一个启用端点及其默认模型；指定的 provider 必须存在且启用，否则任务失败。搜索设置目前只有 HTTP 接口，没有独立管理台页面。

索引失败不阻塞归档与下载；搜索只返回已有题目且资料状态为 archived/degraded 的记录。搜索不检查 `material_index_states.status`，因此重建失败时旧索引仍可搜索，状态显示失败。

## MCP

`POST /mcp`：仅接受代理 Bearer JWT；`Content-Type: application/json`。服务端不强制校验 `Accept`；成功响应为 `application/json`，不提供 SSE。JSON-RPC 2.0 单条消息，batch 不支持。`initialize` 请求 protocolVersion 支持 2025-03-26、2025-06-18、2025-11-25，响应回协商版本、serverInfo、capabilities.tools；`notifications/initialized` 返回 202，无响应体；`ping` result={}；tools/list 根据当前 scope 过滤；tools/call 必须二次检查权限。GET /mcp=405，不发 session id，属于无状态 Streamable HTTP 的 JSON 子集。

工具与入参：`materials_search{q?:string,subject?:string,page?:integer,page_size?:integer}`、`materials_get{id:string}`、`questions_search{query:string,subject?:string,grade?:string,limit?:integer}`、`collections_list{q?:string,page?:integer,page_size?:integer}`、`collections_get{id:string}`、`jobs_get{id:string}`。工具 schema additionalProperties=false；缺少/未知/类型错误参数和校验错误返回 -32602。`questions_search` 使用 materials:read，limit 1..50，返回题目文本、页码和原资料，不返回高亮/合集字段。JSON-RPC 错误 -32700 解析失败、-32600 请求非法、-32601 方法未知、-32602 参数非法；领域失败返回 content=[{type:"text",text:"错误说明"}]、isError=true。成功 content 是 JSON 序列化文本。不得接受任意 HTTP URL/文件路径来代替资料 ID，不允许工具绕过 HTTP 授权。

## 示例 Agent 调用

```sh
curl -X POST http://127.0.0.1:8080/api/v1/auth/agent-token -H 'X-API-Key: pc_<id>.<secret>'
curl http://127.0.0.1:8080/api/v1/materials -H 'Authorization: Bearer <JWT>'
curl -X POST http://127.0.0.1:8080/mcp -H 'Authorization: Bearer <JWT>' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"materials_search","arguments":{"subject":"math"}}}'
```

## 版本与扩展约定

不支持物理删除云盘文件、在线文档编辑、宏执行和跨请求断点分块合并。新增 Adapter 只实现 storage.Adapter；新增资料元数据须同时更新本文和前端 DTO；分层、任务和数据流变更同步 [ARCHITECTURE.md](ARCHITECTURE.md)。SQLite 为单实例 WAL；PostgreSQL 多实例必须配置唯一 `WORKER_INSTANCE_ID` 并共享 `DATA_DIR`。
