# 部署与运行

本文覆盖本机开发、Docker Compose、镜像发布、云端部署和环境变量。项目定位见 [README.md](../README.md)，操作说明见 [USAGE.md](USAGE.md)，备份恢复见 [DATA.md](DATA.md)。

## 1. 运行方式

项目有两种常用运行形态：

- **本机单进程**：默认 SQLite，同一个 Go 进程提供浏览器管理台、REST API、MCP 和后台 worker，适合开发与单机使用。
- **Compose**：PostgreSQL、主端、一次性 schema 服务、自建 RapidOCR 和 Nginx 网关，适合持续运行的部署环境。
- **从端节点**：在其他机器上运行 `ROLE=worker`，分担上传、AI 识别和索引，见 [主从节点](#6-主从节点)。

正式环境至少需要准备 `ADMIN_PASSWORD`、`JWT_SECRET`、`MASTER_KEY` 和数据库密码。`JWT_SECRET` 与 `MASTER_KEY` 必须长期保存；更换 `MASTER_KEY` 后，已有云盘、AI 和 OCR 凭据将无法解密。

## 2. 本机运行

前置条件：

- Go 1.26+，以 `backend/go.mod` 为准。
- Node.js 20.19+（20.x）或 22.12+；CI 使用 Node 22，本机最近一次在 Node 24.22.1 上验证（见 [验证状态](ARCHITECTURE.md#16-验证状态)）。

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

打开 <http://127.0.0.1:8080>。首次启动会创建管理员和一条不上传任何网盘的兜底同步规则。管理员变量只在空库自举时使用，修改它们不会重置已有密码。

Go 服务不会自动加载 `.env`。直接运行 `go run` 时，应在每个新终端中恢复相同的数据目录和密钥。

### 前端开发模式

后端终端按上面的配置启动。另一个终端执行：

```bash
cd web
npm run dev
```

打开 <http://127.0.0.1:5173>。Vite 会把 `/api` 和 `/mcp` 代理到 `127.0.0.1:8080`。

## 3. Docker Compose

Compose 从源码构建应用和 RapidOCR，启动 PostgreSQL、主端和网关。宿主机只暴露一个应用端口；数据库和 OCR 服务只在 Compose 内网监听。

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
docker compose logs -f gateway app rapidocr migrate
```

`paper-data` 卷保存主端的 `DATA_DIR`，包括 CAS、临时文件和 `local` 网盘数据；`paper-postgres` 保存 PostgreSQL；`rapidocr-jobs` 保存 OCR 任务中间文件。

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
| `DATA_DIR` | `./data` | 数据库默认位置、CAS、临时文件和 `local` 网盘 |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` 或 `postgres` |
| `DATABASE_DSN` | `DATA_DIR/paper.db` | PostgreSQL 时必填 |
| `DB_AUTO_MIGRATE` | `true` | 进程启动时是否执行 `AutoMigrate`；Compose 应用实例设为 `false`，由 `migrate` 服务执行 |
| `WEB_DIST` | `../web/dist` | 构建后的前端目录；不存在时只提供 API |
| `ADMIN_USERNAME` | 空 | 仅空库自举时使用；与密码同时设置或同时留空 |
| `ADMIN_PASSWORD` | 空 | 仅空库自举时使用；至少 12 个字符 |
| `JWT_SECRET` | 必填 | 至少 32 个字符 |
| `MASTER_KEY` | 必填 | base64 解码后恰好 32 字节，用于 AES-256-GCM |
| `ROLE` | `master` | `master` 或 `worker`；从端变量见下表 |
| `NODE_NAME` | `主端` | 主端在节点列表中的名称 |
| `WORKER_KINDS` | `replicate,analyze,index` | 主端自己执行的可下放任务；`none` 表示全部交给从端。归档、配套和 `local` 网盘的复制始终由主端执行 |
| `WORKER_INGEST_CONCURRENCY` | `1` | 主端归档任务并发数 |
| `WORKER_REPLICATE_CONCURRENCY` | `2` | 主端副本上传并发数 |
| `WORKER_ANALYZE_CONCURRENCY` | `2` | 主端 AI 识别并发数 |
| `WORKER_INDEX_CONCURRENCY` | `2` | 主端搜题索引并发数 |
| `WORKER_LEASE_DURATION` | `2m` | 任务租约时长，也是从端失联后任务被收回的时间；不能小于 `30s` |
| `WORKER_HEARTBEAT_INTERVAL` | `20s` | 租约续期间隔，必须小于租约时长 |
| `WORKER_POLL_INTERVAL` | `2s` | 没有进程内唤醒时的轮询间隔 |
| `PULL_CACHE_TTL` | `15m` | 云端副本拉回临时目录后的下载有效期 |

并发变量必须是 1 到 64 的整数。配置缺失或非法时进程会拒绝启动，错误只报告变量名，不回显值。

### 从端

从端只需要下表的变量，不需要数据库、`DATA_DIR`、`JWT_SECRET` 或 `MASTER_KEY`。

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ROLE` | 必填 | 固定为 `worker` |
| `MASTER_URL` | 必填 | 主端地址；非回环地址必须是 `https://`，否则拒绝启动 |
| `NODE_TOKEN` | 必填 | 在主端「节点」页面创建，只显示一次 |
| `CACHE_DIR` | `./cache` | 原件缓存目录，一个目录只能给一个从端使用 |
| `CACHE_MAX_BYTES` | `21474836480`（20 GiB） | 缓存上限，`0` 表示不限；删除缓存不影响正确性 |
| `WORKER_SLOTS` | `replicate=2,analyze=1,index=1` | 各类任务的并发数，每项 0 到 64 |
| `RAPIDOCR_URL` | 空 | 从端可访问的 RapidOCR 地址，覆盖主端配置的自建 OCR 地址 |

### Compose

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `APP_PORT` | `2033` | 网关映射到宿主机的端口 |
| `TZ` | `Asia/Shanghai` | Compose 服务时区 |
| `POSTGRES_USER` | `paper` | Compose PostgreSQL 用户 |
| `POSTGRES_DB` | `paper` | Compose PostgreSQL 数据库 |
| `POSTGRES_PASSWORD` | 必填 | Compose PostgreSQL 密码，只放在 `.env` 或密钥管理中 |

## 6. 主从节点

主端负责用户请求、数据库、原件和调度；从端主动连接主端领取任务，可以位于 NAT 之后，主端不需要反向访问从端。只有主端、没有从端时系统照常运行。

### 添加从端

1. 确认主端可通过 HTTPS 访问。任务会携带解密后的网盘和模型密钥，所以从端拒绝通过明文连接非本机主端。Compose 的网关只监听 HTTP，需要在它前面放一层 TLS 反向代理：放行 `/internal/v1/`（不要只放行 `/api`），读取超时不小于 60 秒，请求体上限不小于 32 MiB。
2. 以管理员登录，进入「系统 → 节点 → 添加节点」，保存只显示一次的配置。
3. 在从端机器上启动（镜像与主端相同）：

```bash
MASTER_URL=https://paper.example.com NODE_TOKEN=nd-... \
  docker compose -f deploy/docker-compose.worker.yml up -d
```

不用容器时，直接运行同一个二进制：

```bash
ROLE=worker MASTER_URL=https://paper.example.com NODE_TOKEN=nd-... CACHE_DIR=/var/cache/paper ./paper-collect
```

节点会在几秒内显示为「在线」。主从版本应一起升级：协议版本不一致的从端仍会上报心跳，但领不到任务，节点页会标记「协议不一致」。

### 哪些任务会交给从端

| 任务 | 从端能否执行 | 说明 |
| --- | --- | --- |
| 复制到云盘 | 能 | 从端直连网盘 |
| 复制到 `local` 网盘 | 否 | 目录在主端 `DATA_DIR` 内，始终由主端执行 |
| AI 识别、题目索引 | 能 | 从端直连 OCR 和模型服务，见下 |
| 归档、配套 | 否 | 始终由主端执行 |

从端解析不了主端内网的服务。地址是回环地址或单段主机名（例如 Compose 的 `http://rapidocr:9003`）时，主端不会把依赖它的任务交给该从端，任务留给主端或其他合适的从端。如果 `WORKER_KINDS=none` 且没有合适的从端，这些任务会一直排队。解决办法：

- 自建 OCR：在从端运行 RapidOCR，并设置 `RAPIDOCR_URL`；
- 或者把 OCR 与模型地址配置成从端可访问的地址；
- 或者不在 `WORKER_SLOTS` 中启用 `analyze`、`index`，从端只负责上传。

### 网盘上传节点

网盘的上传节点不需要写入环境变量。管理员在「存储 → 网盘 → 编辑」中为每个远程网盘选择 `自动分配`、`按优先级故障切换` 或 `仅使用指定节点`，然后把主端或从端按优先级加入列表。优先级数字越小越先上传；节点停用、排空或失联后，任务按列表切换。`local` 网盘始终由主端执行。节点列表为空时，系统保持现有的任务池行为。

### 日常操作

- **排空**：不再领取新任务，正在执行的任务继续跑完，适合升级或维护前使用。主端排空时仍处理只能在主端运行的任务。
- **停用**：令牌立即失效，从端在下一次心跳时停止正在执行的任务，这些任务在租约到期后交给其他节点。
- **从端崩溃或断网**：任务在 `WORKER_LEASE_DURATION`（默认 2 分钟）内被收回。旧执行者之后的提交会被拒绝。
- **从端正常退出**（`SIGTERM`）：任务立即回到队列。
- **主端重启**：从端继续执行并重试上报，重连后无需人工处理。

## 7. 存储与运维

原件始终保存在 `DATA_DIR/blobs`，不配置网盘即可完成单机归档。需要网盘副本时，管理员在「存储 → 网盘」中添加并测试本地目录、WebDAV、蓝奏云、OneDrive 或 S3，再在「存储 → 同步规则」中设置哪些文件上传到哪里，预览后保存。规则说明见 [使用指南](USAGE.md#5-网盘同步)。

每个副本独立记录期望路径、实际路径、上传状态、校验级别和分享状态；分享失败不会回滚已上传副本。系统从不自动删除网盘文件。云端文件可按需拉取到 `DATA_DIR/tmp/material-pulls`，下载前会校验字节数和 SHA-256。

数据库与文件系统、云盘操作不是同一事务。备份时需要同时保存数据库和整个 `DATA_DIR`，并单独保管原 `MASTER_KEY`、`JWT_SECRET` 和数据库连接信息。具体步骤见 [备份与恢复](DATA.md#4-备份与恢复)。

OneDrive 和 S3 的字段、权限与分享限制见 [网盘 API](API.md#get-storagedrives--post-storagedrives)，真实云服务上线前应在目标账号和目标网络中完成写入、下载和分享验证。
