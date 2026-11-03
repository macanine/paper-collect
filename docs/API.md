# HTTP API v1 接口合同

版本 1.6.0 · 2026-11-03。基础路径 `/api/v1`；MCP 路径 `/mcp`。以下为实现合同，不是云盘真实联调完成声明。

## 通用规范

- JSON 使用 UTF-8、snake_case；时间 UTC RFC3339；ID 为不透明字符串（UUID），SHA-256 是 64 位小写十六进制。int64 字节数受 2 GiB 批次上限保护，可由 JS number 精确表达。
- 成功：`{"data":T,"request_id":"uuid"}`；列表 T 为 `{"items":[],"total":0,"page":1,"page_size":20}`。错误：`{"error":{"code":"validation_error","message":"说明"},"request_id":"uuid"}`。不得把秘密或堆栈写进 message。
- 默认分页 `page=1&page_size=20`，page >= 1，page_size 1..100，稳定排序 created_at DESC、id DESC。未声明的分页/过滤参数不作为业务条件。
- 写接口拒绝非法 JSON、未知字段、不支持的枚举、越界值。PATCH 是部分更新：省略=不改，空数组=清空，禁止 null（允许 null 的字段另写）。请求 JSON 默认 1 MiB。
- HTTP：200 读取/更新；201 创建；202 任务已入队；204 删除/通知；400 格式错误；401 凭据无效；403 权限不足；404 不存在；409 状态冲突/重复路径；410 临时资源已过期；413 超尺寸；415 不支持类型；422 文件内容与声明不一致；429 登录频率限制；503 暂不可用。
- GET /healthz 无鉴权返回 `{"status":"ok"}`。所有业务接口强制鉴权，仅 auth/login、auth/refresh 例外。
- 浏览器 REST 使用 `Authorization: Bearer <access JWT>`；API 密钥直接使用 `Authorization: Bearer <API 密钥>` 访问 REST 和 MCP。新密钥以 `sk-` 开头，旧的 `pc_<id>.<secret>` 仍可直接使用。MCP 接受 API 密钥，不接受浏览器会话。
- 角色：admin（全部）、editor（套卷与文件写、专题写、上传、任务读）、viewer（套卷、专题、任务读）。scope 包括 materials、collections、uploads、jobs、storage、keys、users、audit、ai 和 search 对应的读写或管理权限。API 密钥 scope 必须为用户权限子集；有相应授权时可访问管理、AI 和搜索设置接口。API 密钥创建其他密钥时只能授予自身持有的权限，创建用户时该用户角色的权限也不得超出当前密钥。新建密钥是独立凭据，不随创建者的密钥撤销而撤销。每次请求重新检查用户和密钥是否有效。

## 数据字典

`Subject.code`：chinese 语文、math 数学、english 英语、physics 物理、chemistry 化学、biology 生物、politics 政治、history 历史、geography 地理、information_technology 信息技术、general_technology 通用技术、comprehensive 综合。`GET /subjects` 返回 `Subject[]{code,name}`。

`Pack`（套卷）：`id,title(1..255),title_locked:boolean,catalog_locked:boolean,doc_type:DocType,subject_codes:string[](0..12),grade:""|grade7|grade8|grade9|grade10|grade11|grade12,term:""|first|second,cohort:integer(0 或 1900..2200),region(<=64),organization(<=128),exam_date(YYYY-MM 或空),exam_name(<=255),chapter(<=255),tags:string[](<=30，每项1..64),description(<=4000),stage:Stage,is_exam:boolean,has_questions:boolean,has_answers:boolean,has_answer_sheet:boolean,members:PackMember[],collections?:Collection[],deleted_at:date-time|null,created_at,updated_at`。`PackMember`：`id,title,file_name,part:Part,answer_start_page:integer,stage:Stage,status:MaterialStatus,media_type,size_bytes,position:integer`。`DocType`：gaokao_real、gaokao_mock、zhongkao_real、zhongkao_mock、joint、monthly、midterm、final、opening、unit、weekly、topic、lecture、study_guide、courseware、homework、other。**Stage**：processing=识别中；review=待整理（低置信度、有配套建议、疑似重复、识别失败或未开启自动识别）；ready=已入库；failed=识别失败。套卷 stage 取成员中最靠后的阶段（ready < processing < review < failed）。`has_questions/has_answers/has_answer_sheet` 由成员 part 推导，`is_exam` 表示试卷类套卷。

`Material`（文件）：`id,pack_id,position:integer,part:Part,answer_start_page:integer,title(1..255),file_name,original_name,stage:Stage,review_reasons:string[],suggested_pack?:{id,title},status:MaterialStatus,media_type,size_bytes,sha256(64 位小写十六进制，全局唯一),created_at,updated_at,pack?:Pack`。**一个 SHA-256 只对应一个 Material**：同一内容再次上传只追加 `MaterialSource`，不重复解析、计费或新建套卷。**Part 有且只有 8 类**：questions=试题，只有题目；answers=答案，只有答案或解析；full=题目+答案在同一文件（后部集中答案）；teacher=教师版，每题后紧跟答案；answer_sheet=空白答题卡；listening=听力音频或原文；main=单文档资料正文（讲义、课件、指南）；other=无法判断。详情额外含 `sources:MaterialSource[]`、`replicas:Replica[]` 和 `pack:Pack`。**MaterialStatus**：pending_storage=等待存储副本；archived=副本齐全；degraded=副本不全但仍可读。`review_reasons` 取值 low_confidence、pack_suggestion、duplicate_suspect、analysis_failed、manual。`suggested_pack` 为待整理的配套建议。

