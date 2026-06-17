# Changez — Agent 快速参考

## 项目简介
代码变更版本化服务器。AI 编程工具（opencode、claudecode、cursor）通过 hook 提交文件快照。服务器使用内容寻址 blob + delta 压缩存储文件版本，提供 REST API + React SPA 浏览界面和 MCP 服务器。

## 架构概览
```
cmd/changez/main.go          ← 入口：组装 DB、存储、HTTP、compactor
internal/db/                 ← SQLite (modernc.org/sqlite) — 项目、文件、版本、blob、delta
internal/storage/            ← 文件系统 blob 存储 + delta 存储（SHA256 内容寻址）
internal/compact/            ← 后台压缩器：合并长 delta 链为 blob 检查点
internal/handler/            ← HTTP 处理逻辑（快照、列表、日志、diff、恢复、项目）
internal/router/             ← HTTP 路由：/api/*、/mcp/*、SPA 回退、认证中间件
internal/mcp/                ← MCP 协议处理器（JSON-RPC over SSE）
internal/config/             ← YAML 配置加载器
web/                         ← React + Vite SPA（通过 //go:embed dist/* 嵌入 Go 二进制）
client/claudecode/           ← 外部 AI 工具的 hook 脚本（JS hook、shell 测试）
```

## 常用命令
| 命令 | 说明 |
|---|---|
| `make build` | 构建前端 → 嵌入 Go 二进制 |
| `make start` | 构建 + 启动服务（PID 跟踪，后台运行） |
| `make stop` | 停止服务 |
| `make restart` | 停止 + 重建 + 启动（代码改动后用这个） |
| `make restart-web` | 仅重建前端 + 重启（仅前端改动时用） |
| `make test` | `go test ./... -count=1 -race`（8 个包） |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w ./internal/ ./cmd/ ./web/embed.go` |
| `make dev` | `go run ./cmd/changez`（前台运行，无需安装） |

**端口**：8760（默认）。配置文件 `config.yaml`。

## 关键陷阱

### 前端嵌入 Go 二进制
`web/embed.go` 使用 `//go:embed dist/*`。任何前端改动后**必须**重新构建 Go 二进制（`make build` 或 `make restart`）。`make restart-web` 仅重建前端然后重启。

### SPA 路由需要服务端回退
路由（`internal/router/router.go`）为非 API、非 MCP 路径提供 `dist/index.html`。SPA 路由直接刷新可以正常工作——但 `dist/index.html` 路径必须正确，否则会 404。

### API 使用查询参数，不是路径参数
文件操作用 query 参数：
- `GET /api/files?project=&limit=&offset=` — 文件列表
- `GET /api/files/versions?path=&source=&action=&since=&until=` — 版本历史
- `GET /api/files/diff?path=&from=&to=` — 两版本 diff
- `GET /api/files/restore?path=&version=` — 恢复文件内容

**不要用路径参数**如 `/api/files/{path}`——那是旧设计。

### 认证使用 Bearer Token
所有 `/api/*` 和 `/mcp*` 路由需要 `Authorization: Bearer <token>`。Token 在 `config.yaml` 中设置。`/api/ui/auth-required` 端点告诉前端是否需要认证。

### Delta 存储是追加写入
Delta 存储在 `data/deltas/` 中作为追加文件。写入使用 `f.Sync()` 保证崩溃安全。后台压缩器合并长 delta 链为 blob 检查点。

### Sources 是预置的
`opencode`、`claudecode`、`cursor`、`human` 在启动时插入（`INSERT OR IGNORE`）。未注册的 source 返回 400。

### 数据目录是 `data/`
SQLite 数据库：`data/changez.db`。Blob：`data/blobs/`。Delta：`data/deltas/`。

## 测试
- 全部是单元测试 + 集成测试（使用真实 SQLite + 文件系统）
- 运行：`make test`（包含 `-race`）
- Handler 测试使用完整 handler 栈 + 内存数据库
- 无需外部服务

## 前端技术栈
- React 18 + TypeScript + Vite 5
- Tailwind CSS + Tailwind Typography
- react-router-dom v6（BrowserRouter）
- react-i18next（中/英，自动检测 + localStorage）
- sonner（Toast 通知，top-right）
- prismjs（语法高亮，prism-tomorrow 主题）
- diff2html（diff 渲染，d2h-dark-color-scheme）
- i18n key 在 `web/src/locales/{en,zh}.json`

## 配置
`config.yaml` 是配置源。关键字段：
- `listen`：HTTP 地址（默认 `127.0.0.1:8760`）
- `token`：Bearer Token（空 = 不认证）
- `storage.max_file_size`：默认 10MB
- `compact.enabled`：后台压缩器开关
- `compact.max_delta_chain`：压缩阈值（默认 50）
- `log.level` / `log.file`：日志配置
