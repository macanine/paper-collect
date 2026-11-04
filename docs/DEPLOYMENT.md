# 部署与运行

本文覆盖本机开发、Docker Compose、镜像发布、云端部署和环境变量。项目定位见 [README.md](../README.md)，操作说明见 [USAGE.md](USAGE.md)，备份恢复见 [DATA.md](DATA.md)。

## 1. 运行方式

项目有两种常用运行形态：

- **本机单进程**：默认 SQLite，同一个 Go 进程提供浏览器管理台、REST API、MCP 和后台 worker，适合开发与单机使用。
- **Compose 多实例**：PostgreSQL、两个应用实例、一次性 schema 服务、自建 RapidOCR 和 Nginx 网关，适合持续运行的部署环境。

正式环境至少需要准备 `ADMIN_PASSWORD`、`JWT_SECRET`、`MASTER_KEY` 和数据库密码。`JWT_SECRET` 与 `MASTER_KEY` 必须长期保存；更换 `MASTER_KEY` 后，已有云盘、AI 和 OCR 凭据将无法解密。

## 2. 本机运行

前置条件：

- Go 1.26+，以 `backend/go.mod` 为准。
- Node.js 20.19+（20.x）或 22.12+；项目实际在 Node 22.22.3 上验证。

从项目根目录执行：

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

打开 <http://127.0.0.1:8080>。首次启动会创建管理员、默认存储策略和本地存储端点。管理员变量只在空库自举时使用，修改它们不会重置已有密码。

Go 服务不会自动加载 `.env`。直接运行 `go run` 时，应在每个新终端中恢复相同的数据目录和密钥。

### 前端开发模式

后端终端按上面的配置启动。另一个终端执行：

```bash
cd web
npm run dev
```

打开 <http://127.0.0.1:5173>。Vite 会把 `/api` 和 `/mcp` 代理到 `127.0.0.1:8080`。

## 3. Docker Compose

Compose 从源码构建应用和 RapidOCR，启动 PostgreSQL、两个应用实例和网关。宿主机只暴露一个应用端口；数据库和 OCR 服务只在 Compose 内网监听。

```bash
cp .env.example .env
```

编辑 `.env`，至少设置：

- `ADMIN_PASSWORD`
- `JWT_SECRET`
- `MASTER_KEY`
- `POSTGRES_PASSWORD`

然后启动并检查：

```bash
docker compose up -d --build
curl http://127.0.0.1:2033/healthz
docker compose ps
```

默认访问 <http://127.0.0.1:2033>。可在 `.env` 中修改 `APP_PORT`；容器内部始终使用 `8080`。

查看日志：

```bash
docker compose logs -f gateway app-1 app-2 rapidocr migrate
```

`paper-data` 卷保存共享 `DATA_DIR`，包括 CAS、临时文件和本地存储端点数据；`paper-postgres` 保存 PostgreSQL；`rapidocr-jobs` 保存 OCR 任务中间文件。

如果模型网关位于同一台 Docker 主机但不在项目网络中，可使用内部网络覆盖文件：

```bash
NEW_API_NETWORK=new-api_new-api-network \
  docker compose -f docker-compose.yml -f deploy/docker-compose.new-api.yml up -d --build
```

该覆盖文件只把应用接入指定外部网络，不发布额外端口。

### RapidOCR

首次识别时，RapidOCR 可能下载并加载中文模型。健康检查只表示 HTTP 服务可访问，不代表模型已加载。Compose 内地址为 `http://rapidocr:9003`，无需令牌。

单机资源有限时保持以下默认值，避免 OCR 挤占应用和数据库资源：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `RAPIDOCR_CPUS` | `1.0` | 容器 CPU 上限 |
| `RAPIDOCR_WORKERS` | `1` | OCR 任务并发数 |
| `RAPIDOCR_THREADS` | `1` | 单个推理任务的计算线程数 |

## 4. GitHub Actions 与云端部署

`.github/workflows/ci.yml` 在 Pull Request 和 `main` 上执行后端测试与 `vet`、前端测试、类型检查与构建，并校验本地和生产 Compose。

`.github/workflows/docker.yml` 在 `main`、`v*` 标签和手动触发时构建并推送 GHCR 镜像：

- `ghcr.io/<owner>/<repo>:sha-<commit>`
- `ghcr.io/<owner>/<repo>-rapidocr:sha-<commit>`

手动运行 `Docker Build and Deploy` 并勾选 `deploy` 后，工作流会通过 SSH 上传生产 Compose、网关配置和部署脚本，在远端拉取同一提交的镜像、执行更新并检查网关健康状态；失败时恢复上一版镜像。

首次部署前，在 GitHub `production` Environment 或 Actions Secrets 中配置：

