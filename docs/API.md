# HTTP API v1 接口合同

版本 1.8.0 · 2026-11-10。基础路径 `/api/v1`；MCP 路径 `/mcp`。以下为实现合同，不是云盘真实联调完成声明。

响应类型与枚举的唯一来源是后端 DTO：`backend/internal/server/contract.go` 生成 `web/src/shared/api/contract.gen.ts`（`make contract`），测试会拒绝过期的生成文件。本文件中的字段说明与生成类型冲突时以生成类型为准。1.8.0 变更：`GET /materials/review-packs` 改为 `GET /inbox`（MCP `inbox_list`）；OCR 服务商接口移到 `/ai/ocr-providers`；AI/OCR 测试统一返回 `ProviderTestResult`。

## 通用规范

- JSON 使用 UTF-8、snake_case；时间 UTC RFC3339；ID 为不透明字符串（UUID），SHA-256 是 64 位小写十六进制。int64 字节数受 2 GiB 批次上限保护，可由 JS number 精确表达。
- 成功：`{"data":T,"request_id":"uuid"}`；列表 T 为 `{"items":[],"total":0,"page":1,"page_size":20}`。错误：`{"error":{"code":"validation_error","message":"说明"},"request_id":"uuid"}`。不得把秘密或堆栈写进 message。
- 默认分页 `page=1&page_size=20`，page >= 1，page_size 1..100，稳定排序 created_at DESC、id DESC。未声明的分页/过滤参数不作为业务条件。
- 写接口拒绝非法 JSON、未知字段、不支持的枚举、越界值。PATCH 是部分更新：省略=不改，空数组=清空，禁止 null（允许 null 的字段另写）。请求 JSON 默认 1 MiB。
- HTTP：200 读取/更新；201 创建；202 任务已入队；204 删除/通知；400 格式错误；401 凭据无效；403 权限不足；404 不存在；409 状态冲突/重复路径；410 临时资源已过期；413 超尺寸；415 不支持类型；422 文件内容与声明不一致；429 登录频率限制；503 暂不可用。
- GET /healthz 无鉴权返回 `{"status":"ok"}`。所有业务接口强制鉴权，仅 auth/login、auth/refresh 例外。
- 浏览器 REST 使用 `Authorization: Bearer <access JWT>`；API 密钥直接使用 `Authorization: Bearer <API 密钥>` 访问 REST 和 MCP。API 密钥以 `sk-` 开头。MCP 接受 API 密钥，不接受浏览器会话。
- 角色：admin（全部）、editor（套卷与文件写、专题写、上传、任务读）、viewer（套卷、专题、任务读）。scope 包括 materials、collections、uploads、jobs、storage、keys、users、audit、ai 和 search 对应的读写或管理权限。API 密钥 scope 必须为用户权限子集；有相应授权时可访问管理、AI 和搜索设置接口。API 密钥创建其他密钥时只能授予自身持有的权限，创建用户时该用户角色的权限也不得超出当前密钥。新建密钥是独立凭据，不随创建者的密钥撤销而撤销。每次请求重新检查用户和密钥是否有效。

## 数据字典

`Subject.code`：chinese 语文、math 数学、english 英语、physics 物理、chemistry 化学、biology 生物、politics 政治、history 历史、geography 地理、information_technology 信息技术、general_technology 通用技术、comprehensive 综合。`GET /subjects` 返回 `Subject[]{code,name}`。

`Pack`（套卷）：`id,title(1..255),locked_fields:string[],doc_type:DocType,subject_codes:string[](0..12),grade:""|grade7|grade8|grade9|grade10|grade11|grade12,term:""|first|second,cohort:integer(0 或 1900..2200),region(固定地区列表之一或空),organization(<=128，名称词典中的规范名称),organization_id,exam_date(YYYY-MM 或空),exam_name(<=255),chapter(<=255),tags:string[](<=30，每项1..64),description(<=4000),stage:Stage,is_exam:boolean,has_questions:boolean,has_answers:boolean,has_answer_sheet:boolean,members:PackMember[],collections?:Collection[],deleted_at:date-time|null,created_at,updated_at`。`PackMember`：`id,title,file_name,part:Part,answer_start_page:integer,stage:Stage,status:MaterialStatus,sync_status:SyncStatus,media_type,size_bytes,position:integer`。`DocType`：gaokao_real、gaokao_mock、zhongkao_real、zhongkao_mock、joint、monthly、midterm、final、opening、unit、weekly、topic、lecture、study_guide、courseware、homework、other。**Stage**：processing=AI 整理中（阅读、分组或编目）；waiting_ai=没有可用的模型或 OCR，配置可用后自动继续；attention=需关注（AI 未能得到通过校验的结果）；ready=已入库。套卷 stage 取成员中最靠后的阶段（ready < processing < waiting_ai < attention）。`locked_fields` 列出人工设定的字段（title、doc_type、subject_codes、grade、term、cohort、region、organization、exam_date、exam_name、chapter、tags），AI 编目不会改动这些字段。地区固定列表：全国甲卷、全国乙卷、新高考I卷、新高考II卷、全国卷与 34 个省级行政区（简称，如“浙江”“内蒙古”）。`has_questions/has_answers/has_answer_sheet` 由成员 part 推导，`is_exam` 表示试卷类套卷。

`Material`（文件）：`id,pack_id,position:integer,part:Part,answer_start_page:integer,title(1..255),file_name,original_name,stage:Stage,ai_step:""|read|group|decide,attention_reasons:string[],part_locked:boolean,status:MaterialStatus,sync_status:SyncStatus,sync_planned_at:date-time|null,media_type,size_bytes,sha256(64 位小写十六进制，全局唯一),created_at,updated_at,pack?:Pack`。**一个 SHA-256 只对应一个 Material**：同一内容再次上传只追加 `MaterialSource`，不重复解析、计费或新建套卷。**Part 有且只有 8 类**：questions=试题，只有题目；answers=答案，只有答案或解析；full=题目+答案在同一文件（后部集中答案）；teacher=教师版，每题后紧跟答案；answer_sheet=空白答题卡；listening=听力音频或原文；main=单文档资料正文（讲义、课件、指南）；other=无法判断。详情额外含 `sources:MaterialSource[]`、`replicas:Replica[]` 和 `pack:Pack`。**MaterialStatus** 只回答原件是否安全：archived=原件在本机 CAS；missing=本机原件缺失，只能从网盘副本读取。**SyncStatus** 单独回答网盘同步进度：waiting_catalog=等待编目确定（规则依赖编目，尚未生成计划）；not_required=命中的规则不上传任何网盘；syncing=有副本在等待、上传或移动；synced=全部必需目标已就绪；attention=必需目标失败、必需目标的分享失败，或副本路径已偏离（网盘设置为保持原位）。`sync_planned_at` 为最近一次生成同步计划的时间。`ai_step` 是处理中文件所在的 AI 步骤。`attention_reasons` 为需关注原因：ai_failed（模型多次失败）、group_unverified（分组没有可靠结果，暂时单独成套）、metadata_only（没有读到正文，只依据文件名判断），以及未通过的关键校验代码（可带 `:字段` 后缀，如 duplicate_role:files、subjects_missing:subjects、grade_missing:grade、doc_type_missing:doc_type、answer_page:files、missing_file:files）。`part_locked=true` 表示部件由人工设定，AI 编目不会改动。

`InboxPack`：`pack:Pack,items:InboxItem[]`；`InboxItem` 为 `id,stage,attention_reasons[]`，只包含当前 stage/reason 命中的文件，`pack.members` 仍包含整套文件。尚未归入套卷的文件（仍在阅读或分组）不出现在此列表。

`MaterialSource`：`id,material_id,upload_id,user_id,path,duplicate:boolean,created_at`；记录每一次到达。`duplicate=true` 表示这次上传的内容此前已经归档。

`Collection`（专题）：`id,title(1..255),description(<=4000),pack_count:integer,created_at,updated_at`；详情额外含 `packs:Pack[]`，按专题内 position 排序。专题只收纳套卷，不含文件，也没有父子层级；同专题同套卷唯一，最多 5000 个套卷。