`MaterialSource`：`id,material_id,upload_id,user_id,path,duplicate:boolean,created_at`；记录每一次到达。`duplicate=true` 表示这次上传的内容此前已经归档。

`Collection`（专题）：`id,title(1..255),description(<=4000),pack_count:integer,created_at,updated_at`；详情额外含 `packs:Pack[]`，按专题内 position 排序。专题只收纳套卷，不含文件，也没有父子层级；同专题同套卷唯一，最多 5000 个套卷。

`Upload`：`id,title,hints:{subject_codes,grade,doc_type,cohort,region,organization,tags},status:pending|processing|completed|failed,job_id,new_count:integer,duplicate_count:integer,skipped:string[],created_at,entries:UploadEntry[],results:UploadResult[]`。`UploadEntry`：`id,relative_path,size_bytes,sha256:string(可空),status:pending|received|existing`。status=received 表示字节数、内容签名和 SHA 已验证；existing 表示声明 SHA 已归档，可秒传、无需上传字节。`UploadResult`：`material_id,pack_id,title,path,duplicate:boolean,stage,part`，finalize 后按条目给出归档结果；`duplicate=true` 表示内容已存在。`skipped` 记录未支持而未归档的路径。folder 使用 `webkitRelativePath`，不上传操作系统路径。

`Job`：`id,kind:ingest|replicate|analyze|index|group,status:pending|running|succeeded|failed,upload_id:string,replica_id:string,material_id:string,attempts:integer,error:string,progress_message:string,progress_current:integer,progress_total:integer,logs?:[{time,level:info|error,message,elapsed_ms?:integer,duration_ms?:integer,metrics?:object}],started_at:date-time|null,finished_at:date-time|null,duration_ms:integer,created_at,updated_at`。详情包含最多 200 条带时间的处理记录；列表省略 `logs`。`duration_ms` 是最近一次执行从领取到终态的耗时；运行中可按 `started_at` 计算当前耗时。日志的 `elapsed_ms` 是相对本次执行开始的偏移，`duration_ms` 表示当前记录覆盖步骤的耗时，`metrics` 只保存应用可控的非敏感事实（端点名、字节数、吞吐、页数、模型名、校验级别、分享状态等）。进度数量只表示当前阶段（文件数、批次数或传输字节数），`progress_total=0` 表示当前阶段没有可计算的总数，不代表整个任务百分比。旧任务可能没有记录或新指标。重试返回同一 job_id，已完成任务禁止重试；入队和启动恢复保证未完成工作不会仅存在内存中。PostgreSQL 支持带租约和心跳的多实例领取；SQLite 仅支持单实例。

`StorageProvider`：`id,name(1..128),kind:local|webdav|lanzou|123pan|onedrive|s3,role:primary|backup,enabled:boolean,priority:integer(0..1000),weight:integer(1..1000),config:object<string,string>,secret_configured:boolean,created_at,updated_at`。任何 GET 不返回 secret。`Replica`：`id,blob_id,provider_id,status:pending|uploading|ready|failed,size_bytes,verification:uploaded|size|sha256,error,share_status:pending|ready|unsupported|failed,share_url:string,share_password:string,share_expires_at:date-time|null,created_at,updated_at`。远端对象 ID 不开放给普通资料接口。

`StoragePolicy`：`mode:mirror_all|primary_backup|balanced,minimum_replicas:integer(1..20)`。默认 mirror_all/1。mirror_all 与 primary_backup 都写所有启用主备；balanced 在主端按权重选 minimum_replicas 个不同端点（主端不足时取全部主端），再加全部备端。读取按主端优先、priority 升序；失败切换。目标不足不能标 archived；一个 Blob/Provider 仅一个副本，复制重试不会重新选择已确定目标。

`APIKey`：`id,name,scopes:string[],expires_at:date-time|null,revoked:boolean,created_at`。`User`：`id,username,role:admin|editor|viewer,enabled:boolean,created_at`。`Audit`：`id,user_id,username,action,target_id,created_at`（username 按当前用户查询，系统操作或用户不存在时为空；仅存元数据，不存正文/凭据）。

## 认证与管理

### POST /auth/login（公开）

JSON `{"username":"admin","password":"至少12位的用户口令"}`。200 data=`{"access_token":"JWT","refresh_token":"opaque","expires_in":900,"user":User}`。错误凭据统一 401；登录有节流。默认没有固定管理员密码，启动时通过环境变量自举。

### POST /auth/refresh（公开）

JSON `{"refresh_token":"opaque"}`。200 同登录；旧 refresh token 立即失效，refresh 最长 7 天，服务端仅存哈希。**重用检测**：提交已经轮换过的旧 refresh token 返回 401，并吊销该会话（已签发和刚轮换的 access/refresh 全部失效），同时写审计 `auth.refresh_reuse`；客户端遇到 401 应重新登录，不要重试旧 token。

### POST /auth/logout（JWT）

JSON `{"refresh_token":"opaque"}`。204；注销关联会话，同时使其 access JWT 失效。无效 token 也返回 204，不泄露会话存在性。

旧 `/auth/agent-token` 接口已移除。已有 `pc_` 密钥不需重新创建，可直接作为 Bearer 凭据；升级前已签发的代理 JWT 在原有效期内仍按密钥状态校验。

### GET /auth/me（JWT）

200 data=User。API 密钥可直接作为 Bearer 凭据。

### GET /users · POST /users（users:manage）

GET 分页，200 data=Page<User>。POST JSON `{"username":"alice","password":"至少12位","role":"editor"}`，201 data=User；username 3..64，只允许字母数字下划线连字符，唯一。

### GET /api-keys · POST /api-keys · DELETE /api-keys/{id}（keys:manage）

