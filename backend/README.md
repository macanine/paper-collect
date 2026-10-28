# paper-collect 后端

Go + Gin + GORM；启动和环境变量见 [根目录 README](../README.md)，技术架构与数据流见 [架构说明](../docs/ARCHITECTURE.md)，接口见 [API 合同](../docs/API.md)，表关系、备份和优化建议见 [数据说明](../docs/DATA.md)。

- `internal/server`：HTTP、MCP、认证和权限；`internal/service`：资料、上传、任务、AI 与检索。
- `internal/model` / `internal/db`：SQLite、PostgreSQL 共用的模型与 `AutoMigrate`；`internal/blob` / `internal/storage`：本地原件及存储适配器。
- 默认为 SQLite 单连接、单进程 worker；不要在事务内通过 `s.db` 获取另一连接。

验证：`go test ./... && go vet ./...`。集成测试使用临时 SQLite 和模拟上游服务。