`Upload`：`id,title,hints:{subject_codes,grade,doc_type,cohort,region,organization,tags},status:pending|processing|completed|failed,job_id,new_count:integer,duplicate_count:integer,skipped:string[],created_at,entries:UploadEntry[],results:UploadResult[]`。`UploadEntry`：`id,relative_path,size_bytes,sha256:string(可空),status:pending|received|existing`。status=received 表示字节数、内容签名和 SHA 已验证；existing 表示声明 SHA 已归档，可秒传、无需上传字节。`UploadResult`：`material_id,pack_id,title,path,duplicate:boolean,stage,part`，finalize 后按条目给出归档结果；`duplicate=true` 表示内容已存在。`skipped` 记录未支持而未归档的路径。folder 使用 `webkitRelativePath`，不上传操作系统路径。

`Job`：`id,kind:ingest|replicate|analyze|index|group|decide|entity,status:pending|running|succeeded|failed,upload_id:string,replica_id:string,material_id:string,attempts:integer,error:string,progress_message:string,progress_current:integer,progress_total:integer,logs?:[{time,level:info|error,message,elapsed_ms?:integer,duration_ms?:integer,metrics?:object}],started_at:date-time|null,finished_at:date-time|null,duration_ms:integer,created_at,updated_at`。详情包含最多 200 条带时间的处理记录；列表省略 `logs`。`duration_ms` 是最近一次执行从领取到终态的耗时；运行中可按 `started_at` 计算当前耗时。日志的 `elapsed_ms` 是相对本次执行开始的偏移，`duration_ms` 表示当前记录覆盖步骤的耗时，`metrics` 只保存应用可控的非敏感事实（端点名、字节数、吞吐、页数、模型名、校验级别、分享状态等）。进度数量只表示当前阶段（文件数、批次数或传输字节数），`progress_total=0` 表示当前阶段没有可计算的总数，不代表整个任务百分比。旧任务可能没有记录或新指标。重试返回同一 job_id，已完成任务禁止重试；入队和启动恢复保证未完成工作不会仅存在内存中。主端和从端节点通过带租约和心跳的方式领取任务。任务响应还包含 `node_id`、`node_name`，表示最近一次执行的节点。

`Drive`（网盘）：`id,name(1..128),kind:local|webdav|lanzou|123pan|onedrive|s3,enabled:boolean,root_path:string,max_file_bytes:integer(>=0，0=不限),allowed_exts:string[],read_order:integer(0..1000),on_path_change:move|keep,route_mode:pool|preferred|exclusive,routes:DriveRoute[],config:object<string,string>,secret_configured:boolean,capabilities:{upload,download,share,folders,move,delete:boolean},stats:object<string,integer>,created_at,updated_at`。`DriveRoute` 为 `node_id,node_name,is_master,priority(1..1000),enabled,status`；优先级数字越小越优先。`pool` 让所有符合任务类型的节点竞争；`preferred` 按路由顺序选择节点，全部配置节点不可用时回退到节点池；`exclusive` 只允许配置的节点。`local` 网盘只能由主端执行。网盘只描述连接方式，上传哪些文件、放在什么路径由同步规则决定。`root_path` 是本系统在该网盘中使用的根目录（安全相对路径；蓝奏云为文件夹 ID）。`read_order` 越小，本机原件缺失时越先读取。`on_path_change=move` 时期望路径变化会在远端移动/改名，`keep` 时保持原位并标记路径偏离；蓝奏云与 123 云盘只能用 `keep`，省略时按类型取默认值。蓝奏云 `max_file_bytes` 为 0 时按 100 MiB 处理。`stats` 按副本状态计数，`size_bytes` 为 ready 与 orphaned 副本的字节数；`capabilities` 和 `stats` 不发起网络请求。任何 GET 不返回 secret。

`SyncRule`：`id(1..36，列表内唯一),name(1..128),enabled:boolean,is_default:boolean,conditions:RuleConditions,targets:SyncTarget[]`。`RuleConditions`：`subjects?:Subject.code[],doc_types?:DocType[],grades?:Grade[],cohorts?:integer[],regions?:string[],tags?:string[],parts?:Part[],media_types?:string[],min_size?:integer,max_size?:integer(0=不限)`；字段之间是「且」，同一字段多个值是「或」，全部省略匹配所有文件。`SyncTarget`：`drive_id,path_template,share:boolean,required:boolean`；同一规则不能重复指定网盘，123 云盘不能作为目标。规则列表 1..100 条，按顺序匹配，**命中第一条启用的规则为止**；最后一条必须是 `is_default=true`、启用且无条件的兜底规则（targets 可为空，即不上传）。

路径模板：必须为相对路径、以 `{文件名}` 结尾，变量为 `{学科}`（单学科取名称，多学科为「综合」，无学科为「未分学科」）、`{年级}`、`{学期}`、`{届别}`（如 `2026届`）、`{地区}`、`{机构}`、`{类型}`（缺省「未分类」）、`{考试年月}`、`{套卷}`（缺省「未命名资料」）、`{部件}`（缺省「其他」）、`{文件名}`、`{sha8}`；默认 `{学科}/{届别}/{类型}/{套卷}/{文件名}`。每段替换 `/ \ : * ? " < > |` 和控制字符为 `_`、空段写为「未知」、截断到 100 字符；编目值里的斜杠不会产生新目录。结果拼接到网盘 `root_path` 之下；蓝奏云只取文件名。同一网盘上两个不同文件解析到相同路径时，后来的文件名追加 ` (sha8)`。

`Replica`：`id,blob_id,drive_id,rule_id,desired_path,remote_path,skip_reason,share:boolean,required:boolean,status:pending|uploading|moving|ready|failed|skipped|orphaned,size_bytes,verification:uploaded|size|sha256,error,share_status:""|pending|ready|unsupported|failed,share_url:string,share_password:string,share_expires_at:date-time|null,created_at,updated_at`。每个 Blob×Drive 至多一个副本。`desired_path` 是当前规则解析出的路径，`remote_path` 是文件实际所在路径，两者不同表示待移动或路径偏离。skipped 表示网盘停用、超过大小上限或扩展名不允许，不算失败；orphaned 表示已不被任何规则需要但远端文件仍在（规则外副本），系统不会自动删除。远端对象 ID 不开放给接口。

`SyncAction`：`material_id,file_name,rule_id,rule_name,drive_id,replica_id,action:upload|move|share|skip|orphan|cancel|drift|unchanged|not_required,desired_path,remote_path,reason,status,error,share_status,share:boolean,required:boolean`；`status/error/share_status` 是执行该动作前的副本现状。cancel 表示从未写入网盘的副本将被撤销；drift 表示路径已变但网盘设置为保持原位。

`APIKey`：`id,name,scopes:string[],expires_at:date-time|null,revoked:boolean,created_at`。`User`：`id,username,role:admin|editor|viewer,enabled:boolean,created_at`。`Audit`：`id,user_id,username,action,target_id,created_at`（username 按当前用户查询，系统操作或用户不存在时为空；仅存元数据，不存正文/凭据）。

## 认证与管理

### POST /auth/login（公开）

JSON `{"username":"admin","password":"至少12位的用户口令"}`。200 data=`{"access_token":"JWT","refresh_token":"opaque","expires_in":900,"user":User}`。错误凭据统一 401；登录有节流。默认没有固定管理员密码，启动时通过环境变量自举。

### POST /auth/refresh（公开）

JSON `{"refresh_token":"opaque"}`。200 同登录；旧 refresh token 立即失效，refresh 最长 7 天，服务端仅存哈希。**重用检测**：提交已经轮换过的旧 refresh token 返回 401，并吊销该会话（已签发和刚轮换的 access/refresh 全部失效），同时写审计 `auth.refresh_reuse`；客户端遇到 401 应重新登录，不要重试旧 token。

### POST /auth/logout（JWT）

