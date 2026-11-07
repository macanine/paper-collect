# paper-collect 后端

Go + Gin + GORM；启动和环境变量见 [部署与运行](DEPLOYMENT.md)，技术架构与数据流见 [架构说明](ARCHITECTURE.md)，接口见 [API 合同](API.md)，表关系、备份和优化建议见 [数据说明](DATA.md)。

- `internal/server`：HTTP、MCP、认证和权限；`internal/service`：资料、上传、任务、AI 与检索。
- `internal/model` / `internal/db`：SQLite、PostgreSQL 共用的模型与 `AutoMigrate`；`internal/blob` / `internal/storage`：本地原件及存储适配器。
- `internal/ooxml`：OPC/ZIP 安全校验与关系解析；`internal/docx`：不依赖 LibreOffice 的 Word 正文/表格/图片解析；`internal/office`：仅用于 DOCX→PDF 预览的 LibreOffice 适配与缓存。
- 主端只有一个实例，默认 SQLite 单连接，PostgreSQL 可选；从端不访问数据库，执行逻辑在 `internal/jobexec`。事务内必须使用传入的 `tx`，不要通过 `s.db` 获取另一连接。
- `cmd/migrate` 创建 schema，并可在空 PostgreSQL 中一次性导入旧 SQLite；应用设置 `DB_AUTO_MIGRATE=false` 时不在启动阶段建表。

验证：`go test -short ./... && go vet ./...`，全量去掉 `-short`。集成测试使用临时 SQLite 和模拟上游服务。
