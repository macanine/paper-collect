# 数据结构、存储与备份

版本 1.3.0 · 2026-10-28。本文描述当前数据库表、文件目录、备份恢复和增长控制；文档索引见 [README.md](README.md)，运行方式见 [ARCHITECTURE.md](ARCHITECTURE.md)，接口字段见 [API.md](API.md)。

## 1. 数据库与运行形态

- 默认 SQLite（WAL、单连接），可通过 `DATABASE_DRIVER=postgres` 切换 PostgreSQL。两种驱动共用同一套 GORM 模型；列表字段以 JSON 文本保存，SQL 不依赖 JSONB、ILIKE、数据库专用枚举或数组。
- 本机开发默认在启动时执行 GORM `AutoMigrate` 增量建表/加列；Compose 由一次性 `migrate` 服务执行，应用设置 `DB_AUTO_MIGRATE=false`。**没有版本化迁移记录**，`AutoMigrate` 也不会删除或重命名已有表/列/索引。升级前必须先备份。
- SQLite 使用 pragma：`foreign_keys(1)`、`busy_timeout(5000)`、`journal_mode(WAL)`、`_txlock=immediate`；连接池固定 1 个连接，避免多 writer 锁争用。PostgreSQL 默认 10 个打开连接、5 个空闲连接、30 分钟连接寿命。
- GORM 日志级别为 silent，避免 SQL 参数中的密文/哈希进入进程日志。
- 任务使用 `lease_owner`、`lease_until`、`heartbeat_at` 和 `job_locks`。PostgreSQL 多实例通过 `FOR UPDATE SKIP LOCKED` 领取，SQLite 通过 `BEGIN IMMEDIATE` 条件更新领取；SQLite 仍只允许单实例。
- 数据库保存元数据、任务/副本状态、凭据密文和分析/索引结果；原件和本地副本在文件系统。两者不是同一事务，必须一起备份。

## 2. 表清单

所有 ID 是 UUID 字符串（`size:36`）；时间由 GORM 维护；`*_json` 列使用 `model.EncodeStrings`/`EncodeMap` 写入 JSON 文本。除显式唯一索引外，没有数据库外键，关联删除由服务层负责。

