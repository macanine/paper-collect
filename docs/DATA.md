# 数据结构、存储与备份

版本 1.7.0 · 2026-11-08。本文描述当前数据库表、文件目录、备份恢复和增长控制；文档索引见 [README.md](README.md)，运行方式见 [ARCHITECTURE.md](ARCHITECTURE.md)，接口字段见 [API.md](API.md)。

## 1. 数据库与运行形态

- 默认 SQLite（WAL、单连接），可通过 `DATABASE_DRIVER=postgres` 切换 PostgreSQL。两种驱动共用同一套 GORM 模型；列表字段以 JSON 文本保存，SQL 不依赖 JSONB、ILIKE、数据库专用枚举或数组。
- 本机开发默认在启动时执行 GORM `AutoMigrate` 增量建表/加列；Compose 由一次性 `migrate` 服务执行，应用设置 `DB_AUTO_MIGRATE=false`。**没有版本化迁移记录**，`AutoMigrate` 也不会删除或重命名已有表/列/索引。升级前必须先备份。
- SQLite 使用 pragma：`foreign_keys(1)`、`busy_timeout(5000)`、`journal_mode(WAL)`、`_txlock=immediate`；连接池固定 1 个连接，避免多 writer 锁争用。PostgreSQL 默认 10 个打开连接、5 个空闲连接、30 分钟连接寿命。
- GORM 日志级别为 silent，避免 SQL 参数中的密文/哈希进入进程日志。
- 任务使用 `lease_owner`、`lease_until`、`heartbeat_at` 和 `job_locks`。PostgreSQL 通过 `FOR UPDATE SKIP LOCKED` 领取，SQLite 通过 `BEGIN IMMEDIATE` 条件更新领取；所有领取都在主端完成，从端通过 HTTP 请求主端。
- 数据库保存元数据、任务/副本状态、凭据密文和分析/索引结果；原件和 `local` 网盘副本在文件系统。两者不是同一事务，必须一起备份。

## 2. 表清单

所有 ID 是 UUID 字符串（`size:36`）；时间由 GORM 维护；`*_json` 列使用 `model.EncodeStrings`/`EncodeMap` 写入 JSON 文本。除显式唯一索引外，没有数据库外键，关联删除由服务层负责。