JSON `{"refresh_token":"opaque"}`。204；注销关联会话，同时使其 access JWT 失效。无效 token 也返回 204，不泄露会话存在性。

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

JSON 部分更新，可含 title/doc_type/subject_codes/grade/term/cohort/region/organization/exam_date/exam_name/chapter/tags/description，至少一个字段，枚举与长度按数据字典校验。PATCH 中出现的编目字段加入 `locked_fields`，之后 AI 编目不再改动；`title` 非空会锁定标题并同步全部成员文件名，空串解除标题锁定、恢复自动命名。`region` 必须取固定地区列表；`organization` 与 `tags` 按名称词典归一：已有写法（含别名）映射到规范名称，新名称建立人工锁定的条目。200 Pack。

### POST /packs/bulk（materials:write）

JSON `{"ids":["uuid"],"patch":{...}}`；1..500 个套卷，patch 至少一个字段且不能设置 title。事务批量应用，200 data=`{"count":n}`。部分套卷不存在时返回 404。

### POST /packs/{id}/merge（materials:write）

JSON `{"source_ids":["uuid"]}`；1..100 个来源套卷。来源的文件按顺序并入目标、专题收录关系跟随迁移，目标的空编目字段用来源补齐，来源清空后删除。200 合并后的 Pack。来源不存在 404。

### PUT /packs/{id}/order（materials:write）

JSON `{"material_ids":["uuid"]}`；必须恰好包含该套卷全部文件，否则 400。按数组顺序写入 position，200 Pack。

### GET /packs/{id}/download（materials:read）

200 `application/zip`，按 position 打包全部成员原件，重名追加序号；套卷没有文件返回 404。与原文件一样，不可用副本返回 503。

### POST /packs/trash · POST /packs/restore · POST /packs/purge（materials:write）

JSON `{"ids":["uuid"]}`；每次 1..500 个，返回 200 data=`{"count":n}`。trash 把套卷及其文件软删除进回收站；restore 恢复回收站中的套卷；purge 只接受已在回收站中的套卷（否则 409），彻底删除文件、题目与解析缓存，并删除无引用的本地 CAS 文件。回收站保留 30 天，到期由后台任务清理。**purge 不会删除网盘上的文件**：已上传到云盘的副本转为 orphaned（规则外副本）并保留 Blob 记录，之后再次上传相同内容会重新接管；需要删除时在 `POST /storage/replicas/bulk` 中执行 `delete_remote`。`local` 网盘中的文件和从未上传的副本行会直接删除。审计记录 pack.trash / pack.restore / pack.purge。

### GET /materials（materials:read）

查询：page、page_size、`q`（标题/原始文件名，<=200）、`stage`（Stage）、`part`（Part）、`status`（MaterialStatus）、`sync_status`（SyncStatus）、`reason`（需关注原因或校验代码前缀，小写字母与下划线）、`pack_id`、`sha256`（64 位十六进制，精确查询相同内容）。200 Page<Material>；`stage=attention` 按 created_at 升序，其余按时间倒序。

### GET /inbox（materials:read）

按套卷分页读取需处理的文件，查询 `page`、`page_size`、`stage`（默认 `attention`）和 `reason`。200 Page<InboxPack>；每个套卷只返回一次，`pack` 包含全部成员，`items` 只包含命中当前 stage/reason 的文件及其需关注原因。适合管理台按套卷核对，避免同一套卷跨页重复。

### GET /materials/stats（materials:read）

200 data=`{"packs":3,"files":8,"processing":1,"waiting_ai":0,"needs_attention":2,"ready":5,"missing_answers":1,"trashed":0,"archived":8,"missing":0,"waiting_catalog":1,"not_required":0,"syncing":2,"synced":5,"attention":0}`。`packs` 为未删除套卷数；`files` 为未删除文件数，按 stage 拆分 processing/waiting_ai/needs_attention/ready；`missing_answers` 为缺答案的试卷类套卷数；`trashed` 为回收站套卷数；`archived/missing` 按 MaterialStatus 统计，`waiting_catalog/not_required/syncing/synced/attention` 按 SyncStatus 统计。未出现的状态为 0。

### GET /materials/{id}（materials:read）

200 Material 详情，含 `pack`、`sources`、`replicas`。分享 URL/提取码只在此有权限响应中返回，不记录日志。404 不存在。

### PATCH /materials/{id}（materials:write）

JSON 可含 `part`（Part）、`answer_start_page`（0..10000）、`pack_id`（移入已存在套卷）或 `new_pack=true`（拆成独立套卷），至少一个字段。不允许修改 title、SHA、归档/同步状态或副本。200 Material 详情；`part` 改为非 questions/full 时会移除旧题目索引，套卷标题与成员文件名按新 part 重新生成。人工修改会把文件置为 ready 并立即按当前编目生成同步计划。

### POST /materials/accept（materials:write）

JSON `{"ids":["uuid"]}`；1..500 个文件。只处理 stage=attention 且已归入套卷的文件：按当前编目置为 `ready`、清空需关注原因并立即生成同步计划；其他文件忽略（不打断 AI 流程）。200 data=`{"count":n}`。审计记录 material.accept。

### GET /materials/{id}/download（materials:read）

200 二进制，Content-Type 取已验证媒体类型；Content-Disposition attachment，UTF-8 文件名；X-Content-Type-Options: nosniff。同源 JWT 通过 fetch Blob 下载，不将 Token 放入 URL。先检查本地原件，再按网盘 `read_order` 升序尝试已写入网盘的副本（含 orphaned）；没有可用副本 503。Range 请求本期不承诺。

### POST /materials/{id}/pull-cache（materials:read）

从已写入网盘的副本中按网盘 `read_order` 升序拉取文件，流式写入 `DATA_DIR/tmp/material-pulls`，同时校验资料的声明字节数和 SHA-256。只有完整匹配才原子发布为临时文件；不会写入永久 CAS，也不会修改副本状态或资料状态。没有可用副本、所有副本读取失败或内容校验失败返回 503，失败内容不会保留为可下载文件。

200 data=`{"download_url":"/api/v1/materials/<id>/pull-cache","file_name":"试卷.pdf","media_type":"application/pdf","size_bytes":12345,"sha256":"<64位小写十六进制>","expires_at":"2026-10-30T12:00:00Z","expires_in":900}`。`download_url` 是服务端相对路径，必须继续携带相同 Bearer 凭据；不得把 Token 写进 URL。未过期且尺寸正确的现有临时文件会直接复用，重复 POST 不会延长 TTL。`expires_in` 为剩余秒数。

TTL 由服务端环境变量 `PULL_CACHE_TTL` 控制，默认 `15m`。修改配置需重启对应进程；重启不会清除尚未到期的文件。过期判断基于临时文件的 mtime。

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

服务端拒绝路径穿越、符号链接、重复路径、加密 ZIP 与解压炸弹；提取前检查整个目录，成员真实格式与扩展名不符时不发布资料。每个 ZIP 展开上限 2 GiB、5000 个非忽略文件、单成员压缩比 200。支持 PDF/DOC/DOCX/PPTX 和嵌套 ZIP 成员，整个 ZIP 链最多递归展开 3 层；超过上限或其他不支持文件跳过并写入任务日志，管理台在完成页提示，只有根 ZIP 中没有可归档文件时任务失败。成员按自然文件名顺序 position；角色仅文件名规则推测，默认为 other。

## 任务与存储

### GET /jobs · GET /jobs/{id}（jobs:read）

列表查询 page/page_size/status/kind/material_id/upload_id/replica_id，200 Page<Job>；详情 200 Job。关联 ID 均为精确匹配，用于从资料、上传清单或副本直接下钻任务。

### GET /jobs/stats（jobs:read）

200 data=`{"total":12,"pending":1,"running":1,"succeeded":9,"failed":1}`；一次统计全部任务的状态，未出现的状态为 0。`total` 包括所有状态。

### POST /jobs/{id}/retry（jobs:write）

