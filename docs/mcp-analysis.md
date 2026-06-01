# MCP 模块分析与完善方案

## 当前状态

### 架构

```text
internal/mcp/
├── handler.go      ← MCP 服务器初始化（mcp-go 库）
├── tools.go        ← MCP Tool 定义 + 处理函数
└── mcp_test.go     ← 单元测试

internal/handler/mcp.go  ← ProcessSnapshot/ProcessLog/ProcessRestore/ProcessDiff
internal/router/router.go  ← /mcp, /mcp/ 路由 + authMiddleware
```

基于 `github.com/mark3labs/mcp-go v0.43.2`，使用 StreamableHTTPServer（SSE 传输）。

### 已实现的 Tools（4 个）

| Tool | 功能 | 对应 API |
|---|---|---|
| `changez_snapshot` | 提交文件快照 | POST /api/snapshot |
| `changez_log` | 查询文件版本历史 | GET /api/files/versions |
| `changez_restore` | 恢复指定版本 | GET /api/files/restore |
| `changez_diff` | 对比两个版本 | GET /api/files/diff |

### 已启用的 MCP 能力

- `WithToolCapabilities(true)` ✓
- `WithLogging()` ✓（启用但未被充分利用）
- SSE 传输 ✓

---

## 核心问题：`changez_snapshot` 在 MCP 中是否有意义？

**结论：不需要。**

原因：
1. 文件快照由 **hook 自动收集**（OpenCode 插件、Claude Code stdin hook、Cursor afterFileEdit）
2. AI agent 写文件时，hook 已经自动上报，不需要 agent 手动调用
3. 让 agent 调 snapshot 需要它自己收集文件路径、内容、source 等信息——**繁琐且多余**

`changez_snapshot` 是**产生数据**的操作，不是**利用数据做决策**的操作。它的消费者是 hook 脚本（走 HTTP API），不是 AI agent。

---

## MCP 的定位

**Client 端** → 收集变更数据（hook 驱动，走 HTTP API）  
**MCP tools** → 让 AI agent **查询和利用**已收集的变更数据做决策

MCP 应该专注于 agent 的**查询/利用**能力，而非数据生产。

---

## 完善方案

### 应移除

| Tool | 原因 |
|---|---|
| ~~`changez_snapshot`~~ | 这是 hook 的事，agent 不需要手动提交 |

### 应保留

| Tool | Agent 场景 |
|---|---|
| `changez_log` | "这个文件改过几次？谁改的？" |
| `changez_diff` | "两个版本之间差了什么？" |
| `changez_restore` | "帮我把这个文件恢复到 v3 的内容" |

### 应新增

| Tool | Agent 场景 | 对应 API |
|---|---|---|
| `changez_activity` | "最近这个项目的变更趋势是什么？" | GET /api/recent-activity |
| `changez_files` | "这个项目里跟踪了哪些文件？" | GET /api/files |
| `changez_stats` | "这个项目的规模有多大？" | GET /api/stats |

### 最终 Tools 列表（6 个）

| # | Tool | 角色 | 参数 |
|---|---|---|---|
| 1 | `changez_files` | **了解项目结构** | `project` (required), `limit` (optional), `offset` (optional) |
| 2 | `changez_activity` | **了解项目动态** | `project` (optional), `source` (optional), `limit` (optional, default 20) |
| 3 | `changez_stats` | **了解项目规模** | `project` (optional) |
| 4 | `changez_log` | **深入查询** | `path` (required), `since/until` (optional), `source` (optional), `action` (optional), `limit/offset` (optional) |
| 5 | `changez_diff` | **深入查询** | `path` (required), `versionA/versionB` (required) |
| 6 | `changez_restore` | **深入查询** | `path` (required), `version` (required) |

覆盖 agent 从"了解全局"到"深入查询"的完整链路。

---

## 各 Tool 详细设计

### `changez_files`

列出项目中被跟踪的文件。Agent 需要了解项目结构时调用。

```
changez_files(project="myproject", limit=50)
```

- `project` — 项目名称（required）
- `limit` — 最大返回数（optional，默认 100）
- `offset` — 偏移量（optional，默认 0）

> **注意**：`HandleListFiles` 当前支持 `project`、`limit`、`offset`，**不支持 `search` 参数**。如果后续需要文件名搜索，需先扩展 handler 层。

### `changez_activity`

返回最近的变更活动。Agent 需要了解项目动态时调用。

```
changez_activity(project="myproject", source="opencode", limit=20)
```

- `project` — 项目名称（optional，不加则返回全局）
- `source` — 来源过滤（optional）
- `limit` — 最大返回数（optional，默认 20）

> **注意**：`HandleRecentActivity` 当前**不支持 `project`/`source` 过滤**，只支持 `limit`。需要在 handler 层新增过滤逻辑。

### `changez_stats`

返回项目统计信息。Agent 需要了解项目规模时调用。

```
changez_stats(project="myproject")
```

- `project` — 项目名称（optional，不加则返回全局统计）