| 表 | 模型 | 作用与关键约束 |
| --- | --- | --- |
| `users` | `User` | 账号；`username` 唯一（3..64，字母/数字/下划线/连字符）；`role` 为 admin/editor/viewer；`enabled` 控制登录与 Agent 密钥 |
| `api_keys` | `APIKey` | Agent 密钥；只存随机 secret 的 SHA-256，并为 `secret_hash` 建索引用于直接鉴权；`user_id`、`expires_at`；`scopes_json`；`revoked`/`revoked_at` |
| `sessions` | `Session` | 浏览器会话；`refresh_hash` 唯一；`prev_hash` 记录上一枚 token 用于重放检测；`expires_at`、`revoked` |
| `audits` | `Audit` | 审计元数据；`user_id`、`action`、`target_id`、`created_at`；不含正文、令牌、Cookie、密钥、分享链接 |
| `blobs` | `Blob` | 不可变内容；`sha256` 唯一；大小/媒体类型/原始文件名；一份内容对应一个 Blob |
| `packs` | `Pack` | 套卷（浏览单位）；`doc_type`/`grade`/`cohort`/`region`/`organization_id` 索引；`locked_json`（人工锁定的编目字段）、`revision`（成员变化计数，过期的 AI 编目被丢弃）、`deleted_at`（回收站）；编目字段与 JSON 列表；`organization` 为名称词典规范名称 |
| `materials` | `Material` | 单个文件（一份 SHA-256）；`pack_id`（分组前为空）、`part`、`stage`、`ai_step`、`batch_id`、`status`、`sync_status` 索引；`sync_planned_at`；`sha256` 唯一；`attention_json`（需关注原因）、`part_locked`；`deleted_at` 随套卷进回收站 |
| `material_sources` | `MaterialSource` | 每次到达记录；`material_id`、`upload_id` 索引；`path`、`duplicate`、`user_id`；同一内容重复上传只追加此行 |
| `pack_subjects` | `PackSubject` | 套卷-学科连接；主键 `(pack_id, code)`；`code` 索引；用于列表与搜题筛选 |
| `collections` | `Collection` | 专题；`title`/`description`；只收纳套卷，无父子层级 |
| `collection_items` | `CollectionItem` | 专题收录；唯一索引 `(collection_id, pack_id)`；`position` 显式排序 |
| `uploads` | `Upload` | 上传批次；`user_id`、`status`、`job_id` 索引；`title`、`hints_json`、`new_count`/`duplicate_count`、`skipped_json` |
| `upload_entries` | `UploadEntry` | 清单文件槽位；`upload_id` 索引；`position`、相对路径、声明大小、可选 SHA、媒体类型、`status`（pending/received/existing） |
| `jobs` | `Job` | 持久任务；`kind`、`status`、`lease_owner`、`next_attempt_at` 索引；`upload_id`、`replica_id`、`material_id`、`pack_id`、`batch_id`、`entity_id` 索引；`payload_json`（准备时的分组文件或套卷版本）；租约、心跳、`attempts`、脱敏 `error`、当前阶段及最多 200 条脱敏 `log_json` 记录 |
| `job_locks` | `JobLock` | 资源互斥：同资料 analyze/index、同套卷 decide、全局 group、全局 entity；资源键、任务 ID、租约到期时间 |
| `storage_drives` | `Drive` | 网盘；`enabled`、`read_order`、`route_mode` 索引；kind、`root_path`、`max_file_bytes`、`allowed_exts_json`、`on_path_change`、上传路由策略、config JSON、加密 secret；kind 不可改 |
| `drive_node_routes` | `DriveNodeRoute` | 网盘上传节点；唯一索引 `(drive_id,node_id)`；`priority`、`enabled`；优先级越小越先领取，主端也可作为路由节点 |
| `sync_rules` | `Rule` | 有序同步规则；`position`、`enabled` 索引；`is_default` 标记兜底规则；`conditions_json` |
| `sync_rule_targets` | `RuleTarget` | 规则目标；唯一索引 `(rule_id, drive_id)`；`path_template`、`share`、`required`、`position` |
| `replicas` | `Replica` | Blob×Drive 副本；唯一索引 `(blob_id, drive_id)`；`status`、`rule_id` 索引；`desired_path`/`remote_path`/`remote_id`、`skip_reason`、`share`/`required`、verification、分享字段 |
| `ai_providers` | `AIProvider` | OpenAI 兼容端点；`enabled`、`priority` 索引；加密 API Key、`models_json`、默认/视觉模型 |
| `ocr_providers` | `OCRProvider` | OCR 服务商；kind 为 `mineru`/`paddleocr`/`rapidocr`，外部服务加密令牌，模型字段（PaddleOCR 默认 `PP-OCRv6`，RapidOCR 字段默认 `PP-OCRv5`，但自建服务固定使用 RapidOCR 3.10 默认 PP-OCRv6 small），启停与优先级 |
| `cluster_settings` | `ClusterSettings` | 单例 `id=default`；主端任务类型与四类并发、租约/心跳/领取/轮询参数和调度修订号 |
| `nodes` | `Node` | 主端与从端运行状态；从端另存主端期望槽位、缓存上限、OCRProvider ID、配置修订和已应用修订，节点令牌只存哈希 |
| `ai_settings` | `AISettings` | 单例 `id=1`；当前 `ocr_provider_id`、页数/图片上限（编目始终由 AI 完成，没有开关） |
| `file_cards` | `FileCard` | 每资料一行；AI 阅读记录：status、provider/model、校验后的 card JSON、校验问题 JSON、是否仅凭文件名、页数、脱敏错误 |
| `pack_decisions` | `PackDecision` | 每套卷一行；最近一次 AI 编目：status、对应 revision、provider/model、校验后的决定 JSON、校验问题 JSON、决定时间、脱敏错误 |
| `entities` | `Entity` | 名称词典条目；`kind`（organization/tag）索引；规范名称、地区、来源（ai/human）、是否锁定 |
| `entity_aliases` | `EntityAlias` | 名称写法；主键 `(kind, norm)`（归一化后的写法）、`entity_id` 索引、原始写法；一种写法只属于一个条目 |
| `search_settings` | `SearchSettings` | 单例 `id=1`；索引开关、页数/批次上限、指定 provider/model |
| `material_texts` | `MaterialText` | 每资料一行；`source_hash`、`parse_key`、页数、逐页文本 JSON、完整 Markdown；供分析与索引复用 |
| `question_blocks` | `QuestionBlock` | 题目；`material_id`、`page_no` 索引；`seq`、题号、保留公式的题干、普通文本归一化、`math_index_version`、置信度、model/prompt_version |
| `question_terms` | `QuestionTerm` | 可移植倒排；主键 `(term, question_id)`；`term` 索引；`tf` 词频；`m:` 前缀用于公式词项 |
| `material_index_states` | `MaterialIndexState` | 每资料一行；status、job_id、页数、题目数、LLM 调用/失败批次、indexed_at、脱敏错误 |