| 表 | 模型 | 作用与关键约束 |
| --- | --- | --- |
| `users` | `User` | 账号；`username` 唯一（3..64，字母/数字/下划线/连字符）；`role` 为 admin/editor/viewer；`enabled` 控制登录与 Agent 密钥 |
| `api_keys` | `APIKey` | Agent 密钥；只存随机 secret 的 SHA-256，并为 `secret_hash` 建索引用于直接鉴权；`user_id`、`expires_at`；`scopes_json`；`revoked`/`revoked_at` |
| `sessions` | `Session` | 浏览器会话；`refresh_hash` 唯一；`prev_hash` 记录上一枚 token 用于重放检测；`expires_at`、`revoked` |
| `audits` | `Audit` | 审计元数据；`user_id`、`action`、`target_id`、`created_at`；不含正文、令牌、Cookie、密钥、分享链接 |
| `blobs` | `Blob` | 不可变内容；`sha256` 唯一；大小/媒体类型/原始文件名；一个 Blob 可被多个 Material 引用 |
| `materials` | `Material` | 编目资料；`blob_id`、`sha256`、`role`、`status`、`parent_material_id` 索引；标题/文件名/来源路径/大小等 |
| `material_subjects` | `MaterialSubject` | 资料-学科连接；主键 `(material_id, code)`；`code` 索引；用于列表与搜题筛选 |
| `collections` | `Collection` | 合集；`parent_id` 索引；`subjects_json`；kind 为 series/exam/folder/zip/custom |
| `collection_items` | `CollectionItem` | 资料摆放；唯一索引 `(collection_id, material_id)`；`position`、存储时角色、`relative_path`；读取时角色以资料为准 |
| `uploads` | `Upload` | 上传清单；`user_id`、`status`、`job_id` 索引；标题/类型/学科/年级/学年/标签；完成后记录 `collection_id`/`material_id` |
| `upload_entries` | `UploadEntry` | 清单文件槽位；`upload_id` 索引；`position`、相对路径、声明大小、可选 SHA、媒体类型、`status` |
| `jobs` | `Job` | 持久任务；`kind`、`status`、`lease_owner` 索引；`upload_id`、`replica_id`、`material_id` 索引；租约、心跳、`attempts`、脱敏 `error`、当前阶段及最多 200 条脱敏 `log_json` 记录 |
| `job_locks` | `JobLock` | 同资料 analyze/index 互斥；资源键、任务 ID、租约到期时间 |
| `storage_providers` | `StorageProvider` | 存储端点；`enabled`、`priority` 索引；kind/role/weight/config JSON/加密 secret；kind 不可改 |
| `replicas` | `Replica` | Blob×Provider 副本；唯一索引 `(blob_id, provider_id)`；`status` 索引；verification、分享字段、远端标识、`plan_mode` |
| `storage_policies` | `Policy` | 单例 `id=1`；mode 与 `minimum_replicas` |
| `ai_providers` | `AIProvider` | OpenAI 兼容端点；`enabled`、`priority` 索引；加密 API Key、`models_json`、默认/视觉模型 |
| `ocr_providers` | `OCRProvider` | OCR 服务商；kind 为 `mineru`/`paddleocr`/`rapidocr`，外部服务加密令牌，模型（PaddleOCR 默认 `PP-OCRv6`，RapidOCR 默认 `PP-OCRv5`），启停与优先级 |
| `ai_settings` | `AISettings` | 单例 `id=1`；自动分析开关、当前 `ocr_provider_id`、页数/图片上限；旧 MinerU 字段仅用于兼容迁移 |
| `material_analyses` | `MaterialAnalysis` | 每资料一行（`material_id` 唯一）；status、provider/model、页数、置信度、建议 JSON、应用时间、Markdown 路径、脱敏错误 |
| `search_settings` | `SearchSettings` | 单例 `id=1`；索引开关、页数/批次上限、指定 provider/model |
| `material_texts` | `MaterialText` | 每资料一行；`source_hash`、`parse_key`、页数、逐页文本 JSON、完整 Markdown；供分析与索引复用 |
| `question_blocks` | `QuestionBlock` | 题目；`material_id`、`page_no` 索引；`seq`、题号、纠错后文本、归一化文本、置信度、model/prompt_version |
| `question_terms` | `QuestionTerm` | 可移植倒排；主键 `(term, question_id)`；`term` 索引；`tf` 词频 |
| `material_index_states` | `MaterialIndexState` | 每资料一行；status、job_id、页数、题目数、LLM 调用/失败批次、indexed_at、脱敏错误 |

### 2.1 关系摘要

```
User ──< APIKey
User ──< Session
User ──< Upload ──< UploadEntry
Blob ──< Material >── MaterialSubject
Material ──< MaterialAnalysis
Material ──< MaterialText
Material ──< QuestionBlock ──< QuestionTerm
Material ──< MaterialIndexState
Collection ──< CollectionItem >── Material
Blob ──< Replica >── StorageProvider
Job ──< Upload / Replica / Material（按 kind 关联）
```

- `Material` 与 `Blob` 是多对一：同一份原件可被不同编目资料引用。
- `Replica` 与 `Material` 不直接关联，而是挂在 Blob 上；共享 Blob 的资料看到同一组副本和同一归档状态。
- `CollectionItem` 与 `Material` 是多对多（带 position/relative_path/缓存角色）；同一资料可在多个合集。
- `MaterialAnalysis`、`MaterialText`、`MaterialIndexState` 都是每资料最多一行；重跑覆盖自身。
- Job 的关联是弱引用：ingest 带 `upload_id`，replicate 带 `replica_id`，analyze/index 带 `material_id`；没有数据库外键。

