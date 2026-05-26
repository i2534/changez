# Changez Web UI — 技术规范

> 日期: 2025-05-20

## 一、技术栈

| 层面 | 选择 | 理由 |
|------|------|------|
| **框架** | React 18 + TypeScript | 生态成熟，diff 库支持好 |
| **构建** | Vite | 快，配置简单 |
| **样式** | Tailwind CSS v3 | 快速原型，体积小 |
| **路由** | React Router v6 | SPA 路由 |
| **Diff 渲染** | `diff2html` | 直接渲染 unified diff，支持 side-by-side |
| **图标** | 内联 SVG | 零依赖 |

**刻意不用的**：图表库（Dashboard 用纯 CSS 条形条）、状态管理库（React Context 够用）

## 二、目录结构

```
changez/
├── cmd/changez/main.go          # 入口 - 增加 embed + 静态文件服务
├── internal/
│   └── router/router.go         # 增加 Web UI 路由 + public 端点
├── web/                         # ← 新增：前端源码
│   ├── index.html
│   ├── package.json
│   ├── vite.config.ts
│   ├── tailwind.config.js
│   ├── postcss.config.js
│   ├── tsconfig.json
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── index.css            # Tailwind + 全局样式
│   │   ├── api/
│   │   │   ├── client.ts        # fetch 封装（token 管理）
│   │   │   └── types.ts         # API 类型定义
│   │   ├── components/
│   │   │   ├── Layout.tsx       # 顶栏 + 面包屑
│   │   │   ├── LoginModal.tsx   # Token 输入弹窗
│   │   │   ├── StatCard.tsx
│   │   │   ├── SourceBar.tsx    # 来源分布条形图
│   │   │   ├── ActivityFeed.tsx # 最近变更流
│   │   │   ├── FileList.tsx
│   │   │   ├── Timeline.tsx     # 版本时间线
│   │   │   ├── DiffViewer.tsx   # diff2html 封装
│   │   │   └── CodeView.tsx     # 文件内容查看
│   │   └── pages/
│   │       ├── Dashboard.tsx
│   │       ├── Projects.tsx
│   │       ├── Files.tsx
│   │       ├── FileTimeline.tsx
│   │       └── DiffPage.tsx
├── dist/
│   └── changez                  # 最终二进制（内含前端静态文件）
```

## 三、Token 认证方案

### 问题

前端是同域 SPA，但 `/api/` 全部受 Bearer token 保护。

### 方案：首次登录弹窗 + localStorage 持久化

```
1. 前端启动 → GET /api/ui/auth-required (无需 token)
   → 返回 { "required": true/false }

2. required = true → 弹出 LoginModal，用户输入 token
   → 存入 localStorage ("changez_token")
   → 后续所有请求自动带 Authorization header

3. required = false → 直接使用，无需 token

4. 401 响应 → 自动清除 localStorage 中的 token，重新弹出登录框
```

### 后端新增公共端点

```go
// router.go - 在 authMiddleware 之外挂载
mux.HandleFunc("/api/ui/auth-required", func(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{
        "required": token != "",  // token 为空表示未启用认证
    })
})
```

## 四、Go 侧改动清单

| 文件 | 改动 |
|------|------|
| `cmd/changez/main.go` | import `embed`，嵌入 `web/dist/*`，将 `embed.FS` 传入 `router.New()` |
| `internal/router/router.go` | ① 修改 `router.New()` 签名，增加 `webFS *embed.FS` 参数 ② 添加 `/api/ui/auth-required` 公共端点（**挂载在 authMiddleware 之外**）③ 添加静态文件服务（见下方路由优先级）④ 新增 `/api/recent-activity` 端点 |
| `internal/handler/` | 新增 `ui.go`：`HandleRecentActivity` handler |
| `internal/db/db.go` | ① `ListProjects` 返回中增加 `fileCount` 字段（子查询 COUNT） ② `createTables()` 中新增 `idx_versions_changed_at(changed_at DESC)` 索引 |

### 路由优先级（关键）

`http.ServeMux` 按精确匹配 → 前缀匹配优先级分发。静态文件服务必须是**最后注册**的 catch-all，确保不拦截已有端点：

```go
// router.New() 内部注册顺序：
mux.HandleFunc("/health", healthHandler)                          // 精确匹配 - 优先级最高
mux.Handle("/mcp", authMiddleware(mcpHandler, token))            // 前缀 /mcp
mux.Handle("/mcp/", authMiddleware(mcpHandler, token))           // 前缀 /mcp/
mux.Handle("/api/", authMiddleware(apiMux, token))               // 前缀 /api/
// ↑ 以上所有受 auth 保护（除了 /api/ui/auth-required 单独挂载在 auth 之外）

// 静态文件服务 - 最后注册，只处理未被上面匹配的路径
if webFS != nil {
    mux.Handle("/", spaHandler(http.FS(webFS), "index.html"))    // catch-all
}
```