### 2.1 关系摘要

```
User ──< APIKey
User ──< Session
User ──< Upload ──< UploadEntry
Blob ── Material（一份 SHA 对应一个文件）
Pack ──< Material ──< MaterialSource
Pack ──< PackSubject
Pack ──< CollectionItem >── Collection
Material ──< FileCard
Pack ──< PackDecision
Entity ──< EntityAlias
Entity ──< Pack（organization_id；标签按名称写入 tags_json）
Material ──< MaterialText
Material ──< QuestionBlock ──< QuestionTerm
Material ──< MaterialIndexState
Blob ──< Replica >── Drive
Rule ──< RuleTarget >── Drive
Drive ──< DriveNodeRoute >── Node
ClusterSettings（单例）── 调度 ── Node
Job ──< Upload / Replica / Material（按 kind 关联）
```

- `Material` 与 `Blob` 一对一：`materials.sha256` 唯一，同一内容只建一份文件；再次上传只写 `MaterialSource`，`duplicate=true`。
- `Material` 属于唯一 `Pack`；套卷持有编目字段，文件继承套卷的学科/年级/届别等，只有 `part`、`answer_start_page` 和 `position` 是文件级字段。
- `Replica` 与 `Material` 不直接关联，而是挂在 Blob 上；资料 `sync_status` 按 Blob 的副本事实刷新。`Replica.rule_id` 记录当前需要它的规则，规则外副本为空。
- `CollectionItem` 是套卷与专题的多对多（带 position）；一个套卷可在多个专题，专题不嵌套。
- `FileCard`、`MaterialText`、`MaterialIndexState` 都是每文件最多一行，`PackDecision` 每套卷最多一行；重跑覆盖自身。
- Job 的关联是弱引用：ingest 带 `upload_id`，replicate 带 `replica_id`，analyze/index 带 `material_id`，group 带 `batch_id`，decide 带 `pack_id`，entity 带 `entity_id`；没有数据库外键。

### 2.2 状态与派生字段