GET 分页，200 Page<APIKey>。POST `{"name":"检索 Agent","scopes":["materials:read","collections:read","jobs:read"],"expires_at":null}`；name 1..128，至少一个有效 scope，过期时间须在未来，省略或 null 表示长期有效。201 data=`{"key":APIKey,"secret":"sk-<随机密钥>"}`，secret 仅此一次返回。DELETE=撤销，204；密钥和已经签发的旧代理 JWT 均立即失效。

### GET /audit-logs（audit:read）

分页，200 Page<Audit>。仅审计元数据。

## 套卷、文件与专题

### GET /subjects（materials:read）

200 data=`Subject[]{code,name}`，返回固定学科字典；顺序固定，不随资料变化。

### GET /packs（materials:read）

查询：page、page_size、`q`（套卷标题，<=200）、`subject`（Subject.code）、`grade`、`doc_type`、`term`、`cohort`（1900..2200）、`stage`（Stage）、`missing=answers`（只看缺答案的试卷）、`sort=updated|cohort|title`、`region`、`organization`、`tag`、`collection_id`、`material_id`、`content=true`（同时搜正文，走解析缓存）、`trashed=true`（只看回收站）。200 Page<Pack>，按 created_at DESC、id DESC。非法枚举或越界值 400。

### GET /packs/facets（materials:read）

200 data=`{"subjects":[{"value":"math","count":3}],"grades":[],"doc_types":[],"cohorts":[],"regions":[],"organizations":[],"tags":[]}`；每个分面是 `{value,count}` 数组。

### GET /packs/{id}（materials:read）

200 Pack（含 members、collections）。404 不存在。

### PATCH /packs/{id}（materials:write）

JSON 部分更新，可含 title/doc_type/subject_codes/grade/term/cohort/region/organization/exam_date/exam_name/chapter/tags/description，至少一个字段，枚举与长度按数据字典校验。`title` 非空会设置 `title_locked=true` 并同步全部成员文件名，空串解除锁定、恢复自动命名；任何人工 PATCH 都会置 `catalog_locked=true`。200 Pack。

### POST /packs/bulk（materials:write）

JSON `{"ids":["uuid"],"patch":{...}}`；1..500 个套卷，patch 至少一个字段且不能设置 title。事务批量应用，200 data=`{"count":n}`。部分套卷不存在时返回 404。

### POST /packs/{id}/merge（materials:write）

JSON `{"source_ids":["uuid"]}`；1..100 个来源套卷。来源的文件按顺序并入目标、专题收录关系跟随迁移，目标的空编目字段用来源补齐，来源清空后删除。200 合并后的 Pack。来源不存在 404。

### PUT /packs/{id}/order（materials:write）

JSON `{"material_ids":["uuid"]}`；必须恰好包含该套卷全部文件，否则 400。按数组顺序写入 position，200 Pack。

### GET /packs/{id}/download（materials:read）

200 `application/zip`，按 position 打包全部成员原件，重名追加序号；套卷没有文件返回 404。与原文件一样，不可用副本返回 503。

### POST /packs/trash · POST /packs/restore · POST /packs/purge（materials:write）

JSON `{"ids":["uuid"]}`；每次 1..500 个，返回 200 data=`{"count":n}`。trash 把套卷及其文件软删除进回收站；restore 恢复回收站中的套卷；purge 只接受已在回收站中的套卷（否则 409），彻底删除文件、副本行、题目与解析缓存，并删除无引用的本地 CAS 文件。回收站保留 30 天，到期由后台任务清理。**云盘适配器没有删除接口，远端对象不会被 purge 删除**，其副本行会被移除；需要删除远端文件时请自行处理。审计记录 pack.trash / pack.restore / pack.purge。

### GET /materials（materials:read）

查询：page、page_size、`q`（标题/原始文件名，<=200）、`stage`（Stage）、`part`（Part）、`status`（MaterialStatus）、`reason`（review_reasons 之一）、`pack_id`、`sha256`（64 位十六进制，精确查询相同内容）。200 Page<Material>；`stage=review` 即待整理收件箱，按 created_at 升序，其余按时间倒序。

### GET /materials/stats（materials:read）

200 data=`{"packs":3,"files":8,"processing":1,"review":2,"ready":5,"failed":0,"missing_answers":1,"trashed":0,"pending_storage":0,"archived":8,"degraded":0}`。`packs` 为未删除套卷数；`files` 为未删除文件数，按 stage 拆分 processing/review/ready/failed；`missing_answers` 为缺答案的试卷类套卷数；`trashed` 为回收站套卷数；其余按副本状态统计。未出现的状态为 0。

### GET /materials/{id}（materials:read）

200 Material 详情，含 `pack`、`sources`、`replicas`。分享 URL/提取码只在此有权限响应中返回，不记录日志。404 不存在。

### PATCH /materials/{id}（materials:write）

JSON 可含 `part`（Part）、`answer_start_page`（0..10000）、`pack_id`（移入已存在套卷）或 `new_pack=true`（拆成独立套卷），至少一个字段。不允许修改 title、SHA、存储状态或副本。200 Material 详情；`part` 改为非 questions/full 时会移除旧题目索引，套卷标题与成员文件名按新 part 重新生成。

### POST /materials/accept（materials:write）

JSON `{"ids":["uuid"],"accept_suggestion":true}`；1..500 个待整理文件。`accept_suggestion=true` 且存在 `suggested_pack` 时先把文件并入建议套卷，然后统一置为 `ready`。200 data=`{"count":n}`。审计记录 material.accept。

### GET /materials/{id}/download（materials:read）

