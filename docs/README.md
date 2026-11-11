# 文档索引

文档只描述当前实现和已确认的运行边界，不再保留已被代码、接口或架构取代的历史计划。

| 文档 | 用途 | 主要读者 |
| --- | --- | --- |
| [README.md](../README.md) | 项目定位、核心能力、最短启动、文档导航、测试与能力边界 | 所有读者 |
| [DEPLOYMENT.md](DEPLOYMENT.md) | 本机开发、Docker Compose、镜像发布、云端部署、环境变量与存储配置 | 部署者、运维、开发者 |
| [USAGE.md](USAGE.md) | 上传归档、资料整理、AI 识别、搜题、角色与状态 | 使用者 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 代码地图（§4：后端包与前端目录）、运行时拓扑、上传/存储/AI/检索数据流、事务、安全边界与验证状态 | 后端与前端开发者 |
| [API.md](API.md) | HTTP/MCP 接口、字段、状态、错误码与权限合同 | 前后端、Agent 集成方 |
| [MCP.md](MCP.md) | MCP 密钥、客户端配置、工具与排查 | Agent 与 MCP 客户端集成方 |
| [DATA.md](DATA.md) | 数据库表、数据目录、备份恢复、增长控制与恢复检查 | 部署者、运维、后端开发者 |

维护约定：

- 修改接口、状态或对外字段时，同步更新 `API.md` 和前端类型。
- 修改分层、任务、事务或数据流时，同步更新 `ARCHITECTURE.md`；新增或移动包、目录时同步更新其 §4 代码地图（`service` 内部文件分组写在 `backend/internal/service/doc.go`）。
- 修改模型、表、数据目录或备份方式时，同步更新 `DATA.md`。
- 只记录实际执行过的验证；真实云盘、MinerU、PaddleOCR、模型网关和 PostgreSQL 未测试时明确标注。自建 RapidOCR 曾于 2026-10-28（套卷重构前）在远端 Compose 用真实扫描 PDF 验证 OCR 与分析全链路。验证结论统一记录在 [ARCHITECTURE.md 第 16 节](ARCHITECTURE.md#16-验证状态)。
- 不提交 `data/`、`.cache/`、`web/dist/`、构建二进制、日志或密钥；临时预览入口不作为产品功能保留。
