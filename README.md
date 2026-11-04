# 高中资料归档系统

一套面向高中试卷、练习、课件、答案和配套资料的归档系统。支持文件、文件夹与 ZIP 上传，按套卷和专题整理，原件去重并复制到本地或云存储；可选 OCR、AI 编目和题目检索。后端使用 Go + Gin + GORM，前端使用 React + Ant Design。

[快速开始](#快速开始) · [文档导航](#文档导航) · [测试](#测试) · [当前边界](#当前边界)

## 核心能力

- **上传与归档**：选择文件、文件夹或 ZIP 后直接上传；按目录和文件名主干自动聚合为套卷，并推断试题、答案、教师版等部件。
- **去重与多点存储**：原件按 SHA-256 去重，可同时保存本地、WebDAV、OneDrive、S3 等副本，并独立跟踪上传与分享状态。
- **整理与回收**：资料库按学科、年级、类型、届别、地区、机构、标签筛选；专题可重新组合套卷；删除内容进入 30 天回收站。
- **AI 与搜题**：RapidOCR、MinerU 或 PaddleOCR 解析文档，OpenAI 兼容模型生成编目建议；试题文件可建立索引并按题干或公式搜索。
- **管理台与 Agent**：浏览器管理台、REST API 和 MCP 共用权限与业务逻辑，支持角色、API 密钥、任务日志和审计。

## 快速开始

前置条件：Go 1.26+、Node.js 20.19+（20.x）或 22.12+。以下命令从项目根目录运行。

`JWT_SECRET` 和 `MASTER_KEY` 只需生成一次并长期保存。服务不会自动读取 `.env`，本机启动时需要每次提供相同配置。

```bash
export DATA_DIR="$PWD/data"
export ADDR=127.0.0.1:8080
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='<至少 12 个字符的初始口令>'
export JWT_SECRET='<至少 32 字符的随机值>'
export MASTER_KEY='<openssl rand -base64 32 的输出>'

(cd web && npm ci && npm run build)
(cd backend && go run ./cmd/server)
```

打开 <http://127.0.0.1:8080> 并使用初始管理员登录。首次启动会创建管理员、默认存储策略和本地存储端点，因此不配置云盘也能完成上传、归档与下载。

使用 Docker Compose 时可复制 `.env.example`、填写其中的必需值后运行：

```bash
cp .env.example .env
docker compose up -d --build
curl http://127.0.0.1:2033/healthz
```

端口、开发模式、云端部署、环境变量和运行检查见 [部署与运行](docs/DEPLOYMENT.md)。

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [部署与运行](docs/DEPLOYMENT.md) | 本机开发、Docker Compose、GitHub Actions、环境变量与存储配置 |
| [使用指南](docs/USAGE.md) | 上传归档、资料整理、AI 识别、搜题、状态与角色 |
| [技术架构](docs/ARCHITECTURE.md) | 分层、运行拓扑、数据流、事务、安全边界与已知限制 |
| [HTTP API](docs/API.md) | 接口、字段、状态码、错误码与权限合同 |
| [MCP 接入](docs/MCP.md) | 客户端配置、工具、上传下载与排查 |
| [数据与备份](docs/DATA.md) | 表关系、数据目录、备份恢复与增长控制 |
| [文档索引](docs/README.md) | 全部文档及维护约定 |

## 测试

```bash
(cd backend && go test ./... && go vet ./...)
(cd web && npm test && npm run typecheck)
```

需要验证前端产物时再运行 `(cd web && npm run build)`。

## 当前边界

- 智能识别与搜题已覆盖协议和本地模拟测试；真实扫描件质量、长文档成本及上游限流仍需按实际资料评估。
- WebDAV、蓝奏云、OneDrive、S3、MinerU 和 PaddleOCR 未全部与真实服务联调，启用前应在目标环境测试。
- 123 云盘目前只支持连接探测，不支持上传、下载和分享，不应作为可写端点启用。
- 彻底删除会清理本地数据，但云盘适配器没有物理删除接口，远端对象需要到对应服务处理。
- SQLite 只支持单实例；PostgreSQL 多实例必须使用唯一 `WORKER_INSTANCE_ID`，并让所有实例共享同一 `DATA_DIR`。