200 二进制，Content-Type 取已验证媒体类型；Content-Disposition attachment，UTF-8 文件名；X-Content-Type-Options: nosniff。同源 JWT 通过 fetch Blob 下载，不将 Token 放入 URL。先检查本地原件，再按主备优先级尝试可读云副本；没有可用副本 503。Range 请求本期不承诺。

### POST /materials/{id}/pull-cache（materials:read）

从状态为 ready 的云端副本中按主端优先、priority 升序拉取文件，流式写入 `DATA_DIR/tmp/material-pulls`，同时校验资料的声明字节数和 SHA-256。只有完整匹配才原子发布为临时文件；不会写入永久 CAS，也不会修改副本状态或资料状态。没有可用副本、所有副本读取失败或内容校验失败返回 503，失败内容不会保留为可下载文件。

200 data=`{"download_url":"/api/v1/materials/<id>/pull-cache","file_name":"试卷.pdf","media_type":"application/pdf","size_bytes":12345,"sha256":"<64位小写十六进制>","expires_at":"2026-10-30T12:00:00Z","expires_in":900}`。`download_url` 是服务端相对路径，必须继续携带相同 Bearer 凭据；不得把 Token 写进 URL。未过期且尺寸正确的现有临时文件会直接复用，重复 POST 不会延长 TTL。`expires_in` 为剩余秒数。

TTL 由服务端环境变量 `PULL_CACHE_TTL` 控制，默认 `15m`。修改配置需重启对应进程；重启不会清除尚未到期的文件。过期判断基于临时文件的 mtime。多实例共享 `DATA_DIR` 时可复用其他实例落盘的同一 SHA 临时文件。

### GET /materials/{id}/pull-cache（materials:read）

200 返回先前通过 POST 拉取且尚未过期的原文件，Content-Type、Content-Disposition 和 `X-Content-Type-Options: nosniff` 与普通下载一致，并设置 `Cache-Control: private, no-store, max-age=0`、`X-Pull-Cache-Expires-At` 和 `X-Pull-Cache-Expires-In`。此接口只读取临时缓存，不会在 GET 时触发云端拉取。临时文件不存在、尺寸不匹配或已过期时删除该文件并返回 410 `gone`；资料不存在仍返回 404。Range 请求本期不承诺。

### GET /materials/{id}/preview（materials:read）

PDF 直接返回原件；DOCX 由 LibreOffice Writer 转换为 PDF，返回 `application/pdf`、`Content-Disposition: inline` 和 `Cache-Control: private, no-store`。浏览器使用授权 fetch 加载，MCP 的 `materials_preview` 返回需要同一 Bearer 认证的路径。首次转换可能需要等待；相同内容的转换结果复用。其他类型返回 415，LibreOffice 不可用返回 503，损坏、加密或包含外部引用资源的文档不能转换。普通超链接不受此限制。预览排版可能因字体或渲染器与 Word 不同；下载仍为原始 DOCX 字节。

### GET /collections · POST /collections（分别 collections:read / collections:write）

GET page、page_size、`q`（标题/描述，<=200）、`pack_id=<套卷ID>`（包含该套卷的专题），200 Page<Collection>。POST JSON `{"title":"导数压轴题","description":"","pack_ids":["uuid"]}`；201 Collection 详情。`pack_ids` 可省略，最多 5000 个，重复或不存在返回 400。

### DELETE /materials/{id}（materials:write）

204。文件先从所在套卷拆出为独立套卷（原套卷其余文件不受影响），再整卷移入回收站，30 天内可恢复；不再提供 `cascade` 参数，也没有级联删除子资料的行为。审计记录 `pack.trash`。

### DELETE /collections/{id}（collections:write）

204，删除专题及其收录关系，套卷和文件本身保留。审计记录 `collection.delete`。

### GET /collections/{id} · PATCH /collections/{id}

GET 200 Collection 详情（含 `packs`）；PATCH 只接收 title/description，至少一个字段，200 Collection。对应 read/write scopes。

### PUT /collections/{id}/items（collections:write）

JSON `{"pack_ids":["uuid"]}`；0..5000 个，禁止重复，所有套卷必须存在。事务整体替换收录关系，200 Collection 详情（含 `packs`）。空数组清空收录，不删除套卷或文件。

### POST /collections/{id}/items（collections:write）

JSON `{"pack_ids":["uuid"]}`；追加到专题末尾，已收录的套卷跳过，最多 5000 个。200 Collection 详情。审计记录 collection.add_packs。

## 清单上传与归档

### POST /uploads（uploads:write）

JSON：

```json
{"title":"2026 高三数学月考","hints":{"subject_codes":["math"],"grade":"grade12","doc_type":"monthly","cohort":2026,"region":"浙江","organization":"","tags":["一轮复习"]},"entries":[{"relative_path":"月考/数学-试卷.pdf","size_bytes":12345,"sha256":""},{"relative_path":"月考/数学-答案.pdf","size_bytes":23456,"sha256":""}]}
```

201 data=Upload（包含服务器分配的 entry.id）。`title` 可省略（用首个文件名生成，<=255），`entries` 必需 1..2000；`hints` 全部可省略，学科/年级/类型/届别/地区/机构/标签按字典校验，作为套卷初始编目和 AI 识别的先验。每条 relative_path 必须唯一，1..1024 UTF-8 字节，正斜杠分隔，拒绝绝对路径、反斜杠、..、空路径段；重复路径返回 **409**（`清单中存在重复路径`）。size_bytes 1..536870912；清单总和 <=2147483648；请求 JSON 总大小不超过 1 MiB。支持 `.pdf .doc .docx .ppt .pptx .xls .xlsx .jpg .jpeg .png .webp .mp3 .m4a .wav .zip`（不区分大小写），其他扩展名 415。可选的 sha256 必须为 64 位十六进制；声明 SHA 已归档时该条目直接标记 `existing`（秒传），无需上传字节。entries 按自然文件名顺序分配 position 后返回。新上传对用户隔离，非创建者仅 admin 可访问。此处只创建上传记录，不创建资料或套卷。

