# 高中资料归档系统

高中资料（试卷、练习、课件、答案、文件夹与 ZIP 合集）的上传、归档与多点存储系统。后端 Go + Gin + GORM，前端 React + Ant Design + UnoCSS。文档入口见 [docs/README.md](docs/README.md)，技术架构与数据流见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，接口见 [docs/API.md](docs/API.md)，数据结构和备份见 [docs/DATA.md](docs/DATA.md)。

## 目录

```
backend/                 Go 服务（HTTP API、MCP、持久任务 worker）
  cmd/server/            入口
  internal/service/      领域逻辑：鉴权、资料、合集、上传、ZIP 归档、复制、任务
  internal/storage/      存储适配器（local / webdav / lanzou / 123pan）
  internal/server/       Gin 路由、DTO、MCP 适配层与集成测试
web/                     React 管理台（构建后由 Go 进程托管）
docs/ARCHITECTURE.md     分层、运行时拓扑、上传/存储/AI/检索数据流与安全边界
docs/API.md              逐字段接口合同（前后端共同依据）
docs/DATA.md             数据结构、备份与精简优化建议
docs/README.md           当前文档索引与维护约定
```

## 快速启动

前置：Go 1.26+、Node 20.19+（20.x）或 22.12+（本项目在 Go 1.27.2 / Node 22.22.3 上验证）。以下命令从项目根目录运行。

首次部署分别运行两次 `openssl rand -base64 32`，将生成的值作为 `JWT_SECRET` 和 `MASTER_KEY` 保存到安全位置。**已有数据必须继续使用原来的 `MASTER_KEY`**；不要每次启动都重新生成。

```bash
export DATA_DIR="$PWD/data"
export ADDR=127.0.0.1:8080
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD='<替换为至少12个字符的初始口令>'
export JWT_SECRET='<替换为已保存的 JWT_SECRET>'
export MASTER_KEY='<替换为已保存的 MASTER_KEY>'
(cd web && npm ci && npm run build)
(cd backend && go run ./cmd/server)
```

打开 <http://127.0.0.1:8080>，用初始管理员登录。首次启动会创建管理员、默认存储策略和本地存储端点，因此不配置任何云盘也能完整归档。服务不会自动加载 `.env` 文件；每次启动时需提供相同的密钥和数据目录。管理员变量仅在空库自举时使用，修改它们不会重置已有密码。

开发模式（两个终端）：

后端终端先按上面设置环境变量，再从项目根目录执行 `(cd backend && go run ./cmd/server)`。另一个终端执行 `cd web && npm run dev`，打开 <http://127.0.0.1:5173>；Vite 将 `/api` 与 `/mcp` 代理到 `127.0.0.1:8080`。

## 配置

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ADDR` | `:8080` | 监听地址 |
| `DATA_DIR` | `./data` | 相对于服务进程的工作目录；保存数据库、CAS、临时文件与本地端点数据 |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DATABASE_DSN` | `DATA_DIR/paper.db` | PostgreSQL 时必须提供连接串 |
| `WEB_DIST` | `../web/dist` | 前端产物目录；不存在时只提供 API |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | 空 | 仅空库自举管理员；密码至少 12 字符 |
| `JWT_SECRET` | 必填 | 至少 32 字符 |
| `MASTER_KEY` | 必填 | 32 字节 base64，用于 AES-256-GCM 加密存储凭据 |

配置缺失或非法会启动失败。生产环境请使用独立强随机值，并妥善保管 `MASTER_KEY`：丢失后已保存的云盘、模型和 MinerU 凭据无法解密。数据库及文件的备份方法见 [docs/DATA.md](docs/DATA.md)。

## 归档行为

- 上传清单先声明文件（路径、字节数），逐文件 PUT 后校验真实格式、字节数与 SHA-256，再 finalize 触发持久任务。
- ZIP 保留原件，展开成员独立归档并记录原路径；单个 ZIP 形成一个合集，批次中的 ZIP 则建立子合集。GBK/Big5/Shift-JIS 文件名会按整包候选编码打分还原。拒绝路径穿越、加密 ZIP、符号链接、重复路径与解压炸弹。
- 归档时服务端按文件名产生角色建议（教师版、答案、学生卷、练习等），归档后可在资料详情修改；判断不出时为「其他」。
- 存储策略：`mirror_all`（默认，所有启用端点各一份）、`primary_backup`（同样双写，读取主端优先）、`balanced`（主端按权重选最低副本数，另写全部备端）。
- 每个副本独立记录上传状态、校验级别（uploaded/size/sha256）与分享状态。分享失败不会回滚已上传副本。

## 数据模型（简化版）

- **资料 = 一个文件**。资料名就是文件名：修改标题会同步下载文件名和本地副本名；原始内容（SHA）不变。云盘适配器没有改名接口，云端对象保留上传时的名字。
- **合集只负责摆放**。合集条目没有自己的角色，角色永远跟随资料；在资料详情改一次即可。
- **题图不落盘**。AI 识别使用 MinerU 的解析文本；图片只在调用视觉模型时短暂存在于内存，不保存。

## 上传流程