| Secret | 说明 |
| --- | --- |
| `DEPLOY_HOST` | 云端 SSH 地址 |
| `DEPLOY_USER` | 具备 Docker 和 Compose 权限的 SSH 用户 |
| `DEPLOY_PATH` | 云端绝对路径，例如 `/opt/paper-collect` |
| `DEPLOY_SSH_KEY` | SSH 私钥 |
| `DEPLOY_KNOWN_HOSTS` | `ssh-keyscan -H <host>` 的完整输出 |
| `GHCR_USERNAME` | 可读取 GHCR 包的 GitHub 用户名 |
| `GHCR_READ_TOKEN` | 具有 `read:packages` 权限的 PAT |

云端主机需要 Docker Engine、Compose 插件、部署目录和 `.env`：

```bash
sudo install -d -o "$USER" -g "$USER" /opt/paper-collect
cp .env.example /opt/paper-collect/.env
```

编辑 `/opt/paper-collect/.env` 后，在 GitHub Actions 手动运行 `Docker Build and Deploy`。本地 Compose 从源码构建；生产 Compose 使用 GHCR 中的固定镜像标签，不在服务器上编译。

## 5. 环境变量

### 应用

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP 监听地址 |
| `DATA_DIR` | `./data` | 数据库默认位置、CAS、临时文件和本地存储端点 |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DATABASE_DSN` | `DATA_DIR/paper.db` | PostgreSQL 时必填 |
| `DB_AUTO_MIGRATE` | `true` | 进程启动时是否执行 `AutoMigrate`；Compose 应用实例设为 `false`，由 `migrate` 服务执行 |
| `WEB_DIST` | `../web/dist` | 构建后的前端目录；不存在时只提供 API |
| `ADMIN_USERNAME` | 空 | 仅空库自举时使用；与密码同时设置或同时留空 |
| `ADMIN_PASSWORD` | 空 | 仅空库自举时使用；至少 12 个字符 |
| `JWT_SECRET` | 必填 | 至少 32 个字符 |
| `MASTER_KEY` | 必填 | base64 解码后恰好 32 字节，用于 AES-256-GCM |
| `WORKER_INSTANCE_ID` | 运行进程随机生成 | PostgreSQL 多实例必须为每个实例设置唯一值；SQLite 禁止设置 |
| `WORKER_INGEST_CONCURRENCY` | `1` | 每实例归档任务并发数 |
| `WORKER_REPLICATE_CONCURRENCY` | `2` | 每实例副本上传并发数 |
| `WORKER_ANALYZE_CONCURRENCY` | `2` | 每实例 AI 识别并发数 |
| `WORKER_INDEX_CONCURRENCY` | `2` | 每实例搜题索引并发数 |
| `WORKER_LEASE_DURATION` | `2m` | 任务租约时长 |
| `WORKER_HEARTBEAT_INTERVAL` | `20s` | 租约续期间隔，必须小于租约时长 |
| `WORKER_POLL_INTERVAL` | `2s` | 没有进程内唤醒时的轮询间隔 |
| `PULL_CACHE_TTL` | `15m` | 云端副本拉回临时目录后的下载有效期 |

并发变量必须是 1 到 64 的整数。配置缺失或非法时进程会拒绝启动，错误只报告变量名，不回显值。

### Compose

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `APP_PORT` | `2033` | 网关映射到宿主机的端口 |
| `TZ` | `Asia/Shanghai` | Compose 服务时区 |
| `POSTGRES_USER` | `paper` | Compose PostgreSQL 用户 |
| `POSTGRES_DB` | `paper` | Compose PostgreSQL 数据库 |
| `POSTGRES_PASSWORD` | 必填 | Compose PostgreSQL 密码，只放在 `.env` 或密钥管理中 |

## 6. 存储与运维

首次启动会创建本地存储端点，默认足够完成单机归档。管理员可在「存储端点」中添加并测试 WebDAV、蓝奏云、OneDrive 或 S3，再在「副本策略」中选择 `mirror_all`、`primary_backup` 或 `balanced`。

每个副本独立记录上传状态、校验级别和分享状态；分享失败不会回滚已上传副本。云端文件可按需拉取到 `DATA_DIR/tmp/material-pulls`，下载前会校验字节数和 SHA-256。

数据库与文件系统、云盘操作不是同一事务。备份时需要同时保存数据库和整个 `DATA_DIR`，并单独保管原 `MASTER_KEY`、`JWT_SECRET` 和数据库连接信息。具体步骤见 [备份与恢复](DATA.md#4-备份与恢复)。

OneDrive 和 S3 的字段、权限与分享限制见 [存储端点 API](API.md#get-storageproviders-post-storageproviders)，真实云服务上线前应在目标账号和目标网络中完成写入、下载和分享验证。