### GET /uploads/{id}（uploads:write）

200 Upload，用 entries.status 恢复中断上传；已收到的文件无需重发。

### PUT /uploads/{id}/files/{entry_id}（uploads:write）

`Content-Type: application/octet-stream`，请求体为单文件原始字节，必须与声明字节数一致，允许 Content-Length 或 chunked。200 data=UploadEntry。验证真实格式和 SHA；重复上传相同已接收文件返回原结果；received 项不得改成不同内容。错误 409（会话已 finalize）、413（超声明/上限）、415（签名不符）、422（字节数/SHA 不符）。流写临时文件，校验通过后原子替换，失败不记 received。

### POST /uploads/{id}/finalize（uploads:write）

无需请求体。所有文件 received 才可完成，否则 409。202 data=`{"job_id":"uuid","upload_id":"uuid"}`；对同一上传重复 finalize 返回同一任务，禁止重复创建资料。

归档任务：服务端按目录 + 文件名主干把清单分组，每组生成一个套卷；组内按自然顺序排序，并按文件名与内容推断 part（试题/答案/教师版/答题卡/听力/正文）。同一 SHA-256 已存在时只追加 `MaterialSource`（`duplicate=true`），不新建文件、不重复下载或分析，但仍记录这次到达路径；`new_count`/`duplicate_count`/`skipped` 汇总本次结果。ZIP 原文件不创建 Material、Blob 记录或存储副本，成员按展开后的路径归档；服务端在临时区保存已接收 ZIP 以支持中断恢复和任务重试，成功提交归档后删除临时 ZIP；失败时保留供重试。升级前已在 CAS 接收的 ZIP 仍可处理，其旧文件及历史 ZIP 资料不会自动删除。

服务端拒绝路径穿越、符号链接、重复路径、加密 ZIP 与解压炸弹；提取前检查整个目录，成员真实格式与扩展名不符时不发布资料。每个 ZIP 展开上限 2 GiB、5000 个非忽略文件、单成员压缩比 200。支持 PDF/DOC/DOCX/PPTX 和嵌套 ZIP 成员，嵌套 ZIP 不递归展开；其他文件跳过并写入任务日志，管理台在完成页提示，只有不支持文件的 ZIP 会使任务失败。成员按自然文件名顺序 position；角色仅文件名规则推测，默认为 other。

## 任务与存储

### GET /jobs · GET /jobs/{id}（jobs:read）

列表查询 page/page_size/status/kind/material_id/upload_id/replica_id，200 Page<Job>；详情 200 Job。关联 ID 均为精确匹配，用于从资料、上传清单或副本直接下钻任务。

### GET /jobs/stats（jobs:read）

200 data=`{"total":12,"pending":1,"running":1,"succeeded":9,"failed":1}`；一次统计全部任务的状态，未出现的状态为 0。`total` 包括所有状态。

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
| onedrive | auth_mode 为 direct（服务端默认，兼容已有配置）或 openlist；direct 时 client_id 必需、tenant 默认 common；path_prefix 可选；share_scope 为 anonymous（默认）、organization 或 disabled | refresh_token 必需；direct 时 client_secret 可选；share_url 可选，共享文件夹链接作为秘密保存 |
| s3 | bucket、region 必需；endpoint 可选；path_prefix 可选；path_style 默认为 false；share_expires_seconds 默认 3600，范围 60..604800 | access_key_id、secret_access_key 必需；session_token 可选 |

省略 role/enabled/priority/weight 时默认 primary/true/100/1；config 或 secret 中出现该 kind 不支持的键会被拒绝。创建 local 限定到服务端数据目录。API 不返回传入 secret，`secret_configured` 只表示已保存，不代表已连通。

适配器能力边界（实现事实，非联调声明）：

- local：上传、下载可用；不支持分享（share_status=unsupported）。未提供 SHA 时 verification 只能是 size。
- webdav：上传、下载可用；未配置 public_base_url 时分享不支持，配置后返回公开目录拼接链接，不代表该 URL 可匿名访问。提供 SHA 时上传后会完整下载重算。
- lanzou：使用非官方接口，按 cookie 会话工作；单文件上限 100 MiB，超限返回 unsupported；默认 verification=uploaded，开启 verify_remote_hash 才可能为 sha256；分享返回 URL 与提取码。未对真实服务联调。
- 123pan：当前只实现 access_token 与 Health 探测；upload/download/share 返回 unsupported（错误码 123pan_upload_unverified 等），Capabilities 全为 false。**不应把 123pan 作为可上传端点启用**；复制任务会失败并记录该错误，而不是伪造成功。
- onedrive：国际版 Microsoft Graph 支持上传、下载、生成只读分享链接；250 MiB 以内的文件直接上传，更大的文件使用分段上传，上传会话遇到同名冲突时会清理中断留下的 0 字节占位文件后重试。暂不支持中国世纪互联版和政府云。`auth_mode=direct` 使用自己的应用，通过微软端点续期；`auth_mode=openlist` 使用 OpenList 提供的应用，从 OpenList Token 页面取得刷新令牌，续期时通过固定的 `https://api.oplist.org/onedrive/renewapi` 发送令牌。OpenList 的接口将令牌置于 URL 查询参数，提供方或中间环节可能记录；本服务不记录该 URL、口令或令牌，禁止续期重定向。OpenList 授权请求 `Files.ReadWrite.All` 和 `offline_access`，自有应用可使用 `Files.ReadWrite` 和 `offline_access`。更换 `auth_mode` 需要同时提交新模式对应的 `secret.refresh_token`，`secret` 仍整体替换，其他必要凭据须一并提交。历史缺少 `auth_mode` 的端点继续使用 direct。刷新令牌轮换后加密保存；`share_url` 可指定有写权限的共享文件夹。匿名分享可能被租户禁用，organization 仅适用于组织账号；分享失败不影响副本。Health 只检查文件夹可访问，不保证写入权限。验证级别为 size。未对真实 OneDrive 与 OpenList 联调。
- s3：使用 AWS SDK 支持 AWS S3 和兼容端点；大文件自动分段并行上传、失败时尝试中止分段，上传后 HEAD 检查大小；分享为签名下载链接，过期后需刷新，临时凭据可能使链接提前失效。Health 使用 HeadBucket，要求相应的桶访问权限。未对真实 S3 联调。

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

