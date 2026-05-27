# Changez

代码变更版本化服务器。为 AI 编程工具（OpenCode、ClaudeCode、Cursor 等）提供文件变更的自动追踪、版本管理和差异对比。

## 核心特性

- **自动快照** — AI 工具通过 hook 自动提交文件变更，无需手动操作
- **Delta 压缩存储** — 使用 diff-match-patch + zstd 压缩，大幅节省存储空间
- **内容寻址** — SHA256 去重，相同内容只存储一次
- **后台压缩** — 自动合并长 delta 链为 blob 检查点，保持读取性能
- **软删除 + 定时清理** — 项目/文件支持软删除，后台定时清理孤儿数据
- **Web 浏览** — React SPA 界面，支持文件树、版本时间线、diff 对比、代码高亮、删除操作
- **MCP 集成** — 内置 MCP 服务器，AI 工具可直接查询变更历史
- **轻量部署** — 单二进制文件，SQLite + 文件系统存储，无需外部依赖

## 快速开始

### 前置要求

- Go 1.25+
- Node.js 18+（构建前端）
- npm

### 构建与启动

```bash
# 完整构建 + 启动（后台运行）
make start

# 开发模式（前台运行，便于调试）
make dev

# 停止服务
make stop

# 代码改动后重启
make restart

# 仅前端改动后重启
make restart-web
```

服务默认监听 `127.0.0.1:8760`。

### 配置

复制 `config.yaml` 并修改：

```yaml
listen: "127.0.0.1:8760"
token: "your-secret-token"      # 留空则不启用认证
storage:
  max_file_size: 10485760       # 10MB
compact:
  enabled: true
  interval: "24h"
  max_delta_chain: 50
cleanup:
  enabled: true
  interval: "168h"       # 一周清理一次
log:
  level: "info"
  file: "changez.log"
```

## API 使用

所有 API 请求需携带 `Authorization: Bearer <token>`（如果配置了 token）。

### 提交快照

```bash
curl -X POST http://127.0.0.1:8760/api/snapshot \
  -H "Authorization: Bearer your-token" \
  -H "Content-Type: application/json" \
  -d '{
    "source": "claudecode",
    "sessionId": "ses-abc123",
    "model": "claude-sonnet-4-20250514",
    "files": [
      {
        "path": "src/main.go",
        "content": "package main\n\nfunc main() {}\n",
        "action": "update"
      }
    ]
  }'
```

### 查询文件列表

```bash
curl "http://127.0.0.1:8760/api/files?project=myproject&limit=50" \
  -H "Authorization: Bearer your-token"
```

### 查询版本历史

```bash
curl "http://127.0.0.1:8760/api/files/versions?path=src%2Fmain.go&source=claudecode" \
  -H "Authorization: Bearer your-token"
```

### 对比两个版本

```bash
curl "http://127.0.0.1:8760/api/files/diff?path=src%2Fmain.go&from=1&to=5" \
  -H "Authorization: Bearer your-token"
```

### 恢复指定版本

```bash
curl "http://127.0.0.1:8760/api/files/restore?path=src%2Fmain.go&version=3" \
  -H "Authorization: Bearer your-token"
```

### 删除文件（软删除）

```bash
curl -X DELETE "http://127.0.0.1:8760/api/files?project=myproject&path=src%2Fmain.go" \
  -H "Authorization: Bearer your-token"
```

### 删除项目（软删除）

```bash
curl -X DELETE http://127.0.0.1:8760/api/projects/1 \
  -H "Authorization: Bearer your-token"
```

## Hook 集成

### ClaudeCode / wps_claude

```bash
# 设置环境变量
export CHANGEZ_URL="http://127.0.0.1:8760"
export CHANGEZ_TOKEN="your-token"
export CHANGEZ_SOURCE="claudecode"

# 在 ClaudeCode 配置中使用 hook
# hook 脚本：client/claudecode/changez-hook.js
```

### OpenCode

通过 MCP 服务器集成，配置 MCP server 指向 `http://127.0.0.1:8760/mcp`。

## MCP 工具

Changez 提供以下 MCP 工具：

| 工具 | 说明 |
|---|---|
| `changez_snapshot` | 提交文件快照 |
| `changez_file_log` | 查询文件版本历史 |
| `changez_file_diff` | 对比两个版本 |
| `changez_file_restore` | 恢复指定版本内容 |

## 技术架构

```
┌──────────────────────────────────────────────────────────┐
│                    HTTP Server (:8760)                    │
│  ┌─────────────┐  ┌─────────────┐  ┌──────────────────┐ │
│  │  /api/*     │  │  /mcp/*     │  │  SPA (React)     │ │
│  │  REST API   │  │  MCP Server │  │  文件浏览/Diff   │ │
│  └──────┬──────┘  └──────┬──────┘  └──────────────────┘ │
│         │                 │                               │
│         ▼                 ▼                               │
│  ┌──────────────────────────────────┐                    │
│  │          Handler Layer           │                    │
│  │  snapshot / list / log / diff    │                    │
│  │  restore / projects / stats      │                    │
│  └────────┬─────────────────────────┘                    │
│           │                                               │
│  ┌────────▼─────────────────────────┐                    │
│  │           SQLite DB              │                    │
│  │  projects / files / versions     │                    │
│  │  blobs / sources                 │                    │
│  └────────┬─────────────────────────┘                    │
│           │                                               │
│  ┌────────▼─────────────────────────┐                    │
│  │       Storage Layer              │                    │
│  │  blobs/  — SHA256 内容寻址       │                    │
│  │  deltas/ — 追加写入 + zstd 压缩  │                    │
│  └──────────────────────────────────┘                    │
│           │                                               │
│  ┌────────▼─────────────────────────┐                    │
│  │       Compactor (后台)           │                    │
│  │  合并长 delta 链 → blob 检查点    │                    │
│  └──────────────────────────────────┘                    │
│           │                                               │
│  ┌────────▼─────────────────────────┐                    │
│  │       Cleanup (后台)             │                    │
│  │  清理软删数据 + 孤儿 blob/delta   │                    │
│  └──────────────────────────────────┘                    │
└──────────────────────────────────────────────────────────┘
```

### 存储策略

- **Blob**：完整文件快照，zstd 压缩，SHA256 命名（去重）
- **Delta**：diff-match-patch 生成的语义差异，zstd 压缩，追加写入
- **Compactor**：后台定时合并长 delta 链为 blob，缩短读取路径

### 数据目录结构

```
data/
├── changez.db          # SQLite 数据库
├── blobs/              # 内容寻址 blob（SHA256 文件名）
│   └── a1b2c3...       # zstd 压缩的完整文件内容
└── deltas/             # Delta 追加文件
    └── 1.delta         # 按文件 ID 组织的 delta 流
```

## 前端技术栈

- React 18 + TypeScript + Vite 5
- Tailwind CSS + Tailwind Typography
- react-router-dom v6（BrowserRouter）
- react-i18next（中/英双语）
- PrismJS（语法高亮，16 种语言）
- diff2html（diff 渲染）
- Sonner（Toast 通知）

## 开发

```bash
# 运行全部测试（含 race detector）
make test

# 代码检查
make vet

# 格式化代码
make fmt

# 仅重建前端
cd web && npm run build
```

## 许可

[待定]