仅 failed 可重试，202 Job（同 id，status=pending，attempts 归零）。其他状态 409。AI 任务重试时相关文件回到 processing。秘密错误须脱敏。

### POST /jobs/{id}/cancel（jobs:write）

取消 pending 或 running 的任务，200 Job（`status=failed`，`error="已由管理员取消"`）；其他状态 409。任务按该类型最终失败处理：阅读任务让文件按文件名继续并标记 metadata_only，分组任务退回单文件成套，编目任务标记 ai_failed，复制任务的传输中副本标记 failed（规则外副本保持不变），索引状态标记 failed，归档任务的上传会话标记 failed。主端正在执行的任务立即停止；从端在下一次心跳的 `lost_job_ids` 中收到并停止，之后提交的结果因租约失效返回 409。取消后可用 retry 重新排队。审计 job.cancel。

### POST /jobs/retry-failed（jobs:write）

JSON 可空或 `{"kind":"analyze"}`；重新排队最多 500 个失败任务（可只选一类）。202 data=`{"count":n}`。

失败策略：暂时性失败（限流、超时、服务不可用、节点失联、写结果失败）按 1 分钟、5 分钟、30 分钟、2 小时、12 小时退避自动重试，最多 6 次；租约多次过期（例如执行时进程崩溃）的任务达到上限后停止重试并标记失败。AI 任务（analyze/group/decide/entity）因缺少模型、OCR 或密钥被拒而失败时不计入次数，文件进入 waiting_ai，每 15 分钟自动再试；保存 AI 模型、OCR 服务商或 AI 解析设置后立即继续。重试用尽后：阅读失败的文件按文件名继续分组和编目并标记 metadata_only；分组失败的文件单独成套并标记 group_unverified；编目失败的套卷标记 ai_failed，暂不同步。

### GET /storage/drives · POST /storage/drives

GET storage:read，返回 `Drive[]`（按 read_order、创建时间排序，不分页）。POST storage:write，201 Drive。JSON：

```json
{"name":"学校 WebDAV","kind":"webdav","enabled":true,"root_path":"paper-collect","max_file_bytes":0,"allowed_exts":[".pdf",".docx"],"read_order":20,"on_path_change":"move","route_mode":"preferred","routes":[{"node_id":"worker-home","priority":1},{"node_id":"master","priority":2}],"config":{"base_url":"https://dav.example/dav"},"secret":{"username":"user","password":"secret"}}
```

`name`、`kind` 必需；省略 enabled/read_order 时默认 true/100，`on_path_change` 省略时 lanzou/123pan 为 keep、其余为 move。省略 `route_mode` 时为 `pool`；配置路由时每个 node_id 只能出现一次，priority 必须为不重复的 1..1000 整数。`allowed_exts` 自动转小写并补齐前导点，为空表示不限。config 与 secret 均为 object<string,string>，字段按适配器：

| kind | config | secret |
| --- | --- | --- |
| local | 无；根目录由服务端固定为 `DATA_DIR/drives/<driveID>`，API 不允许指定 | 空 |
| webdav | base_url 必需（http/https，无内嵌凭据）；public_base_url 可选（真正公开目录的根 URL） | username/password 必需 |
| lanzou | base_url 可选，默认 https://pc.woozooo.com；verify_remote_hash 可选 "true"/"false"（默认 false，开启后上传会再下载重算 SHA，耗时且失败时远端可能残留文件）。文件夹 ID 写在 `root_path`，默认 -1 | cookie 必需（管理员有效登录会话） |
| 123pan | base_url 默认 https://open-api.123pan.com；parent_file_id 默认 0 | client_id/client_secret 必需（开放平台申请） |
| onedrive | auth_mode 为 direct（服务端默认）或 openlist；direct 时 client_id 必需、tenant 默认 common；share_scope 为 anonymous（默认）、organization 或 disabled | refresh_token 必需；direct 时 client_secret 可选；share_url 可选，共享文件夹链接作为秘密保存 |
| s3 | bucket、region 必需；endpoint 可选；path_style 默认为 false；share_expires_seconds 默认 3600，范围 60..604800 | access_key_id、secret_access_key 必需；session_token 可选 |

config 或 secret 中出现该 kind 不支持的键会被拒绝；创建前会构造一次适配器校验配置，但不联网。API 不返回传入 secret，`secret_configured` 只表示已保存，不代表已连通。审计记录 storage.drive_create。

适配器能力边界（实现事实，非联调声明）：

| kind | 上传/下载 | 分享 | 子目录 | 移动 | 删除 |
| --- | --- | --- | --- | --- | --- |
| local | 可用 | 不支持 | 支持 | 支持 | 支持 |
| webdav | 可用 | 配置 public_base_url 时 | 支持 | 支持（MOVE） | 支持 |
| lanzou | 可用（非官方接口） | 可用 | 不支持 | 不支持 | 不支持 |
| 123pan | 不可用 | 不可用 | — | — | — |
| onedrive | 可用 | share_scope≠disabled 时 | 支持 | 支持 | 支持 |
| s3 | 可用 | 签名链接 | 支持（key 前缀） | 复制后删除，非原子 | 支持 |

- 所有适配器的上传都**不覆盖**已有对象：目标路径已有相同内容（大小一致，能校验 SHA 时 SHA 也一致）则直接接管，否则失败为 `conflict`。移动同样不覆盖目标。删除时对象已不存在视为成功。
- local：提供 SHA 时 verification=sha256，否则为 size。
- webdav：未配置 public_base_url 时不支持分享，配置后返回公开目录拼接链接，不代表该 URL 可匿名访问。提供 SHA 时上传后会完整下载重算。
- lanzou：按 cookie 会话工作；单文件上限 100 MiB；默认 verification=uploaded，开启 verify_remote_hash 才可能为 sha256；分享返回 URL 与提取码。未对真实服务联调。
- 123pan：只实现 access_token 与 Health 探测；upload/download/share 返回 unsupported（错误码 123pan_upload_unverified 等），Capabilities 全为 false，规则不能把它作为目标。
- onedrive：国际版 Microsoft Graph 支持上传、下载、移动、删除和只读分享链接；250 MiB 以内的文件直接上传，更大的文件使用分段上传，冲突时只接管大小一致的同名文件，并清理中断留下的 0 字节占位文件后重试。暂不支持中国世纪互联版和政府云。`auth_mode=direct` 使用自己的应用，通过微软端点续期；`auth_mode=openlist` 使用 OpenList 提供的应用，从 OpenList Token 页面取得刷新令牌，续期时通过固定的 `https://api.oplist.org/onedrive/renewapi` 发送令牌。OpenList 的接口将令牌置于 URL 查询参数，提供方或中间环节可能记录；本服务不记录该 URL、口令或令牌，禁止续期重定向。OpenList 授权请求 `Files.ReadWrite.All` 和 `offline_access`，自有应用可使用 `Files.ReadWrite` 和 `offline_access`。更换 `auth_mode` 需要同时提交新模式对应的 `secret.refresh_token`，`secret` 仍整体替换，其他必要凭据须一并提交。缺少 `auth_mode` 的网盘按 direct 处理。刷新令牌轮换后加密保存；`share_url` 可指定有写权限的共享文件夹。匿名分享可能被租户禁用，organization 仅适用于组织账号；分享失败不影响副本。Health 只检查文件夹可访问，不保证写入权限。验证级别为 size。未对真实 OneDrive 与 OpenList 联调。
- s3：使用 AWS SDK 支持 AWS S3 和兼容端点；大文件自动分段并行上传、失败时尝试中止分段，上传后 HEAD 检查大小；分享为签名下载链接，过期后需刷新，临时凭据可能使链接提前失效。Health 使用 HeadBucket，要求相应的桶访问权限。未对真实 S3 联调。

### PATCH /storage/drives/{id}（storage:write）