- `Material.status` 只反映原件：本机 CAS 中存在为 `archived`，缺失为 `missing`。
- `Material.sync_status` 反映网盘同步：未生成计划前为 `waiting_catalog`；之后忽略 skipped/orphaned 副本，没有其他副本为 `not_required`，必需目标全部 ready 为 `synced`，有副本 pending/uploading/moving 为 `syncing`，必需目标失败、必需目标分享失败或已写入副本路径偏离为 `attention`。`sync_planned_at` 记录最近一次生成计划的时间。
- `Material.stage` 是整理阶段：processing（AI 整理中，`ai_step` 为 read/group/decide）、waiting_ai（等待模型或 OCR 配置）、attention（需关注）、ready（已入库）；`attention_json` 记录 ai_failed、group_unverified、metadata_only 或关键校验代码。新文件 `pack_id` 为空，分组后才归入套卷。套卷 stage 取成员中最靠后的阶段。
- `Material.part` 是文件在套卷中的部件：questions、answers、full、teacher、answer_sheet、listening、main、other；`answer_start_page` 记录同一文件内答案起始页。
- `Material.title` 与 `file_name` 由「套卷标题 + part 中文标签」派生；套卷 `locked_json` 含 `title` 时标题固定不变。人工修改的编目字段加入 `locked_json`、人工修改的部件置 `part_locked`，AI 编目不会改动它们。改名后重新计算副本 `desired_path`，`on_path_change=move` 的网盘随之移动。
- `Upload.status`：pending → processing → completed/failed；`UploadEntry.status`：pending → received（或 existing 秒传）→ 归档后写入 `results`。
- `Job.status`：pending → running → succeeded/failed；暂时性失败按退避写入 `next_attempt_at` 并回到 pending（最多 6 次），AI 任务因缺少配置失败时回到 pending 且不计次数；租约多次过期的任务停止重试；failed 可手动重试回 pending（同 id，次数归零）；启动时把遗留 running 改回 pending。
- `Replica.status`：pending → uploading（首次上传）或 moving（已写入、路径变化）→ ready/failed；skipped 表示网盘停用、超限或扩展名不允许；orphaned 表示不再被规则需要但远端文件仍在。分享状态独立为空/pending/ready/unsupported/failed。
- `sync_rules` 只有一行 `is_default=true` 的兜底规则；空库启动时创建 ID 为 `default`、没有目标的「其他所有文件」。
- `Pack.deleted_at` 非空即回收站；同一事务把成员 `materials.deleted_at` 置为非空。30 天后 `PurgeExpiredTrash` 彻底删除，`packs_purge` 只能处理回收站中的套卷。
- `FileCard.status`、`PackDecision.status`：pending/running/succeeded/failed；`MaterialIndexState.status`：pending/running/succeeded/failed/not_applicable。
- `material_texts.parse_key` 是 `sha256("ocr-text-v2:" + kind + ":" + base_url + ":" + model)`；与 `source_hash`（资料 SHA-256）一起判断缓存是否可复用。切换服务商、地址或 OCR 模型都会使旧缓存失效；`POST /materials/{id}/index` 的 `force=true` 会清空 `source_hash` 强制重解析。
- RapidOCR 的 DOCX 解析键额外包含 `docx-native-v2`，保存常见公式结构。已有题目的 `math_index_version=0` 表示尚未补建公式词项；后台按批次事务补建为 1，公式搜索会先完成剩余补建。该操作不修改题干、不调用外部服务；旧题干已丢失的结构需从原件重建索引才能恢复。

## 3. 数据目录

默认 `DATA_DIR` 为进程工作目录下的 `./data`（绝对路径由配置解析）：

```
DATA_DIR/
  paper.db                             默认 SQLite 数据库
  paper.db-wal / paper.db-shm          运行中的 WAL 文件（备份时需与 .db 同时刻）
  blobs/<sha[0:2]>/<sha>               原件 CAS，无扩展名
  tmp/stage-*.part                     上传及 ZIP 展开的临时文件
  tmp/upload-archives/*.zip            已接收且尚未处理成功的 ZIP 临时文件
  tmp/material-pulls/<sha256>          按需从云端拉回的临时原件（默认保留 15m）
  tmp/docx-*.part                      远端副本读取 DOCX 时的临时文件
  tmp/docx-*/                          预览转换期间的临时 Word 文件和独立配置
  tmp/fontconfig-cache/                 macOS 转换器使用的字体缓存
  renders/office-v1/<sha前两位>/<sha>.pdf  DOCX 生成的 PDF 预览缓存
  drives/<driveID>/...                 `local` 网盘的副本（按路径模板分层）
```

Compose 的 RapidOCR 使用独立 `rapidocr-jobs` 卷保存上传原件和 JSONL 中间结果；它不保存页面图片，也不属于业务 `DATA_DIR`。OCR 成功写入 `material_texts` 后，业务侧不再依赖这些中间文件。

