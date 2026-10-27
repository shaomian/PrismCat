# PrismCat 开发说明

## 项目背景

- 这是用户个人维护的仓库：`https://github.com/shaomian/PrismCat`，基于上游 PrismCat 独立维护，保留 MIT License 和来源说明。
- 项目此前使用 OpenCode 开发，后续在 Codex 中开发维护。仓库外的历史对话和未提交代码不在本地上下文中，不推测其中的决定。
- 默认使用简体中文沟通，沿用现有代码命名与注释风格。
- 产品是自托管的 LLM API 透明代理与调试控制台，支持请求日志、SSE、WebSocket、Trace、重放、请求覆盖和用量提取。

## 代码入口

- `cmd/prismcat/`：启动、配置初始化、数据清理及 Windows 平台入口。
- `internal/server/`、`internal/api/`、`internal/auth/`：路由、控制台 API、认证和静态资源。
- `internal/proxy/`、`internal/outbound/`、`internal/requestoverride/`：代理转发、出站连接、请求覆盖。
- `internal/storage/`：SQLite、异步写入及大 body 的文件存储。
- `internal/trace/`、`internal/timeline/`、`internal/usage/`、`internal/analysis/`：Trace、时间线、用量与 AI 分析。
- `internal/config/`、`config.example.yaml`：配置结构、默认值与示例。
- `web/`：React 19、TypeScript、Vite、Tailwind CSS 4 前端；页面在 `src/pages/`，组件在 `src/components/`。
- 产品说明见 `README_CN.md` 和 `README.md`；请求覆盖规则见 `REQUEST_OVERRIDES_CN.md` 和 `REQUEST_OVERRIDES.md`。

## 环境与常用命令

Go 版本以 `go.mod` 为准（当前最低 1.25.7）；发布工作流和 Docker 使用 Node.js 24。前端使用 npm 和已提交的 `web/package-lock.json`。

后端命令在仓库根目录执行：

```sh
go mod download
go test ./...
go vet ./...
go build -o prismcat ./cmd/prismcat
go run ./cmd/prismcat
```

前端命令在 `web/` 执行：

```sh
npm ci
npm test
npm run lint
npm run build
npm run dev
```

- 后端默认监听 `127.0.0.1:8711`，健康检查为 `/healthz`；首次启动自动创建 `data/config.yaml`。
- 前端开发服务器将 `/api` 代理到 `http://localhost:8711`，联调时需同时启动后端。
- 本地后端在没有嵌入 UI 时可从根目录的 `web/dist` 加载前端；先运行前端构建即可使用该回退方式。
- 单文件分发需要先构建前端，将 `web/dist/` 内容复制到 `internal/server/ui/`，再编译 Go。更新嵌入资源时保留已跟踪的 `internal/server/ui/README.md`，它保证干净克隆时 `go:embed` 仍可编译。
- 发布流程以 `.github/workflows/manual-binary-release.yml` 和 `Dockerfile` 为准；Go 模块路径为 `github.com/shaomian/PrismCat`，发布版本通过 `internal/config.Version` 注入。

## 修改约定

- 任务复盘遵循“把清晰留给用户”：按选任务、选观测点、看结果引导；模板支持任意场景。AI 只在用户点击时调用，候选观测点由用户确认；列表分页、正文按需读取，避免逐流式片段重绘长报告。
- 对比实验保持简洁，默认只展示耗时、Token、请求数和错误数；不主动添加人工验收、评分或复杂指标，后续按用户明确需求扩展。
- 保持默认透明代理行为。请求覆盖必须保留总开关、上游绑定和规则开关三层启用条件；涉及规则编写时使用 `.agents/skills/prismcat-request-overrides/SKILL.md`。
- 修改代理时关注 SSE 的 flush、取消、超时、WebSocket、敏感头脱敏及并发请求行为，补充能复现问题的相关测试。
- 修改配置或存储格式时保持已有配置、SQLite 数据和分离 body 的兼容性；同步更新相关配置示例及用户文档。
- UI 文案同步维护 `web/src/locales/zh.json` 和 `web/src/locales/en.json`；使用翻译 key，不给 `t()` 传中文兜底。
- UI 颜色沿用 `web/src/index.css` 的语义 token，遵守现有 ESLint 规则，避免直接使用 Tailwind 调色板色阶类。
- Go 修改使用 `gofmt`，运行相关包测试；涉及跨包行为时运行 `go test ./...`，按需运行 `go vet ./...`。前端逻辑修改运行 `npm test`，前端代码修改运行 lint 与 build。纯文档修改检查差异与命令准确性即可。
- 遇到已有失败时区分基线问题与本次改动；缺少工具或依赖时明确报告未完成的验证。
- `data/`、本地配置、凭据、数据库、日志、构建产物和 `node_modules/` 不提交。`docs/` 当前被 `.gitignore` 忽略，新增需跟踪的开发文档优先放根目录。