可修改 name/enabled/root_path/max_file_bytes/allowed_exts/read_order/on_path_change/route_mode/routes/config/secret；kind 不可改。config 若出现整体替换；secret 若出现整体替换，空对象清空，省略保留。路由变更会唤醒等待中的复制任务；正在运行的任务继续由原租约完成。200 Drive。修改网盘设置不会自动作用到已有文件；需要时调用 `POST /storage/reconcile`（管理台为存储页的「补齐副本」）。停用网盘后，新计划中该网盘的目标记为 skipped，已写入的副本仍可读取。审计记录 storage.drive_update。

### POST /storage/drives/{id}/test（storage:write）

执行真实 adapter.Health，200 data=`{"healthy":true|false,"message":"脱敏说明","capabilities":{"upload":true,"download":true,"share":false,"folders":true,"move":true,"delete":true}}`；连接/认证失败 healthy=false，不伪造成功。适配器实现与实际网盘连通是两个独立事实。

### GET /storage/rules（storage:read）

200 `SyncRule[]`，按顺序排列，最后一条为兜底规则。空库启动时自动创建兜底规则「其他所有文件」，没有目标，即默认不上传任何网盘。

### POST /storage/rules/preview（storage:write）

查询 page/page_size；JSON `{"rules":SyncRule[]}`（完整的草稿列表）。不写库，对所有已编目文件计算草稿规则下的动作。200 data=`{"preview_token":"...","counts":{"upload":128,"move":30,"orphan":12,"skip":4,"unchanged":900,"waiting_catalog":3},"items":SyncAction[],"total":174}`；`counts` 按动作统计（含 unchanged 和尚在等待编目的文件数），`items`/`total` 只含需要变化的动作并分页。`preview_token` 绑定当前用户、草稿内容以及当时的规则和网盘配置，15 分钟内有效。

### PUT /storage/rules（storage:write）

JSON `{"rules":SyncRule[],"preview_token":"..."}`。必须提交与预览完全相同的草稿；预览之后规则或任何网盘被修改、令牌过期或不属于当前用户时返回 409，需要重新预览。在一个事务中整体替换规则，并为所有已编目文件重新生成计划。202 data=`{"queued":integer}`。审计记录 storage.rules_update。

### GET /storage/rules/explain（storage:read）

查询 `material_id` 必需。返回该文件按当前规则得到的 `SyncAction[]`：命中的规则、每个网盘的期望路径、实际路径、动作和副本现状；规则不上传任何网盘时返回一条 `action=not_required`。只读，不改变副本。404 文件不存在。

### POST /materials/{id}/sync（storage:write）

无需请求体。即使文件仍在等待编目，也立即按当前编目和规则生成计划。202 data=`{"queued":integer}`。审计记录 material.sync。

### GET /storage/replicas（storage:read）

查询 page/page_size、`status`（Replica.status 之一，或 `drift`=已写入但实际路径与期望路径不同的非规则外副本）、`drive_id`。200 Page<Replica>，按更新时间倒序。

### POST /storage/replicas/bulk（storage:write）

JSON `{"ids":["uuid"],"action":"retry|delete_remote|forget"}`；1..100 个。逐个处理，200 data=`[{"id":"...","success":true,"error":"..."}]`。有等待或运行中任务的副本返回 409，需稍后重试。

- `retry`：按当前规则重新规划该副本所属文件（失败副本重新排队）。
- `delete_remote`：只用于 orphaned 副本；调用适配器删除远端文件后删除记录，审计 storage.replica_delete。删除期间以一条运行中的 replicate 任务占位，防止并发操作；若删除期间规则重新需要该副本，则改为重新上传。网盘不支持删除时报错。
- `forget`：只用于 orphaned 副本；只删除记录，远端文件保留，审计 storage.replica_forget。

### POST /storage/reconcile（storage:write）

无需请求体。按当前规则重新规划所有已编目文件：补建上传、移动、补分享，把不再需要的副本标记为 orphaned（从未写入网盘的直接撤销），不删除远端文件。202 data=`{"queued":integer}`。审计记录 storage.reconcile。

### POST /storage/replicas/{id}/share（storage:write）

刷新/补建已有 ready 副本分享；200 Replica。网盘不支持分享时 share_status=unsupported、share_url 为空；远端分享失败返回 share_status=failed、脱敏 error，副本仍 ready。URL 不等于下载校验成功，verification 单独记录。

## 智能识别（LLM + OCR）

AI scope：`ai:read`（读取模型/OCR 配置与解析设置）、`ai:write`（修改配置、探测、测试）。两者默认只授予 admin，可单独授权给 API 密钥。资料分析读取沿用 materials:read，触发与应用沿用 materials:write。密钥只写不读：响应只返回 `api_key_configured`、`token_configured` 和 `ocr_configured`；服务端用 MASTER_KEY 加密，AAD 为 provider id。

`AIProvider`：`id,name(1..128),base_url(http/https，OpenAI 兼容根，通常以 /v1 结尾),models:string[](0..200，每项 1..200 字符，自动去重),default_model:string(<=200),vision_model:string(<=200，可空),enabled:boolean,priority:integer(0..1000),api_key_configured:boolean,created_at,updated_at`。`POST /ai/providers` 请求 `{"name":"主模型","base_url":"https://api.example.com/v1","api_key":"sk-...","priority":10,"enabled":true,"default_model":"gpt-4o-mini","vision_model":"gpt-4o","models":["gpt-4o-mini","gpt-4o"]}`；创建时 `api_key` 可省略（表示没有密钥；需要鉴权的端点会失败），PATCH 时省略保留、空串清空。PATCH 部分更新，不允许改 id；不提供物理删除，使用 `enabled=false` 停用。

- `GET /ai/providers`（ai:read）：200 数组，按 priority 升序。
- `POST /ai/providers`（ai:write）：201 AIProvider。
- `PATCH /ai/providers/{id}`（ai:write）：200 AIProvider。
- `POST /ai/providers/probe`（ai:write）：请求 `{"base_url":"...","api_key":"..."}`；提供 `provider_id` 时 `base_url` 可省略（复用已存地址），`api_key` 可省略表示复用该 provider 的已存密钥；同时提供 api_key 时以请求中的密钥为准。调用 `GET {base_url}/models`，200 `{"models":["..."],"count":2}`；远端错误按 502/503 返回脱敏原因，不得回显密钥。
- `POST /ai/providers/{id}/probe`（ai:write）：使用已存密钥探测，只返回模型列表，**不修改**已保存的 models；需要保存列表时在 PATCH 请求中提交 models，200 `{"models":[],"count":0}`。
- `POST /ai/providers/{id}/test`（ai:write）：请求 `{"model":""}`，空则用 default_model；发一次最小 chat，200 `{"healthy":true|false,"model":"...","message":"脱敏说明"}`。远端失败返回 healthy=false，不伪造成功。

