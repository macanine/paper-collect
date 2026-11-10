# 项目协作约定

- 文档入口见 [docs/README.md](docs/README.md)，项目概览看 [README.md](README.md)，部署与环境变量看 [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)，用户操作看 [docs/USAGE.md](docs/USAGE.md)，接口以 [docs/API.md](docs/API.md) 为准，技术架构与数据流见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，数据库与备份见 [docs/DATA.md](docs/DATA.md)；不保留与当前实现冲突的历史计划文档。
- `backend/internal/server` 负责 HTTP/MCP 与权限，`internal/service` 负责业务，`internal/model`/`db` 负责模型和迁移，`internal/storage` 负责存储适配；前端按 `web/src/features` 组织。
- 资料是一份逻辑文件，Blob 按 SHA-256 去重；合集条目角色读取资料角色。修改接口、状态或字段时同步 API 文档及前端类型。
- 主端只运行一个实例，默认 SQLite 单连接，PostgreSQL 可选。从端（`ROLE=worker`）没有数据库和共享磁盘，通过 `/internal/v1` 领取任务；任务执行代码在 `internal/jobexec`，不得访问数据库。事务内使用传入的 `tx`，不要通过 `s.db` 再取连接。
- 数据库兼容 SQLite/PostgreSQL，跨表状态变更使用事务；数据库与文件系统或云端操作不能视为同一事务。
- 项目采用敏捷开发；模型或字段重构不要求兼容旧数据库，不为旧库编写迁移、数据回填或兼容分支，也不把迁移能力作为评审或交付阻塞项。
- 选择文件后直接上传；单个普通文件不创建合集，文件夹、多文件或 ZIP 自动创建清单。编目（部件、分组、套卷字段、学校与标签名称）只由 AI 决定，不写按文件名或规则猜测的分支；规则只用于校验 AI 回答（原文证据、一致性、固定字典）并推算由其回答必然得出的值。AI 输出是不可信输入；人工修改的字段会锁定，AI 不再改动。不要记录口令、令牌、Cookie、密钥或分享链接，也不要在非授权响应中暴露它们。
- `data/`、`.cache/` 可能含用户文件、数据库或密钥；除任务明确要求外，不查看敏感内容，不清空或覆盖用户数据。测试使用临时目录和临时数据库，外部服务用模拟实现。

## 前端视觉规范（固定）

- 前端固定采用 [Ant Design v6 设计语言](https://ant.design/design.md)，以 `web/src/app/theme.ts` 和 `web/src/styles/global.css` 的主题令牌为准；除任务明确要求整体换肤外，不得改回暖色纸张、深色侧栏、衬线标题或其他自定义视觉体系。
- 使用 Ant Design 标准浅色令牌：主色 `#1677FF`，成功 `#52C41A`，警告 `#FAAD14`，错误 `#FF4D4F`；页面背景 `#F5F5F5`，容器/浮层 `#FFFFFF`，弱背景 `#FAFAFA`；正文 `#1F1F1F`，次级文字 `#595959`，说明文字 `#8C8C8C`，边框 `#D9D9D9`/`#F0F0F0`。
- 字体使用系统 UI 字体栈，正文 14px；界面只使用 400 和 600 字重。控件圆角 6px、卡片/弹层圆角 8px、标签圆角 4px；间距、控件高度和状态反馈优先使用 Ant Design 令牌，不写一次性颜色、圆角、阴影或动画曲线。
- 三个表面层级固定为布局背景、白色容器、白色浮层；以边框和留白建立层级，阴影只用于弹窗、抽屉、下拉等真正悬浮的界面。控件默认高度 32px，卡片内边距 24px，表头使用 `#FAFAFA` 背景和 600 字重。
- 侧栏固定为白色，当前导航使用 `#E6F4FF` 背景和 `#1677FF` 文字/图标；顶栏与侧栏保持白色和 `#F0F0F0` 分隔线。主操作每屏只保留一个实心主色按钮，其余操作使用默认、文本或链接按钮。
- 优先通过 `ConfigProvider`、Ant Design 组件和设计令牌实现视觉，页面 CSS 只处理布局与响应式；登录页、仪表盘、列表、详情、后台管理、网络面板、弹窗和移动端必须保持同一视觉语言。修改界面后运行 `make check`，并检查桌面与移动端。

- 验证按需进行：开发中只跑改动所在包的测试，例如 `(cd backend && go test ./internal/service/)`；提交前在项目根目录运行 `make check`（`go test -short`、`go vet`、前端测试与构建，`build` 已包含类型检查）；`make test-all` 跑全量，CI 也运行全量。只有改动界面时才检查桌面与移动端。
- 测试只覆盖当前行为和安全边界；不为旧库迁移、已删除的接口或文档文字比对编写测试。耗时超过 1 秒的测试加 `testing.Short()` 跳过。
- 仅报告实际执行过的测试；真实云盘、MinerU、模型网关和 PostgreSQL 未测试时明确说明。