1. 在「上传资料」选择文件、文件夹或 ZIP，选择完成后立即开始上传，无需预检或填写表单。
2. 单个普通文件直接归档；多个文件或文件夹自动生成合集清单；单个 ZIP 保留原件并在同一合集展开成员。文件夹中的文件逐个传输并保留相对路径。
3. 管理台按文件名尽量推断标题和学科，无法判断的学科留空；归档后可编辑。全部文件接收后自动提交归档，传输中关闭页面需要重新选择未完成文件。

ZIP 会保留原件并展开支持的成员；文件夹保留相对路径；文件名乱码会按整包编码自动还原。

## 智能识别（LLM + MinerU）

- 系统管理 →「AI 模型」：添加多个 OpenAI 兼容端点（Base URL、API Key、优先级、启停），「探测模型」会调用 `GET /models` 并填充候选列表；可分别指定默认文本模型与视觉模型，支持最小对话测试。密钥加密保存且不回显。
- 系统管理 →「AI 解析」：开启自动识别，配置 MinerU Base URL、令牌和视觉模型的图片数量限制。
- 资料详情 →「AI 识别」：手动触发、查看状态/置信度/解析文本，确认后「应用全部建议」才会修改资料并写审计。ZIP 容器本身不分析，展开成员各自分析。
- 链路：归档后按设置运行 MinerU 解析 PDF/DOC/DOCX/PPTX → 模型生成建议 → 用户确认。识别与搜题索引可复用同一文件的解析文本；需要视觉图片时会重新解析，图片不落盘。
- 未配置 MinerU 令牌或没有可用模型时，识别任务会明确失败，不影响归档与副本上传。

## 搜题

- 「搜题」入口：输入题干，返回题目、页码、原试卷与合集；同一题多卷按多条结果返回。
- 索引（默认启用）：归档后自动入队 `index` 任务；关闭索引期间归档的资料会在重新启用后由启动补建。MinerU 只取逐页文本，LLM 默认每批 10 页/12000 字（可配置）完成 OCR 纠错与切题并标注页码；长页按段落切块，输出截断时自动对半拆批。题目保存完整题干（含选项/小问），纯文本倒排检索。
- 资料详情「题目索引」可查看页码/题目/置信度；有资料写权限的用户可重建索引。
- 索引失败不影响归档与下载；重建失败时旧索引仍可查，搜索只返回已有题目且资料状态为 `archived`/`degraded` 的记录。

## 鉴权

- 浏览器：用户名口令登录，得到 15 分钟 JWT 与可轮换 refresh token。
- Agent：管理员创建 API Key（仅创建时显示一次），用 `X-API-Key` 换取绑定该密钥的 JWT；调用 REST 或 `/mcp` 时必须同时提交 `Authorization: Bearer <JWT>` 与 `X-API-Key`。撤销密钥或用户后立即失效。
- MCP：`POST /mcp`，无状态 JSON-RPC（initialize / tools/list / tools/call），提供资料、合集、任务与搜题只读工具（`materials_search`、`materials_get`、`questions_search`、`collections_list`、`collections_get`、`jobs_get`）。

## 测试

```bash
(cd backend && go test ./... && go vet ./... && go build ./cmd/server)
(cd web && npm run typecheck && npm test && npm run build)
```

后端集成测试使用临时 SQLite 与模拟上游，覆盖直传单文件/文件夹/ZIP、恶意 ZIP 拒绝、上传→归档→ZIP 展开→合集→本地副本、AI 分析与索引共享解析、MCP 双凭据/撤销、未鉴权 401。前端测试覆盖清单路径/大小/格式、上传默认值与直传载荷、搜题高亮、AI 格式/权限、存储字段表与 API envelope。

## 当前能力边界

- **智能识别**：协议测试覆盖 OpenAI 兼容接口与 MinerU 模拟服务；归档后的识别流程尚未用真实服务重新验证。Word/PPT/扫描件解析质量、限流与费用未系统验证。
- **搜题**：切题与页码准确率、长文档成本、上游限流未用真实试卷系统验证；索引失败不影响归档与下载，重建失败时旧索引仍可查。
- **删除**：资料/合集支持级联删除；会清理目录记录、合集条目、分析文件、题目索引/解析缓存与本地副本，但云盘适配器没有删除接口，远端文件会留在云盘。
- **123 云盘**：因开放平台文档无法访问，只实现 access_token 与健康探测；上传/下载/分享明确返回 `unsupported`（`123pan_upload_unverified` 等），**不要把它作为可上传端点启用**。
- **蓝奏云**：按非官方协议（上传、`task=22` 分享、直链解析）实现并用协议测试覆盖，未对真实服务联调；旧版分享页解析已移除，部分旧链接可能无法下载。单文件上限 100 MiB，默认校验级别为 `uploaded`。
- **WebDAV**：协议测试覆盖，未与真实服务联调；标准不含分享接口，只有配置 `public_base_url` 时才返回公开链接。
- ZIP 内嵌套 ZIP 保留为原件不递归展开；不提供独立本地 OCR、通用全文检索、Office 转换、病毒扫描与云盘物理删除。
- 当前为单进程 worker；多实例部署需先为任务领取增加租约，避免重复执行。
