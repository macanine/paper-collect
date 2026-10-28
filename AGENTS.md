# 项目协作约定

- 文档入口见 [docs/README.md](docs/README.md)，用户说明看 [README.md](README.md)，接口以 [docs/API.md](docs/API.md) 为准，技术架构与数据流见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，数据库与备份见 [docs/DATA.md](docs/DATA.md)；不保留与当前实现冲突的历史计划文档。
- `backend/internal/server` 负责 HTTP/MCP 与权限，`internal/service` 负责业务，`internal/model`/`db` 负责模型和迁移，`internal/storage` 负责存储适配；前端按 `web/src/features` 组织。
- 资料是一份逻辑文件，Blob 按 SHA-256 去重；合集条目角色读取资料角色。修改接口、状态或字段时同步 API 文档及前端类型。
- 默认 SQLite 单连接、单实例。PostgreSQL 多实例使用任务租约、心跳、`SKIP LOCKED` 和资料锁；所有实例必须共享 `DATA_DIR`。事务内使用传入的 `tx`，不要通过 `s.db` 再取连接。
- 数据库兼容 SQLite/PostgreSQL，跨表状态变更使用事务；数据库与文件系统或云端操作不能视为同一事务。
- 选择文件后直接上传；单个普通文件不创建合集，文件夹、多文件或 ZIP 自动创建清单。AI 输出是不可信建议，归档后仍须人工应用。不要记录口令、令牌、Cookie、密钥或分享链接，也不要在非授权响应中暴露它们。
- `data/`、`.cache/` 可能含用户文件、数据库或密钥；除任务明确要求外，不查看敏感内容，不清空或覆盖用户数据。测试使用临时目录和临时数据库，外部服务用模拟实现。
- 在项目根目录验证：`(cd backend && go test ./... && go vet ./...)`；`(cd web && npm test && npm run typecheck)`。前端产物验证可运行 `(cd web && npm run build)`。
- 仅报告实际执行过的测试；真实云盘、MinerU、模型网关和 PostgreSQL 未测试时明确说明。