AI scope：`ai:read`（读取模型/OCR 配置与解析设置）、`ai:write`（修改配置、探测、测试）。两者默认只授予 admin，可单独授权给 API 密钥。资料分析读取沿用 materials:read，触发与应用沿用 materials:write。密钥只写不读：响应只返回 `api_key_configured`、`token_configured`、`mineru_configured` 和 `ocr_configured`；服务端用 MASTER_KEY 加密，AAD 为 provider id（旧 `ai_settings` 字段 使用固定串 `ai:settings`，迁移时重新绑定到新 provider id）。

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

选择自建 RapidOCR 时，DOCX 由 Go 直接解析 OOXML：正文、标题、表格、文本框、公式和分页提示进入解析缓存，嵌入的位图再逐张提交同一 OCR 接口；识别和索引共享解析缓存。常见 OMML 分式、上下标、根号、求和/积分、括号、上下限、横线和矩阵保留为 LaTeX，其他结构尽量保留文本。DOCX 不经过 LibreOffice 或 PDF 文本层，分析/搜题无需 `soffice`，也无需同步升级 RapidOCR 镜像。页码来自显式分页或文档保存的分页提示，连续文档可能只有第 1 页；复杂公式、图片 OCR 和复杂排版不能保证与原文完全一致。

- `GET /ai/settings`（ai:read）：200 AISettings。
- `PUT /ai/settings`（ai:write）：请求完整设置，`ocr_provider_id` 可选：省略=保留，空串=清除选择；`mineru_token` 为旧 MinerU 兼容字段：省略=保留，空串=清除；200 AISettings。旧版仅提交 MinerU 字段的客户端会继续工作，并自动物化/更新 MinerU OCRProvider。
- `POST /ai/mineru/test`（ai:write）：兼容旧客户端，实际测试当前选中的 OCR 服务商；200 `{"healthy":boolean,"message":"脱敏说明"}`。新客户端应使用 `POST /ocr/providers/{id}/test`。

升级时若 `ocr_providers` 为空且旧 `ai_settings.mineru_token_cipher` 有值，启动/读取设置会幂等迁移为一条 MinerU 服务商并设为当前服务商。PaddleOCR 使用 `https://paddleocr.aistudio-app.com` 的 `POST /api/v2/ocr/jobs`、`GET /api/v2/ocr/jobs/{jobId}` 与 JSONL `jsonUrl`；`PP-OCRv6` 沿用官网 v5 兼容异步协议。自建 RapidOCR 使用 `POST /api/v1/ocr/jobs`、`GET /api/v1/ocr/jobs/{id}` 和 `result_url` JSONL；服务地址在 Compose 内为 `http://rapidocr:9003`，不对外暴露。

自建服务当前固定使用 RapidOCR 3.10.0 的默认 PP-OCRv6 small；请求中的 `model` 字段仅保留协议兼容，并不会切换模型。现有 JSONL 每页使用 `page_no` 和 `texts` 字符串数组。接收端也支持按阅读顺序排列的 `blocks`：`{"type":"text","text":"..."}`、`{"type":"inline_formula","latex":"..."}` 和 `{"type":"display_formula","latex":"..."}`；`latex` 为不含外围分隔符的原始 LaTeX，JSON 中的反斜杠须转义。有非空 `blocks` 时优先使用它并忽略 `texts`，未知块类型或空公式会失败而不静默丢失。该协议为后续公式模型提供入口；当前 RapidOCR 服务仍只输出 `texts`，没有接入扫描件公式识别模型。

`MaterialAnalysis`：`id,material_id,status:pending|running|succeeded|failed,provider_id,provider_name,model,page_count,image_count(恒为 0，页面图不落盘),confidence:number(0..1),suggestions:{exam_name?,doc_type?,subject_codes?,grade?,term?,cohort?,region?,organization?,exam_date?,chapter?,tags?,part?,answer_start_page?,uncertain?},markdown_excerpt:string(<=4000),error:string,applied_at:date-time|null,created_at,updated_at`。`applied_at` 表示最近一次自动或手动应用建议的时间；`uncertain` 列出模型不确定的字段名。