### 2.2 状态与派生字段

- `Material.status` 由 Blob 的副本事实刷新：ready 数 ≥ `minimum_replicas` 为 `archived`；0 ready、0 pending/uploading、至少 1 failed 为 `degraded`；其他不足情况为 `pending_storage`。
- `Material.role` 是唯一角色来源；`CollectionItem.role` 是写入时缓存，读取时被资料角色覆盖；资料角色变化时会同步仍等于旧默认值的条目。
- `Material.title` 与 `file_name` 合一：改名会更新文件名和本地副本名；云盘远端保留上传时名字。
- `Upload.status`：pending → processing → completed/failed；`UploadEntry.status`：pending → received。
- `Job.status`：pending → running → succeeded/failed；failed 可重试回 pending（同 id）；启动时把遗留 running 改回 pending。
- `Replica.status`：pending → uploading → ready/failed；failed 可重试回 pending；分享状态独立为 pending/ready/unsupported/failed。
- `MaterialAnalysis.status` 与 `MaterialIndexState.status`：pending/running/succeeded/failed。
- `material_texts.parse_key` 是 `sha256("ocr-text-v2:" + kind + ":" + base_url + ":" + model)`；与 `source_hash`（资料 SHA-256）一起判断缓存是否可复用。切换服务商、地址或 OCR 模型都会使旧缓存失效；`POST /materials/{id}/index` 的 `force=true` 会清空 `source_hash` 强制重解析。

## 3. 数据目录

默认 `DATA_DIR` 为进程工作目录下的 `./data`（绝对路径由配置解析）：

```
DATA_DIR/
  paper.db                             默认 SQLite 数据库
  paper.db-wal / paper.db-shm          运行中的 WAL 文件（备份时需与 .db 同时刻）
  blobs/<sha[0:2]>/<sha>               原件 CAS，无扩展名
  tmp/stage-*.part                     上传及 ZIP 展开的临时文件
  tmp/upload-archives/*.zip            已接收且尚未处理成功的 ZIP 临时文件
  tmp/docx-*.part                      远端副本读取 DOCX 时的临时文件
  tmp/docx-*/                          预览转换期间的临时 Word 文件和独立配置
  tmp/fontconfig-cache/                 macOS 转换器使用的字体缓存
  previews/docx-v1/<sha前两位>/<sha>.pdf  DOCX 生成的 PDF 预览缓存
  analysis/<materialID>/document.md    识别用 Markdown（数据库存绝对路径）
  providers/<providerID>/...           本地存储端点的副本（按对象 key 分层）
```

Compose 的 RapidOCR 使用独立 `rapidocr-jobs` 卷保存上传原件和 JSONL 中间结果；它不保存页面图片，也不属于业务 `DATA_DIR`。OCR 成功写入 `material_texts` 后，业务侧不再依赖这些中间文件。

- `DATABASE_DSN` 若指向 `DATA_DIR` 外的 SQLite 文件或 PostgreSQL，数据库不在上述默认位置；CAS/临时文件/分析 Markdown/本地副本仍在 `DATA_DIR`。
- CAS 以内容 SHA-256 寻址；相同内容只存一份。`Publish` 用原子 rename 落盘，重复内容丢弃临时文件。
- DOCX 预览是可重建的派生文件，原件最后一个资料引用删除时一并清理；转换异常退出时可能留下临时目录。预览和临时文件也可能包含资料正文，备份和权限应与原件一致。
- 原始上传在 `tmp/` 完成后才发布到 CAS；失败不会标记 entry received，但极端情况下可能留下未被引用的 CAS 文件。
- `analysis/<materialID>/document.md` 每次分析前会删除并重建；数据库中的 `markdown_path` 是绝对路径，移动数据目录后旧分析摘要可能读不到，直到重新识别。
- 本地存储端点根目录固定为 `DATA_DIR/providers/<providerID>`，API 不能指定任意路径。
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
5. 在独立环境定期演练恢复。恢复后旧 `material_analyses.markdown_path` 指向原绝对路径；换目录时摘要可能为空，重新识别即可重建。`material_texts` 仍可按文件 SHA + 解析器地址复用。
6. `AutoMigrate` 只增不删；恢复旧库后启动会补新表/列。不要依赖它清理旧功能留下的表/列/目录。