`OCRProvider`：`id,name(1..128),kind:mineru|paddleocr|rapidocr,base_url(http/https),model(<=200；PaddleOCR 默认 PP-OCRv6，RapidOCR 默认 PP-OCRv5，MinerU 可为空),enabled:boolean,priority:integer(0..1000),token_configured:boolean,created_at,updated_at`。请求字段为 `{"name":"RapidOCR（自建）","kind":"rapidocr","base_url":"http://rapidocr:9003","model":"PP-OCRv5","enabled":true,"priority":10}`；自建 RapidOCR 可以省略 `token`。外部 PaddleOCR/MinerU 的 `token` 只写不读，PATCH 省略保留、空串清除。kind 创建后不可修改；不提供物理删除，使用 `enabled=false` 停用。PaddleOCR token 从 [AI Studio PaddleOCR 任务页](https://aistudio.baidu.com/paddleocr/task) 获取。

- `GET /ai/ocr-providers`（ai:read）：200 OCRProvider 数组，按 priority 升序。
- `POST /ai/ocr-providers`（ai:write）：201 OCRProvider。缺少 `kind` 时按 paddleocr；PaddleOCR 缺少 model 时默认 `PP-OCRv6`，RapidOCR 缺少 model 时默认 `PP-OCRv5`。
- `PATCH /ai/ocr-providers/{id}`（ai:write）：200 OCRProvider。
- `POST /ai/ocr-providers/{id}/test`（ai:write）：用已保存配置探测可达性与令牌；200 `{"healthy":boolean,"message":"脱敏说明"}`，不伪造成功。

`AISettings`：`ocr_provider_id:string`（当前选中的 OCRProvider；为空时选择 priority 最小的启用服务商）,`max_pages:integer(1..10，默认 3；参与限制发送给视觉模型的图片数量，OCR 仍解析全文，文本另有长度上限)`,`max_images:integer(0..4，默认 2)`,`ocr_configured:boolean`（当前服务商令牌状态）,`updated_at`。编目始终由 AI 完成，没有开关。

选择自建 RapidOCR 时，DOCX 由 Go 直接解析 OOXML：正文、标题、表格、文本框、公式和分页提示进入解析缓存，嵌入的位图再逐张提交同一 OCR 接口；识别和索引共享解析缓存。外部图片、模板、OLE 对象和数据源不会读取。常见 OMML 分式、上下标、根号、求和/积分、括号、上下限、横线和矩阵保留为 LaTeX，其他结构尽量保留文本。该 DOCX 路径不经过 LibreOffice 或 PDF 文本层，分析/搜题无需 `soffice`，也无需同步升级 RapidOCR 镜像。选择 MinerU 或 PaddleOCR 时，DOC/DOCX/PPT/PPTX 会先由 LibreOffice 转为 PDF；RapidOCR 的 DOC/PPT/PPTX 也走同一转换。DOCX/PPTX 转 PDF 前会从临时副本移除可触发网络访问的外部关系并保留 inert 超链接。页码来自显式分页或文档保存的分页提示，连续文档可能只有第 1 页；复杂公式、图片 OCR 和复杂排版不能保证与原文完全一致。

- `GET /ai/settings`（ai:read）：200 AISettings。
- `PUT /ai/settings`（ai:write）：请求 `ocr_provider_id`、`max_pages`、`max_images`；保存后等待 AI 的任务立即继续；省略 `ocr_provider_id` 表示保留当前选择，空串表示清除选择；200 AISettings。PaddleOCR 使用 `https://paddleocr.aistudio-app.com` 的 `POST /api/v2/ocr/jobs`、`GET /api/v2/ocr/jobs/{jobId}` 与 JSONL `jsonUrl`；`PP-OCRv6` 沿用官网 v5 兼容异步协议。提交响应明确为“任务提交队列已满”时最多尝试 6 次并按 2、4、8、16、30 秒退避；成功创建 jobId 后不会重复提交。自建 RapidOCR 使用 `POST /api/v1/ocr/jobs`、`GET /api/v1/ocr/jobs/{id}` 和 `result_url` JSONL；服务地址在 Compose 内为 `http://rapidocr:9003`，不对外暴露。

自建服务当前固定使用 RapidOCR 3.10.0 的默认 PP-OCRv6 small；请求中的 `model` 字段仅保留协议兼容，并不会切换模型。现有 JSONL 每页使用 `page_no` 和 `texts` 字符串数组。接收端也支持按阅读顺序排列的 `blocks`：`{"type":"text","text":"..."}`、`{"type":"inline_formula","latex":"..."}` 和 `{"type":"display_formula","latex":"..."}`；`latex` 为不含外围分隔符的原始 LaTeX，JSON 中的反斜杠须转义。有非空 `blocks` 时优先使用它并忽略 `texts`，未知块类型或空公式会失败而不静默丢失。该协议为后续公式模型提供入口；当前 RapidOCR 服务仍只输出 `texts`，没有接入扫描件公式识别模型。

`MaterialCard`（AI 对单个文件的阅读记录）：`material_id,status:none|pending|running|succeeded|failed,provider_name,model,page_count,metadata_only:boolean,card:Card,problems:Problem[],markdown_excerpt:string(<=4000),error,updated_at|null`。`Card`：`role:Part,answer_start_page?,title_text?,doc_type?,subjects?,grade?,school_year_start?,cohort?,exam_date?,org_mentions?:string[],region_mentions?:string[],chapter?,fingerprint?:string[],evidence?:{字段:原文摘录},metadata_only?`。`Problem`：`code,field?,message,critical?`；critical 表示结果需要关注，其余表示被剔除的值。

`PackAI`（套卷最近一次 AI 编目决定）：`status:none|pending|running|succeeded|failed,provider_name,model,revision,decision:Decision|null,problems:Problem[],error,decided_at|null,cards:{material_id:Card}`。`Decision`：`doc_type,subjects,grade?,school_year_start?,cohort?,exam_date?,term?,region?,organization?:{id}|{new,aliases?},exam_name?,chapter?,tags?:[{id}|{new}],files:[{id,role,answer_start_page?}],evidence?`。

`Entity`（名称词典条目）：`id,kind:organization|tag,name,region,source:ai|human,locked:boolean,aliases:string[],usage:integer`。一所学校、一个联盟或一个标签只有一个规范名称，文中的写法（简称、旧称、带省市前缀等）都是它的别名；套卷的 `organization` 与 `tags` 只使用规范名称。

- `GET /materials/{id}/analysis`（materials:read）：200 MaterialCard；尚未阅读时 status=none。
- `POST /materials/{id}/analysis`（materials:write）：请求可空；让 AI 重新阅读该文件，之后重新确定所在套卷的编目。202 data=`{"queued":true}`。审计 material.ai_reread。
- `GET /packs/{id}/ai`（materials:read）：200 PackAI。
- `POST /packs/{id}/ai/redecide`（materials:write）：让 AI 重新确定该套卷编目，`locked_fields` 与 `part_locked` 保持不变。202 data=`{"queued":true}`。审计 pack.ai_redecide。
- `POST /ai/rerun`（ai:write）：JSON `{"mode":"decide"|"full","scope":"attention"|"all","dry_run":true}`。decide 保留分组，只重新编目所选文件所在的套卷；full 让所选文件重新阅读（已有阅读记录直接复用）、分组并编目。dry_run=true 只统计。200 data=`{"materials":n,"packs":n,"estimated_calls":n,"applied":boolean}`。审计 ai.rerun_decide / ai.rerun_full。
- `GET /entities`（materials:read）：查询 `kind`（必填）、`q`（名称或别名）、page、page_size；200 Page<Entity>。
- `PATCH /entities/{id}`（materials:write）：JSON `{"name"?,"region"?,"locked"?,"remove_aliases"?:string[]}`；改名同步到所有使用该条目的套卷（标题、文件名与网盘路径随之更新），名称已属于其他条目时 409；任何修改都会锁定条目（可用 locked=false 解锁）。200 Entity。审计 entity.update。
- `POST /entities/{id}/merge`（materials:write）：JSON `{"into":"目标 id"}`；别名与套卷迁移到目标，源条目删除，目标锁定。200 Entity。审计 entity.merge。

AI 编目链路（编目只由 AI 决定；规则只用于校验模型的回答）：

1. **阅读（analyze，可在从端执行）**：每个新文件归档后入队。读取原件 → 当前 OCRProvider 解析（或复用缓存的正文）→ 模型输出阅读记录 Card。音频、表格等无法解析正文的文件，以及解析失败或没有正文的文件，只依据文件名和来源路径阅读，并标记 `metadata_only`。Card 中的标题、年级、学年、届别、考试年月必须附带能在材料中逐字找到的原文摘录，`org_mentions`、`region_mentions`、`fingerprint` 也必须原样出现在材料中，否则相应值被剔除；文件名、路径、上传提示和同文件夹文件名只是旁证。长文档保留开头约 2/3 与末尾约 1/3 共 8000 字并标注省略。
2. **分组（group，主端，同一时间只运行一个）**：同一批上传的文件全部读完后（等待 AI 或阅读失败的文件不阻塞），按来源路径排序，每次最多 80 个文件（同一文件夹最多延伸到 120 个）连同候选套卷交给模型分组。候选包括文件当前所在套卷、本批已建立的套卷，以及学科相同且年级或届别相同的已有套卷（按标题、机构、考试年月相近程度取前 40 个，仅用于提供候选，归属由模型决定）。回答必须把每个文件恰好分到一组、只能引用候选套卷，同一套中试题卷、答案、前题后答、教师版、答题卡各最多一份且学科不能混杂；不合格时附上问题让同一模型修正一次，再尝试下一个模型；仍无合格结果时每个文件单独成套并标记 `group_unverified`。
3. **编目（decide，主端）**：套卷成员变化后入队；成员仍在阅读或分组时推迟。模型看到全部成员的阅读记录与正文摘录（最多 8 个文件各 3000 字）、人工锁定的字段、机构候选（按与文中写法的相似度和地区排序取前 30 个）和常用标签，输出整套编目与每个文件的部件。校验：doc_type 必填；试卷类必须有学科和年级；学年与届别必须一致（高中 届 = 学年开始年份 + (12 − 年级) + 1，初中按 9 年级计算），只给出其一时由服务端推算；考试年月必须落在学年内（当年 8 月至次年 7 月），学期由考试月份推算（8 月至次年 1 月为上学期）；地区必须取固定列表；年级、学年、届别、考试年月、考试名称和机构需有原文依据或与已校验的阅读记录一致；机构必须优先选用候选条目，新建时必须写完整正式名称（比原文写法更短的会被退回），与任何已知名称或别名归一后相同的“新名称”自动映射到已有条目；标签最多 5 个，新标签最多 2 个；每个文件必须恰好出现一次，部件唯一性同分组；人工锁定的字段和部件总是原样保留。不合格时修正一次再换模型；关键问题（缺类型、学科、年级，部件重复或遗漏等）未解决时套卷进入 attention 并记录校验代码，其余问题只剔除对应值。
4. **名称去重（entity，主端，同一时间只运行一个）**：模型新建机构或标签后入队；把新条目与名称相近的条目（各附最近使用的套卷标题）交给模型判断是否同一对象，确认后合并（别名、套卷一并迁移，人工锁定的条目不会被自动合并）。

编目提交后在同一事务中写入套卷字段（锁定字段除外）、规范名称、成员部件、标题与文件名，生成网盘同步计划，并按部件入队题目索引。套卷成员变化时 `revision` 递增，旧版本的编目结果被丢弃并重新安排。人工移动文件、拆分、合并套卷或把文件移入回收站都会让相关套卷重新编目。模型 provider 按 priority 故障切换；失败错误必须脱敏，模型原文不落库。RapidOCR/PaddleOCR 不保存页面图，因此视觉模型只会收到文本。

题目索引链路：解析文本按 `batch_pages`（默认 10）与 `batch_chars`（默认 12000）分组，最多 3 个批次并发切题；模型输出上限 128000 token，并请求 `reasoning_effort=high`。单批发生空响应或非法 JSON 时按页、再按正文二分重试；上游 5xx/限流最多额外重试一次，超时、鉴权等错误直接失败，不通过递归重试放大调用量。批次结果按原顺序合并去重后在同一事务写入题目与检索词。

Job 的 `material_id` 关联 analyze/index 任务资料，`pack_id` 关联 decide，`batch_id` 关联 group，`entity_id` 关联 entity。

## 搜题（纯文本）

`search:read`（读取搜索设置）、`search:write`（修改搜索设置）默认仅管理员，可单独授权给 API 密钥。搜索接口本身使用 materials:read。

索引由 `index` 任务完成：默认启用；只有 part 为 `questions`（试题）或 `full`（题目+答案同文件）的文件会在归档、AI 识别应用 part 或启动补建时入队，其他 part 不会建立题目索引，part 改为非试卷类时会移除旧索引。任务复用命中“文件 SHA + OCR 类型/地址/模型 + 缓存版本”的 `material_texts`，未命中时调用当前 OCRProvider → LLM 默认每批 10 页/12000 字（可配置）完成 OCR 纠错与切题 → 保存题目和页码 → 建立 CJK unigram+bigram 与连续字母/数字词倒排。不做跨资料同题聚合；同一题出现在多份试卷就是多条结果。索引不保存页面图/bbox；需要视觉图片时 MinerU 会重新解析。

题干保留原始公式文本/LaTeX，模型被要求保留运算符和结构；输入中已有的 LaTeX 命令若在模型 JSON 中少转义反斜杠，会在解码前修复，无法安全解码的控制字符使任务失败。搜索额外建立带运算符的公式二元/三元词项，并按公式原文的规范化形式再次校验：例如 `x+y` 与 `x-y` 可区分，`x²` 与 `x^{2}`、简单的 `\frac{x}{y}` 与 `x/y` 可匹配，全角和常见运算符写法可归一化。数学变量保留大小写；不支持通用代数等价、公式结构推理或任意 LaTeX。普通文本仍使用原有词项。公式命中高亮覆盖源文本中的公式片段，`text`/`snippet` 仍为普通文本，不返回可执行 HTML。

升级后后台分批为已有题干补建公式词项，重复执行安全，不重新调用 OCR 或切题模型；公式搜索会先补完尚未处理的旧题，首次搜索可能等待。旧题干本身已经丢失的公式无法凭索引恢复；如需从原生 DOCX 恢复常见公式结构，请对资料重建题目索引，重新解析和切题会产生正常的模型调用。

- `POST /search/questions`（materials:read）：请求 `{"query":"已知函数...","subject":"math","grade":"grade12","academic_year":0,"limit":20,"offset":0}`。`query` 必填 1..2000 字；`subject`/`grade`/`academic_year` 按字典校验；`limit` 默认 20、最大 50；`offset` >= 0。只返回已有题目、未删除的资料（归档状态 archived 或 missing）。200 `{"items":[{"question_id":"...","question_no":"12","text":"完整题干（含选项/小问）","snippet":"...","highlights":[[0,6]],"score":12.3,"page_no":3,"material":{"id":"...","title":"...","file_name":"..."},"pack":{"id":"...","title":"..."},"subject_codes":["math"],"also_in":[{"id":"...","title":"..."}]}],"total":1,"took_ms":23}`。`pack` 是文件所属套卷；`also_in` 是同一题干在其他套卷中的重复出现（按套卷去重，可空数组）。`snippet` 围绕最长连续文字匹配或命中公式截取，省略处有 `…`；`highlights` 是相对于 `snippet` 的 Unicode 字符区间数组，无匹配时为空数组，客户端自行渲染，禁止返回 HTML。
- `GET /search/questions/{id}`（materials:read）：单题详情（同上字段，score 为 0）；没有查询词，`snippet` 为题干首段，`highlights` 为空数组。
- `GET /materials/{id}/questions`（materials:read）：分页 `{id,material_id,page_no,seq,question_no,text,confidence,created_at}`，按 `seq` 升序。
- `GET /materials/{id}/index`（materials:read）：`{status:pending|running|succeeded|failed|not_applicable,job_id,page_count,question_count,llm_calls,llm_failed_batches,error,indexed_at}`；试卷类资料尚无索引记录时返回 `status=pending`、`job_id=""`；其他角色返回 `status=not_applicable`、`job_id=""`。
- `POST /materials/{id}/index`（materials:write）：`{"force":true}` 重新索引；`force=true` 会清空解析缓存（`material_texts.source_hash`）以强制重新解析；202 `{"job_id":"...","state":{...}}`。只有 part 为 `questions` 或 `full` 的文件可索引；其他 part 拒绝索引。
- `GET /search/settings`（search:read）：`{enabled,max_pages,batch_pages,batch_chars,provider_id,model}`。
- `PUT /search/settings`（search:write）：完整替换；默认 enabled=true、max_pages=500、batch_pages=10、batch_chars=12000；max_pages 1..5000、batch_pages 1..50、batch_chars 1000..100000。未指定 provider/model 时使用第一个启用端点及其默认模型；指定的 provider 必须存在且启用，否则任务失败。搜索设置可通过 HTTP 或 MCP 访问，目前没有独立管理台页面。

索引失败不阻塞归档与下载；搜索只返回已有题目的未删除资料。搜索不检查 `material_index_states.status`，因此重建失败时旧索引仍可搜索，状态显示失败。

## MCP

`POST /mcp`：使用 API 密钥作为 Bearer 凭据；`Content-Type: application/json`。服务端不强制校验 `Accept`；成功响应为 `application/json`，不提供 SSE。JSON-RPC 2.0 单条消息，batch 不支持。`initialize` 请求 protocolVersion 支持 2025-03-26、2025-06-18、2025-11-25，响应回协商版本、serverInfo、instructions 和 capabilities.tools；无 ID 通知返回 202，无响应体；`ping` result={}；tools/list 根据当前 scope 过滤并标记操作属性；tools/call 使用与 REST 相同的权限和业务校验。上传小文件支持 Base64，较大文件与下载通过返回的路径流式传输。GET /mcp=405，不发 session id，属于无状态 Streamable HTTP 的 JSON 子集。详细客户端配置和请求示例见 [MCP.md](MCP.md)。

工具及权限的完整列表见 [MCP.md](MCP.md)；`tools/list` 返回准确的参数 schema。套卷、文件、专题、题目、任务、上传、存储、AI/OCR、用户和密钥等操作与对应 REST 处理器使用相同字段与权限。`questions_search` 返回与 REST 一致的高亮、学科和套卷字段，默认 limit 为 20、最大 50。schema 拒绝未知字段；缺少/未知/类型错误参数与校验错误返回 -32602。JSON-RPC 错误 -32700 表示解析失败、-32600 请求非法、-32601 方法未知、-32602 参数非法；领域失败返回 `content=[{type:"text",text:"错误说明"}]` 和 `isError=true`。成功 content 是 JSON 序列化文本。不存在通用 URL 或服务器文件读取工具；AI 和存储配置可以按对应权限设置服务端点，工具不能绕过 HTTP 授权。

## 示例 Agent 调用

```sh
curl http://127.0.0.1:8080/api/v1/materials -H "Authorization: Bearer $PAPER_COLLECT_API_KEY"
curl -X POST http://127.0.0.1:8080/mcp -H "Authorization: Bearer $PAPER_COLLECT_API_KEY" -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"materials_search","arguments":{"subject":"math"}}}'
```

## 版本与扩展约定

不会自动删除网盘文件（只能通过 `storage/replicas/bulk` 的 `delete_remote` 人工删除规则外副本）；不支持在线文档编辑、宏执行和跨请求断点分块合并。新增 Adapter 实现 storage.Adapter，支持移动、删除时另外实现 `storage.Mover`、`storage.Deleter` 并在 Capabilities 中声明；新增资料元数据须同时更新本文和前端 DTO；分层、任务和数据流变更同步 [ARCHITECTURE.md](ARCHITECTURE.md)。主端只有一个实例；从端见下方「节点」一节。

## 节点

以下接口需要 `nodes:manage`（管理员），并提供 MCP 工具 `nodes_list`、`nodes_create`、`nodes_update`、`nodes_metrics`、`cluster_settings_get`、`cluster_settings_update`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/nodes` | 节点列表，主端在前 |
| POST | `/api/v1/nodes` | 创建从端，body `{"name","slots","cache_max_bytes","ocr_provider_ids"}`；响应额外包含只出现一次的 `token` |
| PATCH | `/api/v1/nodes/:id` | 修改 `name`、`enabled`、`draining`、`slots`、`cache_max_bytes`、`ocr_provider_ids`；主端运行参数只能在集群设置中修改，主端不能停用，可以排空 |
| GET | `/api/v1/nodes/:id/metrics` | 最近 1 小时指标，保存在主端内存，重启后清空 |
| GET | `/api/v1/cluster/settings` | 读取主端控制的调度策略 |
| PATCH | `/api/v1/cluster/settings` | 部分更新主端任务类型、并发、租约、心跳、领取等待和空闲轮询 |

节点字段：`id`、`name`、`is_master`、`enabled`、`draining`、`status`（`online`、`stale`、`offline`、`draining`、`disabled`）、`kinds`、`slots`（节点最近上报）、`configured_slots`（主端期望）、`cache_max_bytes`、`ocr_provider_ids`、`config_revision`、`applied_config_revision`、`running`（节点上报）、`running_kinds`（按类型统计的租约任务）、`succeeded_1h`、`failed_1h`、`version`、`protocol`、`last_ip`、`last_seen_at`、`started_at`、`cpu_percent`、`cpu_cores`、`proc_cpu_percent`、`mem_used`、`mem_total`、`proc_rss`、`net_rx_bps`、`net_tx_bps`、`cache_used`、`cache_max`、`created_at`。`cache_max` 是节点上报的实际值，`cache_max_bytes` 是主端配置；`config_revision == applied_config_revision` 表示从端已应用最新配置。

创建从端时的 `slots` 可省略，默认 `{"replicate":2,"analyze":1,"index":1}`；每项 0..64，0 表示不领取该类型。`cache_max_bytes` 可省略，默认 20 GiB；0 表示不限。`ocr_provider_ids` 可省略，数组为空表示不声明额外 OCR 可达性，最多 32 个，每个 ID 必须指向启用的 OCRProvider。节点选择多个服务时，按 OCRProvider 的 `priority` 升序选择。

`ClusterSettings` 字段：`master_kinds:("replicate"|"analyze"|"index")[]`、`master_ingest_concurrency`、`master_replicate_concurrency`、`master_analyze_concurrency`、`master_index_concurrency`、`lease_seconds`、`heartbeat_seconds`、`claim_wait_seconds`、`poll_seconds`、`revision`、`updated_at`。并发 1..64；租约 30..3600 秒；心跳至少 1 秒且小于租约；领取等待 1..25 秒且小于租约；轮询 1..60 秒。PATCH 省略的字段保持不变。每次成功 PATCH 递增全局 `revision` 和所有从端的 `config_revision`；主端调度协程立即按新并发计算容量，从端在下一次心跳取得相同配置。

主端节点名称固定为「主端」；主端任务类型和并发不再读取环境变量。节点列表和集群设置都写入主端数据库，因此重启后保持。只有从端的 `ROLE=worker`、`MASTER_URL`、`NODE_TOKEN` 和本机 `CACHE_DIR` 仍来自环境变量。

网盘的上传路由在网盘编辑表单中配置；节点管理权限的管理员可以把主端或任意从端加入某个网盘的优先级列表。路由只影响 `replicate`，不改变 AI/索引任务的节点类型配置。

任务列表支持 `node_id` 筛选，任务响应包含 `node_id`、`node_name`。

### 节点协议 `/internal/v1`

只接受节点令牌；节点令牌不能访问其他接口，用户令牌和 API Key 也不能访问这组接口。不属于公开 API 和 MCP。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/heartbeat` | 上报指标、`running_job_ids` 和 `config_revision`，续租，返回 `lost_job_ids`、`draining`，以及包含槽位、缓存上限、OCR 服务、心跳、领取等待和修订号的 `config` |
| POST | `/jobs/claim` | 长轮询批量领取（任务输入的 `blob.sources` 列出该节点可直接读取的云端副本及凭据，节点先读副本、失败再回退 `/blobs/:sha`），body `{"capacity":{"replicate"\|"analyze"\|"index":空闲数},"wait_ms","protocol","ocr_provider_ids"}`，每类最多 64，最长 25 秒；返回 `{"jobs":[Input...]}`（每类不超过所给空闲数），无任务返回 204，协议版本不一致返回 409（当前协议 4） |
| POST | `/jobs/:id/progress` | 进度 |
| POST | `/jobs/:id/complete` | 提交结果，可 gzip，上限 32 MiB；幂等，非当前租约返回 409 |
| POST | `/jobs/:id/fail` | `{"error","extra"}` 记录失败，`{"interrupted":true}` 重新排队 |
| GET | `/blobs/:sha` | 下载原件，本地原件支持 Range |
| POST | `/drives/:id/refresh-token` | 回写轮换后的 OneDrive refresh token |