- `POST /materials/{id}/analysis`（materials:write）：请求可空或 `{"force":false}`；pending/running 返回 202 现有分析；succeeded 且 force=false 返回现有；failed 或 force=true 重新入队。202 data=`{"job_id":"uuid","analysis":MaterialAnalysis}`。
- `GET /materials/{id}/analysis`（materials:read）：200 MaterialAnalysis；尚无分析时返回 404 `not_found`。
- `POST /materials/{id}/analysis/apply`（materials:write）：重新应用已成功完成的建议。请求可覆盖白名单字段 `{"title":"","doc_type":"","subject_codes":[],"grade":"","term":"","cohort":0,"region":"","organization":"","exam_name":"","exam_date":"","chapter":"","tags":[],"part":"","answer_start_page":0}`，省略字段取建议值；显式提供的白名单字段也可覆盖或补充建议。应用前与套卷 PATCH、文件 PATCH 使用同一套校验；200 更新后的 **Material 详情**，不能修改 blob/状态/路径。写审计 `material.ai_apply`。

分析链路：归档完成后若 `analysis_enabled` 自动入队（ZIP 原件不入队，展开成员各自入队）；任务读取原件 → 当前选中的 OCRProvider 解析（RapidOCR 自建异步任务、PaddleOCR 异步任务或 MinerU v4） → 统一 Markdown/逐页文本 → 有 vision_model 时以 Markdown + 前 max_images 张图片调用视觉模型，否则纯文本调用 default_model → 严格 JSON 校验建议 → **在同一事务中自动应用白名单字段、同步文件名、分析已应用标记，并在条件满足时创建 `group` 任务**。归类以正文（或页面图像）为依据，文件名和来源路径只作辅助；提示明确禁止仅凭文件名判断，正文与文件名冲突时以正文为准。长文档最多发送 12000 字符，保留开头约 2/3 与末尾约 1/3，并标注中间省略，以免漏掉卷末答案区；未解析出任何正文或页面图像时任务失败，不退回文件名猜测。自动应用只接受 `parseSuggestions` 校验后的字段，标题会同步为下载名和本地副本名；模型原文、未知字段和非法值不会写入。自动应用写审计 `material.ai_auto_apply`，人工重应用写 `material.ai_apply`，两者都会在满足条件时尝试入队自动归类。模型 provider 按 priority 故障切换；失败错误必须脱敏。未配置 OCR 服务商或外部服务令牌时任务失败并给出明确原因，不伪造结果。RapidOCR/PaddleOCR 不保存页面图，因此视觉模型只会收到文本。

题目索引链路：解析文本按 `batch_pages`（默认 10）与 `batch_chars`（默认 12000）分组，最多 3 个批次并发切题；模型输出上限 128000 token，并请求 `reasoning_effort=high`。单批发生空响应或非法 JSON 时按页、再按正文二分重试；上游 5xx/限流最多额外重试一次，超时、鉴权等错误直接失败，不通过递归重试放大调用量。批次结果按原顺序合并去重后在同一事务写入题目与检索词。

自动配套链路：`group` 任务只处理“套卷恰好一个文件、该文件是试卷类 part（questions/full/teacher/answers/answer_sheet，可配套）且套卷 doc_type 属于试卷类”的文件；候选为其他同 doc_type 的试卷类套卷。评分结合学科、年级、届别、地区、机构、套卷标题与成员标题；最佳分 >=70 且领先第二名 >=15 时自动把整个套卷并入候选套卷（迁移文件与专题收录、补齐空编目字段），写审计 `pack.auto_pair`；最佳分 >=40 时只把文件置为 `review` 并记录 `pack_suggestion` 与 `suggested_pack`，等待人工确认（`POST /materials/accept`）。分数不足或出现学科/年级/届别冲突时不改动，任务仍以 succeeded 结束并记录原因。人工修改 part 或重新分析成功后可能再次入队。审计记录 `pack.auto_pair`。

Job 的 `material_id` 关联 analyze/index/group 等任务资料；`kind` 包含 `analyze`、`index`、`group`。

## 搜题（纯文本）

`search:read`（读取搜索设置）、`search:write`（修改搜索设置）默认仅管理员，可单独授权给 API 密钥。搜索接口本身使用 materials:read。

索引由 `index` 任务完成：默认启用；只有 part 为 `questions`（试题）或 `full`（题目+答案同文件）的文件会在归档、AI 识别应用 part 或启动补建时入队，其他 part 不会建立题目索引，part 改为非试卷类时会移除旧索引。任务复用命中“文件 SHA + OCR 类型/地址/模型 + 缓存版本”的 `material_texts`，未命中时调用当前 OCRProvider → LLM 默认每批 10 页/12000 字（可配置）完成 OCR 纠错与切题 → 保存题目和页码 → 建立 CJK unigram+bigram 与连续字母/数字词倒排。不做跨资料同题聚合；同一题出现在多份试卷就是多条结果。索引不保存页面图/bbox；需要视觉图片时 MinerU 会重新解析。

题干保留原始公式文本/LaTeX，模型被要求保留运算符和结构；输入中已有的 LaTeX 命令若在模型 JSON 中少转义反斜杠，会在解码前修复，无法安全解码的控制字符使任务失败。搜索额外建立带运算符的公式二元/三元词项，并按公式原文的规范化形式再次校验：例如 `x+y` 与 `x-y` 可区分，`x²` 与 `x^{2}`、简单的 `\frac{x}{y}` 与 `x/y` 可匹配，全角和常见运算符写法可归一化。数学变量保留大小写；不支持通用代数等价、公式结构推理或任意 LaTeX。普通文本仍使用原有词项。公式命中高亮覆盖源文本中的公式片段，`text`/`snippet` 仍为普通文本，不返回可执行 HTML。

升级后后台分批为已有题干补建公式词项，重复执行安全，不重新调用 OCR 或切题模型；公式搜索会先补完尚未处理的旧题，首次搜索可能等待。旧题干本身已经丢失的公式无法凭索引恢复；如需从原生 DOCX 恢复常见公式结构，请对资料重建题目索引，重新解析和切题会产生正常的模型调用。