> **注意**：`HandleStats` 当前**不支持 `project` 过滤**，只返回全局统计。需要在 handler 层新增按 project 查询的逻辑。

### `changez_log`（无变更）

查询文件变更历史。当前已支持 `path`、`since`、`until`、`source`、`action`、`limit`、`offset`。

> **注意**：`project` 参数是多余的——`doLog` 通过 `path` 自动解析 project（`FindProjectByPath` → `GetFileByPath`），无需额外过滤。

```
changez_log(path="/home/user/proj/src/main.go", since="2025-01-01", source="opencode")
```

### `changez_diff`（无变更）

对比两个版本。当前实现使用 `mcp.WithNumber` + `req.GetInt()`，version ID 是 auto-increment int64，不会达到 float64 精度上限（2^53），**无需修改**。

### `changez_restore`（无变更）

恢复指定版本。同上，**无需修改**。

> **验证结论**：mcp-go 库没有 `WithInteger`，只有 `WithNumber`（JSON number / float64）。`GetInt` 返回 `int`（64-bit 系统上等于 int64）。version ID 不会达到 float64 精度上限，实践中无精度问题。

---

## 设计权衡

### `changez_stats` — 全局统计 vs 按 project 过滤

全局统计（"项目有 N 个文件"）对 agent 决策帮助有限，按 project 过滤才有实际价值。但按 project 过滤需要改动 DB 层（`GetStats` 是固定 SQL），改动成本不低。

**取舍**：保留 `project` 参数，实施时改动 handler 层。如果后续发现改动成本过高，可降级为全局统计 tool。

### `changez_activity` — 需要 project 过滤，但改动 handler 成本不低

不加 project 过滤时，返回全局活动，噪音可能太大。但改动 `HandleRecentActivity` 需要修改 SQL 查询（当前只有 `WHERE p.is_deleted = 0`）。

**取舍**：保留 `project` 参数，实施时改动 handler 层。改动范围有限（加一个 WHERE 条件），成本可控。

### handler 改动量评估

3 个新增 tool 中有 2 个需要改动 handler 层：

| 改动 | 范围 | 复杂度 |
|---|---|---|
| `HandleRecentActivity` 加 project/source 过滤 | 改 SQL WHERE 条件 | 低 |
| `HandleStats` 加 project 过滤 | 改 DB 层查询 + handler | 中 |

改动量不大，Phase 1 的工作量可控。

---

## 代码质量问题

暂无。

> ~~version 参数精度问题~~：已验证 mcp-go 无 `WithInteger`，version ID 不会达到 float64 精度上限，实践中无此问题。  
> ~~`changez_log` 缺少 project 参数~~：`doLog` 通过 path 自动解析 project，无需额外过滤。

---

## 已讨论并排除的方案

### MCP Resources

Resources 适合做只读数据源（`changez://projects`、`changez://files/{project}`），但 Resources 是被动拉取的，不支持参数过滤。对于 agent 主动查询的场景，**tools 更灵活**。暂不实现。

### MCP Prompts

Prompts 是给人类用户降低使用门槛的，目标用户是 AI agent，它们已经知道怎么调 tool。暂不实现。

### 错误信息语言统一

handler 层返回的错误信息**中英混杂**（如 `"source 处理失败: %v"` 和 `"unknown source: %s"`）。LLM 对两种语言都能理解，加翻译层增加维护成本。**保持现状，MCP 透传 handler 层返回的错误信息**。

---

## handler 层能力对照

| MCP Tool | handler 方法 | 当前支持 | 需扩展 | 改动复杂度 |
|---|---|---|---|---|
| `changez_files` | `HandleListFiles` | project, limit, offset | — | — |
| `changez_activity` | `HandleRecentActivity` | limit | **project, source** | 低（改 SQL WHERE） |
| `changez_stats` | `HandleStats` | 全局统计 | **project 过滤** | 中（改 DB 层 + handler） |
| `changez_log` | `ProcessLog` | path, since, until, source, action, limit, offset | — | — |
| `changez_diff` | `ProcessDiff` | path, versionA, versionB | — | — |
| `changez_restore` | `ProcessRestore` | path, version | — | — |

---

## 测试覆盖

- ✅ 工具定义验证
- ✅ 参数缺失/错误验证
- ✅ 正常快照流程
- ❌ 缺少 log/restore/diff 端到端流程测试（仅有参数错误测试）
- ❌ 缺少新增 tools 的测试

---

## 实施计划

### Phase 1: Handler 层扩展

- [ ] `HandleRecentActivity` 增加 `project`/`source` 过滤
- [ ] `HandleStats` 增加 `project` 过滤（或新增 `ProcessStatsByProject`）

### Phase 2: 移除

- [ ] 移除 `changez_snapshot` tool

### Phase 3: 新增 Tools

- [ ] 实现 `changez_files`
- [ ] 实现 `changez_activity`
- [ ] 实现 `changez_stats`

### Phase 4: 测试

- [ ] 新增 tools 的完整测试（含 handler 层扩展的测试）
- [ ] 端到端 log/restore/diff 流程测试