- `DATABASE_DSN` 若指向 `DATA_DIR` 外的 SQLite 文件或 PostgreSQL，数据库不在上述默认位置；CAS/临时文件/分析 Markdown/`local` 网盘副本仍在 `DATA_DIR`。
- CAS 以内容 SHA-256 寻址；相同内容只存一份。`Publish` 用原子 rename 落盘，重复内容丢弃临时文件。
- `tmp/material-pulls/<sha256>` 是按需拉取缓存，不是 CAS。写入前校验资料声明的大小和 SHA-256，过期后由读取或后续拉取机会性清理；保留期由 `PULL_CACHE_TTL` 控制，不参与资料归档或副本状态。
- DOCX 预览是可重建的派生文件，原件最后一个资料引用删除时一并清理；转换异常退出时可能留下临时目录。预览和临时文件也可能包含资料正文，备份和权限应与原件一致。
- 原始上传在 `tmp/` 完成后才发布到 CAS；失败不会标记 entry received，但极端情况下可能留下未被引用的 CAS 文件。
- `local` 网盘根目录固定为 `DATA_DIR/drives/<driveID>`，API 不能指定任意路径；系统不再默认创建本地网盘。
- 数据库还保存凭据密文（云盘、AI、OCR）和哈希（密码、refresh、API Key secret）；`MASTER_KEY` 不存入数据库。

## 4. 备份与恢复

### 4.1 备份

1. **停止服务并等待进程退出**，同时停止任何其他写同一数据库/数据目录的进程（包括手工导入、脚本和另一个 worker）。升级结构前先完成备份。
2. **完整复制 `DATA_DIR`**。SQLite 默认数据库在其中；若 `DATABASE_DSN` 指向目录外，还须一并复制该数据库。不要在服务运行中只复制 `.db` 文件：WAL 模式下还必须复制同一时点的 `-wal`/`-shm`，或在停止服务后复制。
3. **PostgreSQL**：停止写入后执行 `pg_dump` 备份数据库，再复制同一时点的 `DATA_DIR`。数据库和文件快照必须来自同一时点。
4. **单独安全保存**：原 `MASTER_KEY`、`JWT_SECRET`、`DATABASE_DSN`、管理员自举配置（如需）。不要把这些值放进代码库或普通文档。`MASTER_KEY` 丢失后，已保存的云盘、AI 和 OCR 凭据无法解密；`JWT_SECRET` 改变会使现有 access JWT 失效，未撤销的 refresh token 仍可换取新浏览器 JWT，直接使用的 API 密钥不受影响。
5. 记录当前版本/提交和升级前的数据库文件副本。云盘上的副本不属于本地备份；本地 CAS 是重新复制和下载资料所需的内容，必须保留。新上传的 ZIP 在处理成功后清理临时文件，历史数据不会自动清除；未完成任务依赖的临时 ZIP 也需要备份。

### 4.2 恢复

1. 停止所有应用实例，将数据库和 `DATA_DIR` 恢复到原路径；PostgreSQL 用 `pg_restore`/`psql` 恢复。
2. 使用**原来的** `MASTER_KEY`（以及匹配的 `JWT_SECRET`）启动；恢复后管理员/用户/密钥不需要重建。
3. 检查 `/healthz`、登录、资料详情、一次原件下载；如使用 AI，抽查解析文本、模型端点和凭据是否可用。
4. 如使用云盘，先在隔离网络或临时端点验证副本状态，避免 worker 把未完成任务重试到生产云盘。
5. 在独立环境定期演练恢复。`material_texts` 仍可按文件 SHA + 解析器地址复用。
6. `AutoMigrate` 只增不删；恢复旧库后启动会补新表/列。不要依赖它清理旧功能留下的表/列/目录。

### 4.3 一致性注意

- 数据库与 CAS/远端对象不是同一事务：普通文件和 ZIP 成员先写 CAS，再在数据库事务中发布资料；进程在两步之间崩溃可能留下孤儿 CAS。复制任务先远端上传、后写副本状态，也可能出现“远端已存在但数据库未记录”的情况；适配器上传不覆盖，重试时会接管同一路径下内容相同的对象。
- 备份前停止所有写入比“热拷贝”更可靠。只恢复数据库而不恢复 CAS，会出现资料元数据在但原件缺失，下载只能尝试云副本；只恢复 CAS 而不恢复数据库，则原件无法被索引/关联。
- 恢复后 worker 会处理 pending 任务和启动补建索引；在确认数据完整前，建议先禁用外部端点或隔离网络。

## 5. 结构演进与兼容