- `POST /search/questions`（materials:read）：请求 `{"query":"已知函数...","subject":"math","grade":"grade12","academic_year":0,"limit":20,"offset":0}`。`query` 必填 1..2000 字；`subject`/`grade`/`academic_year` 按字典校验；`limit` 默认 20、最大 50；`offset` >= 0。只返回资料状态为 archived/degraded 且已有题目的记录。200 `{"items":[{"question_id":"...","question_no":"12","text":"完整题干（含选项/小问）","snippet":"...","highlights":[[0,6]],"score":12.3,"page_no":3,"material":{"id":"...","title":"...","file_name":"..."},"pack":{"id":"...","title":"..."},"subject_codes":["math"],"also_in":[{"id":"...","title":"..."}]}],"total":1,"took_ms":23}`。`pack` 是文件所属套卷；`also_in` 是同一题干在其他套卷中的重复出现（按套卷去重，可空数组）。`snippet` 围绕最长连续文字匹配或命中公式截取，省略处有 `…`；`highlights` 是相对于 `snippet` 的 Unicode 字符区间数组，无匹配时为空数组，客户端自行渲染，禁止返回 HTML。
- `GET /search/questions/{id}`（materials:read）：单题详情（同上字段，score 为 0）；没有查询词，`snippet` 为题干首段，`highlights` 为空数组。
- `GET /materials/{id}/questions`（materials:read）：分页 `{id,material_id,page_no,seq,question_no,text,confidence,created_at}`，按 `seq` 升序。
- `GET /materials/{id}/index`（materials:read）：`{status:pending|running|succeeded|failed|not_applicable,job_id,page_count,question_count,llm_calls,llm_failed_batches,error,indexed_at}`；试卷类资料尚无索引记录时返回 `status=pending`、`job_id=""`；其他角色返回 `status=not_applicable`、`job_id=""`。
- `POST /materials/{id}/index`（materials:write）：`{"force":true}` 重新索引；`force=true` 会清空解析缓存（`material_texts.source_hash`）以强制重新解析；202 `{"job_id":"...","state":{...}}`。只有 part 为 `questions` 或 `full` 的文件可索引；其他 part 拒绝索引。
- `GET /search/settings`（search:read）：`{enabled,max_pages,batch_pages,batch_chars,provider_id,model}`。
- `PUT /search/settings`（search:write）：完整替换；默认 enabled=true、max_pages=500、batch_pages=10、batch_chars=12000；max_pages 1..5000、batch_pages 1..50、batch_chars 1000..100000。未指定 provider/model 时使用第一个启用端点及其默认模型；指定的 provider 必须存在且启用，否则任务失败。搜索设置可通过 HTTP 或 MCP 访问，目前没有独立管理台页面。

索引失败不阻塞归档与下载；搜索只返回已有题目且资料状态为 archived/degraded 的记录。搜索不检查 `material_index_states.status`，因此重建失败时旧索引仍可搜索，状态显示失败。

## MCP

`POST /mcp`：使用 API 密钥作为 Bearer 凭据；`Content-Type: application/json`。服务端不强制校验 `Accept`；成功响应为 `application/json`，不提供 SSE。JSON-RPC 2.0 单条消息，batch 不支持。`initialize` 请求 protocolVersion 支持 2025-03-26、2025-06-18、2025-11-25，响应回协商版本、serverInfo、instructions 和 capabilities.tools；无 ID 通知返回 202，无响应体；`ping` result={}；tools/list 根据当前 scope 过滤并标记操作属性；tools/call 使用与 REST 相同的权限和业务校验。上传小文件支持 Base64，较大文件与下载通过返回的路径流式传输。GET /mcp=405，不发 session id，属于无状态 Streamable HTTP 的 JSON 子集。详细客户端配置和请求示例见 [MCP.md](MCP.md)。

工具及权限的完整列表见 [MCP.md](MCP.md)；`tools/list` 返回准确的参数 schema。套卷、文件、专题、题目、任务、上传、存储、AI/OCR、用户和密钥等操作与对应 REST 处理器使用相同字段与权限。`questions_search` 返回与 REST 一致的高亮、学科和套卷字段，默认 limit 为 20、最大 50。schema 拒绝未知字段；缺少/未知/类型错误参数与校验错误返回 -32602。JSON-RPC 错误 -32700 表示解析失败、-32600 请求非法、-32601 方法未知、-32602 参数非法；领域失败返回 `content=[{type:"text",text:"错误说明"}]` 和 `isError=true`。成功 content 是 JSON 序列化文本。不存在通用 URL 或服务器文件读取工具；AI 和存储配置可以按对应权限设置服务端点，工具不能绕过 HTTP 授权。

## 示例 Agent 调用

```sh
curl http://127.0.0.1:8080/api/v1/materials -H "Authorization: Bearer $PAPER_COLLECT_API_KEY"
curl -X POST http://127.0.0.1:8080/mcp -H "Authorization: Bearer $PAPER_COLLECT_API_KEY" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"materials_search","arguments":{"subject":"math"}}}'
```

## 版本与扩展约定

不支持物理删除云盘文件、在线文档编辑、宏执行和跨请求断点分块合并。新增 Adapter 只实现 storage.Adapter；新增资料元数据须同时更新本文和前端 DTO；分层、任务和数据流变更同步 [ARCHITECTURE.md](ARCHITECTURE.md)。SQLite 为单实例 WAL；PostgreSQL 多实例必须配置唯一 `WORKER_INSTANCE_ID` 并共享 `DATA_DIR`。