`spaHandler` 实现：优先尝试读取请求路径对应的文件；如果文件不存在且不是 API 路径，回退到 `index.html`（支持 React Router 前端路由）。

## 五、需要新增/修改的后端 API

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/api/ui/auth-required` | 无需 | 返回 `{ "required": bool }` |
| GET | `/api/recent-activity?limit=20` | 需要 | Dashboard 最近变更流 |
| GET | `/api/projects` | 需要 | **修改**：返回中增加 `fileCount` 字段 |

### `/api/recent-activity` 实现

Dashboard 的「最近变更」语义是 activity feed（类似 GitHub Activity），同一文件多次变更出现多条是合理的。直接用全局排序 + LIMIT，无需去重：

```sql
SELECT v.id AS versionId, f.id AS fileId, f.path AS filePath,
       p.id AS projectId, p.name AS projectName,
       v.action, s.name AS source, v.changed_at AS timestamp
FROM versions v
JOIN files f ON v.file_id = f.id
JOIN projects p ON f.project_id = p.id
JOIN sources s ON v.source_id = s.id
WHERE p.is_deleted = 0
ORDER BY v.changed_at DESC
LIMIT ?
```

**性能考虑**：
- 需要新增 `idx_versions_changed_at(changed_at DESC)` 索引以支持全局排序
- 现有 `idx_versions_file_time(file_id, changed_at DESC)` 对此查询不适用（file_id 在前，无法单独用于排序）
- 对于数据量 < 10 万版本的场景，此查询在 SQLite 上可接受（< 50ms）

### 新增索引

在 `createTables()` 中补充：

```sql
CREATE INDEX IF NOT EXISTS idx_versions_changed_at ON versions(changed_at DESC)
```

### `/api/recent-activity` 返回格式

```json
{
  "activity": [
    {
      "fileId": 1,
      "filePath": "internal/db/db.go",
      "projectName": "changez",
      "projectId": 1,
      "versionId": 89,
      "action": "update",
      "source": "opencode",
      "timestamp": "2025-05-20T14:32:10Z"
    }
  ]
}
```

### `/api/projects` 修改

在 `ListProjects` 查询中增加 `fileCount`：

```sql
SELECT p.id, p.name, p.root_path, p.extra, p.created_at,
       (SELECT COUNT(*) FROM files WHERE project_id = p.id) AS file_count
FROM projects p
WHERE p.is_deleted = 0
ORDER BY p.name
```

**注意**：此改动会改变 `ListProjects` 的返回结构，需要同步更新以下测试文件中的期望值：
- `internal/db/db_test.go` — `TestListProjects`（约 120-195 行）
- `internal/handler/handler_test.go` — `TestHandleListProjects`（约 106-123 行）
- 在断言中增加 `"fileCount": <expected_count>` 字段即可

## 六、构建流程

```bash
# 1. 构建前端
cd web && npm install && npm run build  # → web/dist/

# 2. 构建 Go 二进制（自动嵌入 web/dist/*）
go build -o dist/changez ./cmd/changez

# 3. 运行（单二进制，含前端）
./dist/changez -c config.yaml
```

### Makefile 更新

```makefile
.PHONY: build build-web

build-web:
	cd web && npm install && npm run build

build: build-web
	@mkdir -p $(DIST)
	go build -o $(BINARY) $(CMD)
```

### .gitignore 策略

```gitignore
# .gitignore 新增
web/node_modules/
web/dist/
```

`web/dist/` **不入版**。原因：
- 它是构建产物，应由 `make build` 在需要时生成
- Go `embed` 在 `web/dist/` 为空时不会嵌入任何文件，此时启动后访问 `/` 会返回 404（不影响 API 和 MCP 功能）
- CI/CD 和发布流程中先执行 `make build`（含前端构建）再打包

## 七、前端 API 封装 (`api/client.ts`)

```typescript
const TOKEN_KEY = "changez_token";

export async function getToken(): Promise<string | null> {
  return localStorage.getItem(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  localStorage.setItem(TOKEN_KEY, token);
}

export async function clearToken(): Promise<void> {
  localStorage.removeItem(TOKEN_KEY);
}

export async function api(path: string, options?: RequestInit): Promise<Response> {
  const token = await getToken();
  const headers: HeadersInit = {
    "Content-Type": "application/json",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...options?.headers,
  };

  const res = await fetch(path, { ...options, headers });
  if (res.status === 401) {
    clearToken();
    window.dispatchEvent(new CustomEvent("auth-required"));
  }
  return res;
}
```

## 八、部署

单二进制部署，`go build` 自动嵌入前端静态文件。运行后：
- `http://127.0.0.1:8760/` → Web UI（SPA）
- `http://127.0.0.1:8760/api/` → REST API（受 token 保护）
- `http://127.0.0.1:8760/mcp/` → MCP 协议（受 token 保护）