- 默认使用 `db.Migrate` 在启动点调用 `AutoMigrate(model.Models()...)`；Compose 的 `cmd/migrate` 统一执行 schema 和可选 SQLite 导入。测试覆盖空库建表与重复迁移幂等。按项目约定，模型重构不兼容旧库：旧库中的 `storage_providers`、`storage_policies` 等表和旧列不会被迁移或清理，旧副本也不会转换为新模型，不保证旧库可以直接升级。
- 新增字段/表必须同时更新 `internal/model`、[API.md](API.md) 和前端 `shared/api/types.ts`（如对外暴露）。
- 已移除功能（例如上传前 AI 预检）的旧表/旧列不会被 `AutoMigrate` 删除；新库不会创建这些旧结构，但旧库升级后可能残留。需要清理时先备份并单独评估。
- 跨 SQLite/PostgreSQL 的 SQL 约束：不使用数据库专用类型/函数；JSON 存文本；`LIKE` 使用 `ESCAPE '!'`；分页/排序由 ORM 生成；唯一性由显式索引保证。
- 没有数据库外键，删除/级联由服务层在事务中完成；新增关系时需同时考虑孤儿数据和 SQLite/PostgreSQL 行为差异。

## 6. 增长控制与优化

当前没有自动保留期/GC，以下数据会持续增长：

| 数据 | 增长源 | 建议/现状 |
| --- | --- | --- |
| `blobs/` 孤儿 | 上传 CAS 发布后 DB 事务失败、ZIP 展开失败、已删除资料未清理干净 | 没有自动 GC；先做引用扫描，再在停机窗口清理，保留正在使用的 SHA |
| `material_texts` | 每份资料的完整 Markdown + 逐页文本，最多数 MB/份 | 分析与索引共享；可设保留期或压缩/外置，删除前确认无进行中的任务 |
| `question_terms` / `question_blocks` | 每道题每个词项/题目行 | 重建索引会替换旧行；删除资料会清理；可定期统计热点与碎片 |
| `file_cards` / `pack_decisions` | 每次重跑覆盖同一行 | 无需清理；不保存模型原文和页面图 |
| `jobs` / `audits` | 每次上传/复制/分析/索引/管理操作 | 可设保留期；清理前考虑失败任务重试和审计合规 |
| `sessions` / `api_keys` | 过期会话、撤销密钥 | 可定期清理过期/撤销记录，但要保留必要的撤销证据 |

优先顺序：

1. **让升级和恢复可控**：先以现有库为基线引入版本化迁移和恢复演练；清查孤儿数据后再考虑给关键关系增加外键（需先处理空字符串/NULL）。
2. **控制数据增长**：为过期任务/审计/解析缓存制定保留期；清理前排除运行中任务和仍被引用的 CAS；不要删除正在使用的 `material_texts`。
3. **可观测性**：为任务失败、解析超时、副本失败和索引重建增加指标/日志采样；日志仍须脱敏。
4. **主从节点**：主端只有一个实例，SQLite 和 PostgreSQL 都可以带多个从端；从端没有数据库，也不共享 `DATA_DIR`，备份只需要覆盖主端。`nodes` 表保存节点令牌哈希和最新指标。

默认本机开发使用 SQLite 单连接。生产 Compose 使用 PostgreSQL 和一个主端；不需要额外引入消息队列或向量库。任务由租约和资源锁协调。

## 7. 恢复后检查清单

- [ ] `/healthz` 返回 `{"status":"ok"}`。
- [ ] 管理员/普通用户可登录，`/auth/me` 正常。
- [ ] 资料列表、详情、SHA 与大小正确。
- [ ] 至少下载一份原件（本地 CAS 与云端副本各抽一份，如适用）。
- [ ] 任务列表没有异常的 running 状态；必要时等待启动恢复或手动重试 failed。
- [ ] 如使用 AI：`/ai/settings`、`/ai/providers`、`/ocr/providers` 可读且密钥配置状态正确，OCR/模型测试成功（真实联网能力另行确认）。
- [ ] 如使用索引：抽查题目索引和搜题结果；重建失败时旧索引是否仍可查询。
- [ ] 如使用网盘：确认网盘健康、同步规则正确，副本 ready/失败/规则外状态与远端实际文件一致。
- [ ] 备份文件和 `MASTER_KEY` 已分别妥善保存，恢复演练记录已更新。