### 4.3 一致性注意

- 数据库与 CAS/远端对象不是同一事务：普通文件和 ZIP 成员先写 CAS，再在数据库事务中发布资料；进程在两步之间崩溃可能留下孤儿 CAS。复制任务先远端上传、后写副本状态，也可能出现“远端已存在但数据库未记录”的情况。
- 备份前停止所有写入比“热拷贝”更可靠。只恢复数据库而不恢复 CAS，会出现资料元数据在但原件缺失，下载只能尝试云副本；只恢复 CAS 而不恢复数据库，则原件无法被索引/关联。
- 恢复后 worker 会处理 pending 任务和启动补建索引；在确认数据完整前，建议先禁用外部端点或隔离网络。

## 5. 结构演进与兼容

- 默认使用 `db.Migrate` 在启动点调用 `AutoMigrate(model.Models()...)`；Compose 的 `cmd/migrate` 统一执行 schema 和可选 SQLite 导入。测试覆盖空库建表、重复迁移和账号/存储设置保留。
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
| `material_analyses` | 每次重跑覆盖同一行，但 Markdown 文件重建 | 可清理失败/过期分析记录和对应 `analysis/` 目录 |
| `jobs` / `audits` | 每次上传/复制/分析/索引/管理操作 | 可设保留期；清理前考虑失败任务重试和审计合规 |
| `sessions` / `api_keys` | 过期会话、撤销密钥 | 可定期清理过期/撤销记录，但要保留必要的撤销证据 |

优先顺序：

1. **让升级和恢复可控**：先以现有库为基线引入版本化迁移和恢复演练；清查孤儿数据后再考虑给关键关系增加外键（需先处理空字符串/NULL）。
2. **控制数据增长**：为过期任务/审计/解析缓存制定保留期；清理前排除运行中任务和仍被引用的 CAS；不要删除正在使用的 `material_texts`。
3. **可观测性**：为任务失败、解析超时、副本失败和索引重建增加指标/日志采样；日志仍须脱敏。
4. **多实例**：使用 PostgreSQL、唯一 `WORKER_INSTANCE_ID`、共享 `DATA_DIR`，并按实例数配置各任务并发；SQLite 不得多实例。租约过期的任务会被其他实例回收，旧实例的终态写入会被租约栅栏拒绝。

默认本机开发仍使用 SQLite 单连接、单实例 worker。生产 Compose 使用 PostgreSQL 和两个应用实例；不需要额外引入消息队列或向量库。所有实例共享同一数据库与 `DATA_DIR`，任务由租约和资源锁协调。

## 7. 恢复后检查清单

- [ ] `/healthz` 返回 `{"status":"ok"}`。
- [ ] 管理员/普通用户可登录，`/auth/me` 正常。
- [ ] 资料列表、详情、SHA 与大小正确。
- [ ] 至少下载一份原件（本地 CAS 与云端副本各抽一份，如适用）。
- [ ] 任务列表没有异常的 running 状态；必要时等待启动恢复或手动重试 failed。
- [ ] 如使用 AI：`/ai/settings`、`/ai/providers`、`/ocr/providers` 可读且密钥配置状态正确，OCR/模型测试成功（真实联网能力另行确认）。
- [ ] 如使用索引：抽查题目索引和搜题结果；重建失败时旧索引是否仍可查询。
- [ ] 如使用云盘：确认端点健康、副本 ready/失败状态与远端实际文件一致。
- [ ] 备份文件和 `MASTER_KEY` 已分别妥善保存，恢复演练记录已更新。
