# 项目协作约定

- 文档入口见 [docs/README.md](docs/README.md)，用户说明看 [README.md](README.md)，接口以 [docs/API.md](docs/API.md) 为准，技术架构与数据流见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，数据库与备份见 [docs/DATA.md](docs/DATA.md)；不保留与当前实现冲突的历史计划文档。
- `backend/internal/server` 负责 HTTP/MCP 与权限，`internal/service` 负责业务，`internal/model`/`db` 负责模型和迁移，`internal/storage` 负责存储适配；前端按 `web/src/features` 组织。
- 资料是一份逻辑文件，Blob 按 SHA-256 去重；合集条目角色读取资料角色。修改接口、状态或字段时同步 API 文档及前端类型。
- 默认 SQLite 单连接、单实例。PostgreSQL 多实例使用任务租约、心跳、`SKIP LOCKED` 和资料锁；所有实例必须共享 `DATA_DIR`。事务内使用传入的 `tx`，不要通过 `s.db` 再取连接。
- 数据库兼容 SQLite/PostgreSQL，跨表状态变更使用事务；数据库与文件系统或云端操作不能视为同一事务。
- 选择文件后直接上传；单个普通文件不创建合集，文件夹、多文件或 ZIP 自动创建清单。AI 输出是不可信输入，但通过白名单校验的编目字段会在识别成功后自动应用；标题同时成为默认文件名，人工仍可编辑或重新应用。不要记录口令、令牌、Cookie、密钥或分享链接，也不要在非授权响应中暴露它们。
- `data/`、`.cache/` 可能含用户文件、数据库或密钥；除任务明确要求外，不查看敏感内容，不清空或覆盖用户数据。测试使用临时目录和临时数据库，外部服务用模拟实现。

## 前端视觉规范（固定）

- 前端固定采用 [Ant Design v6 设计语言](https://ant.design/design.md)，以 `web/src/app/theme.ts` 和 `web/src/styles/global.css` 的主题令牌为准；除任务明确要求整体换肤外，不得改回暖色纸张、深色侧栏、衬线标题或其他自定义视觉体系。
- 使用 Ant Design 标准浅色令牌：主色 `#1677FF`，成功 `#52C41A`，警告 `#FAAD14`，错误 `#FF4D4F`；页面背景 `#F5F5F5`，容器/浮层 `#FFFFFF`，弱背景 `#FAFAFA`；正文 `#1F1F1F`，次级文字 `#595959`，说明文字 `#8C8C8C`，边框 `#D9D9D9`/`#F0F0F0`。
- 字体使用系统 UI 字体栈，正文 14px；界面只使用 400 和 600 字重。控件圆角 6px、卡片/弹层圆角 8px、标签圆角 4px；间距、控件高度和状态反馈优先使用 Ant Design 令牌，不写一次性颜色、圆角、阴影或动画曲线。
- 三个表面层级固定为布局背景、白色容器、白色浮层；以边框和留白建立层级，阴影只用于弹窗、抽屉、下拉等真正悬浮的界面。控件默认高度 32px，卡片内边距 24px，表头使用 `#FAFAFA` 背景和 600 字重。
- 侧栏固定为白色，当前导航使用 `#E6F4FF` 背景和 `#1677FF` 文字/图标；顶栏与侧栏保持白色和 `#F0F0F0` 分隔线。主操作每屏只保留一个实心主色按钮，其余操作使用默认、文本或链接按钮。
- 优先通过 `ConfigProvider`、Ant Design 组件和设计令牌实现视觉，页面 CSS 只处理布局与响应式；登录页、仪表盘、列表、详情、后台管理、网络面板、弹窗和移动端必须保持同一视觉语言。修改后执行前端测试、类型检查和构建，并检查桌面与移动端。

- 在项目根目录验证：`(cd backend && go test ./... && go vet ./...)`；`(cd web && npm test && npm run typecheck)`。前端产物验证可运行 `(cd web && npm run build)`。
- 仅报告实际执行过的测试；真实云盘、MinerU、模型网关和 PostgreSQL 未测试时明确说明。
