# Claude Code 接入设计文档

> 创建日期：2026-05-18
> 状态：✅ Phase 1 已完成（2026-05-20）

---

## 目标

在 Claude Code 中自动追踪文件变更，AI 修改文件后自动上报 snapshot 到 changez 服务。用户零操作，完全无人参与。

---

## 实现状态

| 组件 | 文件 | 状态 |
|------|------|------|
| Hook 脚本 | `client/claudecode/changez-hook.js` | ✅ 已完成（~180 行） |
| 自动化测试 | `client/claudecode/test-hook.sh` | ✅ 10/10 通过 |
| HTTP Daemon（备用） | `client/claudecode/changez-daemon.js` | ✅ 已搁置 |
| 重试机制 | 进程内指数退避 | ✅ 已完成 |
| 本地队列（Phase 2） | — | ⏸️ 未启动 |

---

## 架构总览

```
┌─────────────────────────────────────────────────────┐
│  Claude Code                                        │
│                                                     │
│  AI ──Write/Edit──▶ Tool Execution                  │
│                      │                              │
│                      ▼ PostToolUse hook              │
│              ┌──────────────────┐                    │
│              │ Hook Matcher:    │                    │
│              │ "Write|Edit"     │                    │
│              │ Type: command    │                    │
│              │ Command:         │                    │
│              │ changez-hook   │                    │
│              │ $ARGUMENTS JSON  │                    │
│              └────────┬─────────┘                    │
│                       │ shell exec                   │
│                       ▼                              │
│              ┌──────────────────┐                    │
│              │ changez-hook   │                    │
│              │ (Node.js 脚本)   │                    │
│              │ 解析 stdin JSON  │                    │
│              │ 提取 path+content│                    │
│              │ POST → /api/     │                    │
│              │ snapshot         │                    │
│              └──────────────────┘                    │
└─────────────────────────────────────────────────────┘
                  │ HTTP POST
                  ▼
            ┌────────────────┐
            │  changez       │
            │  service       │
            │  :8760/api/    │
            │  (无需任何改动) │
            └────────────────┘
```

**核心原则：** 利用 Claude Code 原生 hooks 系统，无需修改 Claude Code 源码。通过 PostToolUse hook + shell 脚本上报 snapshot。

---

## Claude Code Hooks 系统（调研发现）

### 可用 Hook 事件（27 种）

从 Claude Code cli.js 逆向分析得到的关键事件：

| 事件 | 触发时机 | 输入数据 |
|------|---------|---------|
| `PreToolUse` | 工具执行前 | tool_name, tool_input, tool_use_id |
| `PostToolUse` | 工具执行成功后 | tool_name, tool_input, tool_response |
| `PostToolUseFailure` | 工具执行失败后 | error, is_interrupt |
| `FileChanged` | 文件系统变更 | file_path, event(change/add/unlink) |
| `SessionStart` | 会话启动 | session_id, model |
| `SessionEnd` | 会话结束 | session_id, reason |
| `UserPromptSubmit` | 用户提交 prompt | user_message |

### Hook 配置格式

```jsonc
// Claude Code settings（~/.claude/settings.json 或 .claude/settings.json）
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "changez-hook",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

### Hook 类型

| type | 说明 | 输入传递方式 |
|------|------|-------------|
| `command` | 执行外部命令 | **JSON 通过 stdin 传入**（`child.stdin.write(jsonInput)`） |
| `http` | POST webhook | POST body = JSON |
| `prompt` | LLM 评估 | 命令字符串中 `$ARGUMENTS` 占位符被替换 + stdin |
| `agent` | 完整 agent | 命令字符串中 `$ARGUMENTS` 占位符被替换 + stdin |

**command 类型通过 stdin 接收 JSON**，脚本从 stdin 读取即可。Node.js 用 `for await (const chunk of process.stdin)`。

### Hook 输出格式（SyncHookJSONOutputSchema）

hook 可以返回 JSON 控制 Claude Code 行为：

```json
{
  "continue": true,        // 是否继续执行
  "suppressOutput": false, // 是否抑制输出
  "decision": "approve",   // "approve" | "block"
  "systemMessage": ""      // 注入系统消息
}
```

**重要：** changez-hook 不需要返回控制 JSON，只需 fire-and-forget。

### 关键限制

1. `CLAUDE_CODE_SIMPLE=1`（即 `--bare` 模式）会禁用所有 hooks
2. `http` 类型不支持 `SessionStart` 和 `Setup` 事件
3. Hook 执行是阻塞的（有 timeout 保护，默认 10s）
4. matcher 支持管道分隔的模式匹配：`"Write|Edit|Bash"`

---

## 方案：PostToolUse Hook + Node.js 脚本

### 为什么选择 PostToolUse + command

| 方案 | 优点 | 缺点 |
|------|------|------|
| **PostToolUse + command** | 工具执行成功后触发，有完整 input+output，无需额外读取文件 | hook 阻塞 AI 等待返回 |
| PostToolUse + http | 直接 POST，无需中间脚本 | http hook 不改造 payload 格式，changez 服务需适配 |
| PreToolUse | 写入前拦截 | 工具可能失败，文件未实际写入 |
| FileChanged | 文件系统级监听 | 只有 file_path 无 content，需额外读取 |
| MCP tool | 标准 MCP 集成 | AI 不会自动调用，需 Rule/Skill 引导，不可靠 |

**结论：PostToolUse + command 类型最可靠。**

### 为什么用 Node.js 而非 shell 脚本

| 维度 | shell 脚本 | Node.js 脚本 |
|------|-----------|-------------|
| JSON 处理 | 依赖 `jq`，多层提取丑 | 原生支持，`JSON.parse()` |
| 大文件 content | shell 变量有大小限制，可能截断/OOM | `fs.readFileSync` 直接读，无此问题 |
| HTTP 请求 | 依赖 `curl` | 原生 `fetch()` |
| 外部依赖 | `jq` + `curl` | **零外部依赖** |
| 跨平台 | bash 版本差异 | Node.js 环境一定存在（Claude Code 本身是 Node 进程） |
| 代码风格 | 与项目不一致 | 与 OpenCode 插件（TypeScript）一致 |

**结论：Node.js 脚本更可靠、更简洁、零外部依赖。**

### Hook 输入 JSON（实机验证）

PostToolUse hook 通过 **stdin** 接收 JSON，实际格式：

```json
{
  "session_id": "06e8062a-603e-45be-affb-92674aef3850",
  "transcript_path": "/home/lan/.claude/projects/-home-lan-workspace-go-changez/06e8062a.jsonl",
  "cwd": "/home/lan/workspace/go/changez",
  "permission_mode": "acceptEdits",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write",
  "tool_input": {
    "file_path": "/home/lan/workspace/go/changez/docs/hook-test2.md",
    "content": "test2\n"
  },
  "tool_response": {
    "type": "create",
    "filePath": "/home/lan/workspace/go/changez/docs/hook-test2.md",
    "content": "test2\n",
    "structuredPatch": [],
    "originalFile": null
  },
  "tool_use_id": "Write:1"
}
```

**Write 工具：**
- `tool_input.file_path` — 文件绝对路径
- `tool_input.content` — **完整文件内容**
- `tool_response.type` — `"create"` 或 `"update"`（精确区分 action！）
- `tool_response.content` — 写入的完整内容（与 tool_input 一致）
- `tool_response.originalFile` — 旧文件内容（新建文件为 null）

**Edit 工具（实机验证）：**
- `tool_input.file_path` — 文件绝对路径
- `tool_input.old_string` — 要替换的旧内容片段
- `tool_input.new_string` — 替换后的新内容片段
- `tool_response.originalFile` — **编辑后的完整文件内容**（实机验证可用！）
- `tool_response.structuredPatch` — 数组，包含行级 diff 信息

---

### Edit vs Write 文件内容获取策略

| 工具 | `tool_input` 有 content | `tool_response` 有 content | 获取完整文件内容的方式 |
|------|------------------------|---------------------------|----------------------|
| **Write** | ✅ `tool_input.content` | ✅ `tool_response.content` | 直接用 `tool_input.content` |
| **Edit** | ❌ 只有 old_string/new_string | ✅ `tool_response.originalFile` | 用 `tool_response.originalFile` |

**结论**：脚本获取文件内容的逻辑：
```javascript
// 优先从 hook 数据获取
let content = hook.tool_input?.content        // Write
            ?? hook.tool_response?.originalFile // Edit

// 兜底：从文件系统读取
if (!content && fs.existsSync(filePath)) {
  content = fs.readFileSync(filePath, "utf8")
}
```

---

## changez-hook.js 统一脚本设计

### 位置

```
~/.local/bin/changez-hook
```

或随 changez 服务一起分发：

```
client/claudecode/changez-hook.js    # 项目代码仓库中
```

### 架构

一个脚本处理所有 hook 事件，通过 `hook.tool_name` 是否存在来区分事件类型：

- **SessionStart** → 项目自动注册（`POST /api/projects`）
- **PostToolUse** → 文件变更上报（`POST /api/snapshot`）

### 脚本逻辑

```javascript
#!/usr/bin/env node
"use strict"

/**
 * changez-hook — Claude Code 统一 hook 脚本
 *
 * 通过 stdin 接收 hook JSON，处理 SessionStart（项目注册）和 PostToolUse（文件变更上报）。
 * 
 * 零外部依赖（仅用 Node.js 内置模块）。
 * fire-and-forget，不阻塞 Claude Code 执行流。
 */

const fs = require("fs")

// ── 配置（从环境变量读取）─────────────────────────────
const CHANGEZ_URL = process.env.CHANGEZ_URL ?? "http://127.0.0.1:8760"
const CHANGEZ_TOKEN = process.env.CHANGEZ_TOKEN ?? ""
const CHANGEZ_SOURCE = process.env.CHANGEZ_SOURCE ?? "claudecode"
const CHANGEZ_LOG_FILE = process.env.CHANGEZ_LOG_FILE ?? ""
const MAX_FILE_SIZE = parseInt(process.env.CHANGEZ_MAX_FILE_SIZE ?? "10485760", 10) // 10MB

// ── 日志辅助 ──────────────────────────────────────────
function log(level, msg) {
  const ts = new Date().toISOString()
  const line = `[${ts}] [${level}] ${msg}`
  
  // 如果配置了日志文件，追加写入
  if (CHANGEZ_LOG_FILE) {
    try { fs.appendFileSync(CHANGEZ_LOG_FILE, line + "\n") } catch {}
  }
  
  // 同时输出到 stderr（Claude Code hook 会捕获）
  if (level === "error") {
    console.error(line)
  }
}

// ── 主逻辑 ────────────────────────────────────────────
async function main() {
  // 1. 从 stdin 读取 JSON
  let raw = ""
  for await (const chunk of process.stdin) {
    raw += chunk
  }
  if (!raw) {
    log("debug", "empty stdin")
    return
  }

  let hook
  try {
    hook = JSON.parse(raw)
  } catch (e) {
    log("error", `JSON parse failed: ${e.message}`)
    return
  }

  // 2. 只处理 Write 和 Edit 工具
  const toolName = hook.tool_name
  if (toolName !== "Write" && toolName !== "Edit") {
    log("debug", `skip tool: ${toolName}`)
    return
  }

  // 3. 提取字段
  const filePath = hook.tool_input?.file_path
  if (!filePath) {
    log("debug", "no file_path in tool_input")
    return
  }

  const action = hook.tool_response?.type ?? "update"
  const sessionId = hook.session_id ?? ""
  let content = hook.tool_input?.content ?? ""

  // 4. Edit 工具 fallback — 从文件系统读取编辑后的文件
  if (!content && fs.existsSync(filePath)) {
    try {
      const stat = fs.statSync(filePath)
      if (stat.size > MAX_FILE_SIZE) {
        log("debug", `skip large file (${stat.size} > ${MAX_FILE_SIZE}): ${filePath}`)
        return
      }
      content = fs.readFileSync(filePath, "utf8")
    } catch (e) {
      log("error", `read file failed: ${filePath}: ${e.message}`)
      return
    }
  }

  // 5. Write 工具 — 检查文件大小
  if (toolName === "Write" && content) {
    const size = Buffer.byteLength(content, "utf8")
    if (size > MAX_FILE_SIZE) {
      log("debug", `skip large file (${size} > ${MAX_FILE_SIZE}): ${filePath}`)
      return
    }
  }

  // 6. 构建请求体
  const body = JSON.stringify({
    source: CHANGEZ_SOURCE,
    sessionId,
    files: [{
      path: filePath,
      content,
      action,
    }],
  })

  // 7. 发送 snapshot（fire-and-forget）
  log("debug", `reporting: ${filePath} (${action}, ${Buffer.byteLength(content, "utf8")} bytes)`)

  try {
    const headers = {
      "Content-Type": "application/json",
    }
    if (CHANGEZ_TOKEN) {
      headers["Authorization"] = `Bearer ${CHANGEZ_TOKEN}`
    }

    const resp = await fetch(`${CHANGEZ_URL}/api/snapshot`, {
      method: "POST",
      headers,
      body,
      signal: AbortSignal.timeout(5000),
    })

    if (resp.ok) {
      log("debug", `snapshot OK: ${filePath}`)
    } else {
      log("warn", `snapshot failed: ${resp.status} ${resp.statusText}`)
    }
  } catch (e) {
    // fire-and-forget — 不阻塞 Claude Code
    log("warn", `snapshot error: ${e.message}`)
  }
}

// 执行并始终 exit 0
main().then(() => process.exit(0)).catch((e) => {
  log("error", `unhandled: ${e.message}`)
  process.exit(0)
})
```

### 关键设计点

1. **JSON 从 stdin 读取**：`for await (const chunk of process.stdin)` 接收 Claude Code 传入的 JSON
2. **fire-and-forget**：fetch 失败不影响 Claude Code（catch 后继续，始终 exit 0）
3. **超时保护**：`AbortSignal.timeout(5000)` 防止 hook 卡住
4. **action 取值**：`tool_response.type` 返回 `"create"` 或 `"update"`。服务端 `ProcessSnapshot` 校验 action 字段（只接受 `create`/`update`/`delete`/空字符串），并通过版本内容哈希比对去重。脚本从 `tool_response.type` 获取 action 值，服务端会做双重判断修正（create+已存在→update，update+不存在→create）。
5. **Edit 工具 fallback**：如果 tool_input 没有完整 content，用 `fs.readFileSync` 直接从文件系统读取
6. **大文件过滤**：`MAX_FILE_SIZE` 环境变量控制（默认 10MB），超过跳过
7. **日志输出**：可选日志文件（`CHANGEZ_LOG_FILE`），同时输出到 stderr
8. **零外部依赖**：仅用 Node.js 内置模块（`fs`），不需要 `jq` / `curl`
9. **sessionId 可用**：hook JSON 包含 `session_id`，直接传给 changez
10. **始终 exit 0**：不阻塞 Claude Code 执行流

---

## Session/Model 信息获取

### session_id 可用（实机验证）

hook JSON 中包含 `session_id` 字段，可以直接传给 changez：

```json
{
  "session_id": "06e8062a-603e-45be-affb-92674aef3850",
  ...
}
```

脚本中通过 `jq -r '.session_id // empty'` 提取，作为 `sessionId` 传给 changez。

### model 不可用

hook JSON 中没有 model 信息。Claude Code 的 PostToolUse hook 不暴露当前使用的模型。

**结论：传 sessionId，不传 model。**

---

## 配置方式

### 方案 A：项目级配置（推荐）

在项目根目录创建 `.claude/settings.json`：

```jsonc
{
  "env": {
    "CHANGEZ_URL": "http://127.0.0.1:8760",
    "CHANGEZ_TOKEN": ""
  },
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "changez-hook",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

**⚠️ token 配置说明**：changez 服务端对 `/api/` 路由配置了认证中间件（`router.go`）。
- 如果服务端配置了 token → `CHANGEZ_TOKEN` **必须**配置，否则请求会被 401 拒绝
- 如果服务端未配置 token → 认证中间件直接透传，`CHANGEZ_TOKEN` 可留空

### 方案 B：全局配置

在 `~/.claude/settings.json` 中添加 hooks 配置，对所有项目生效。

**完整配置示例**：
```jsonc
// ~/.claude/settings.json
{
  "env": {
    "CHANGEZ_URL": "http://127.0.0.1:8760",
    "CHANGEZ_TOKEN": "",
    "CHANGEZ_SOURCE": "claudecode",
    "CHANGEZ_LOG_FILE": "/tmp/changez-claude.log",
    "CHANGEZ_MAX_FILE_SIZE": "10485760"
  },
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "changez-register",
            "timeout": 5000
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "changez-hook",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

### 方案 C：一键安装脚本

提供安装脚本，用户执行一次即可自动配置：

```bash
# changez install claudecode
# 自动：
# 1. 安装 changez-hook 到 ~/.local/bin/
# 2. 在 settings.json 中注入 hooks 配置
# 3. 配置环境变量
```

---

## 文件删除检测

Claude Code 没有原生的 "文件删除" hook。但 `FileChanged` hook 支持 `unlink` 事件：

```jsonc
{
  "hooks": {
    "FileChanged": [
      {
        "matcher": "*.go|*.ts|*.py",
        "hooks": [
          {
            "type": "command",
            "command": "changez-file-event",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

`changez-file-event` 脚本解析 `$ARGUMENTS` 中的 event 字段：

- `change` → 跳过（PostToolUse 已覆盖）
- `add` → POST create snapshot
- `unlink` → POST delete snapshot

**但 FileChanged hook 只有 file_path 和 event，没有 content。** add/change 事件需要额外读取文件。

### Phase 2 删除支持

Phase 2 实现文件删除支持非常直接：
- 服务端已支持 `action: "delete"`（`TestProcessSnapshot_Delete` 已验证）
- `FileChanged` hook 可捕获 `unlink` 事件
- 只需编写 `changez-file-event` 脚本处理 `unlink` → POST delete snapshot

**注意**：通过 `Bash(rm)` 执行的删除不会触发 Write/Edit hook，只能通过 `FileChanged` hook 捕获。

Phase 1 只处理 Write/Edit，不处理文件删除。文件删除是低频操作，且可以通过 git 追踪。

---

## 错误处理

| 场景 | 行为 |
|------|------|
| changez 服务不可达 | fetch 超时/失败，catch 忽略，exit 0 |
| JSON 解析失败 | 记录日志，exit 0 |
| 文件读取失败（Edit fallback） | 记录日志，跳过该文件 |
| stdin 为空 | 记录日志，exit 0 |
| 文件超过 MAX_FILE_SIZE | 记录日志，跳过该文件 |
| hook timeout | Claude Code 自动跳过，不影响主流程 |
| 未捕获异常 | `.catch()` 记录日志，exit 0 |

---

## 与 OpenCode 插件对比

| 维度 | OpenCode 插件 | Claude Code Hook | Cursor 扩展 |
|------|--------------|-----------------|-------------|
| 触发方式 | `tool.execute.after` hook | `PostToolUse` hook | FileSystemWatcher + onDidSave |
| 确定性 | 100%（hook 必触发） | 100%（hook 必触发） | ~100%（文件系统事件） |
| session/model | 有（chat.message hook） | 有 sessionId，无 model | 无（API 未暴露） |
| action 精度 | 高（从工具类型推断） | 高（tool_name 直接可用） | 中（从文件系统事件推断） |
| Token 开销 | 零 | 零（hook 是 shell 命令） | 零 |
| 用户配置 | opencode.json | settings.json hooks | VS Code settings |
| 是否需要额外进程 | 否 | 否（shell exec） | 否（扩展内嵌） |
| 阻塞 AI | 否（异步） | 是（hook 阻塞，有 timeout） | 否（异步） |

---

## 关键设计决策

### 1. PostToolUse vs PreToolUse

**选择 PostToolUse。** 原因：
- 工具执行成功后才上报，避免写入失败仍记录
- tool_response 确认操作成功
- 虽然阻塞 AI 等待 hook 返回，但 curl 是 5s 超时 + fire-and-forget

### 2. command vs http hook 类型

**选择 command。** 原因：
- command 可以灵活处理不同工具的 input 格式
- 可以做 Edit 工具的 fallback（从文件系统读取）
- http hook 直接 POST 原始 $ARGUMENTS，changez 服务需要适配新格式
- command 脚本可以随时修改，不需要改服务端

### 3. Hook 阻塞问题

**需要关注。** 原因：
- 每个 Write/Edit 调用都会触发一个同步 HTTP POST
- 如果 changez 服务短暂不可用，Claude Code 会最多等待 5 秒（`AbortSignal.timeout(5000)`）
- 批量编辑时（如大型重构），累积延迟可能显著降低吞吐量
- 实践中，5 秒超时很少实际触发（本地服务通常响应 <50ms）

**缓解方案**：如果阻塞成为问题，可将脚本改为后台执行实现真正的 fire-and-forget：
```bash
# 在 hook command 中使用 & 后台运行
"command": "changez-hook &"
```
代价是丢失响应状态，无法确认上报是否成功。

### 4. sessionId 传，model 不传

**传 sessionId，不传 model。** 原因：
- hook JSON 包含 `session_id`（实机验证），直接提取即可
- hook JSON 中没有 model 信息，Claude Code 未暴露
- sessionId 对 changez 有用（关联同一会话的变更）

**Phase 2 可选 workaround**：通过 `SessionStart` hook 捕获 model 信息并保存到临时文件，然后在 `PostToolUse` 中读取。但会增加脚本复杂度，Phase 1 保持简单。

### 5. Project 自动注册

OpenCode 插件和 Cursor 扩展均在启动时自动注册 project。Claude Code 方案利用 `SessionStart` hook 实现同样功能。

**方案**：统一脚本 `changez-hook` 同时处理 `SessionStart` 和 `PostToolUse` 事件，通过 `hook.tool_name` 是否存在来区分：

```jsonc
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "changez-hook",
            "timeout": 5000
          }
        ]
      }
    ],
    "PostToolUse": [
      // ... 现有配置
    ]
  }
}
```

`changez-hook` 统一脚本从 stdin 读取 hook JSON，根据事件类型分发处理：
- `SessionStart` → 提取 `cwd` 作为 `rootPath`，调用 `POST /api/projects`
- `PostToolUse` → 提取文件变更信息，调用 `POST /api/snapshot`

**幂等**：`POST /api/projects` 已存在返回 409，脚本静默忽略。
**不阻塞**：注册失败不影响后续 snapshot 上报（服务端 `ResolvePathsToProjects` 可兜底查找）。

### 6. 文件删除不处理（Phase 1）

**Phase 1 不处理。** 原因：
- Claude Code 的文件删除通常是 Bash(rm) 命令，不是独立的 Delete 工具
- FileChanged hook 可以捕获 unlink 但没有 content
- 低频操作，git 已经追踪

### 7. 文件扩展名过滤

脚本有 `MAX_FILE_SIZE` 大小限制，但**没有扩展名过滤**。AI 可能写入 `.jpg`、`.woff2`、`.gitignore` 等文件。

**当前策略**：不过滤扩展名，依赖服务端 SHA256 去重。changez 只追踪文本文件，二进制文件上报后会被服务端识别为无变化或跳过。

**Phase 2 可选优化**：在脚本中增加扩展名黑名单（`.png`, `.jpg`, `.gif`, `.woff2`, `.ttf`, `.ico`, `.zip`, `.tar.gz` 等），减少无效 HTTP 请求。

### 8. 多文件批量编辑

Claude Code 的 Write/Edit 工具在单次消息中编辑多个文件时，会为每个文件单独调用 → 触发多次 hook → 每个文件产生一次独立的 HTTP POST。

**示例**：AI 一次性修改 10 个文件 → 10 次 hook 触发 → 10 次 HTTP POST。

**优化方向**：changez 服务的 `SnapshotRequest` 支持 `files: [...]` 数组，单次请求可包含多个文件。理论上可设计缓冲脚本累积变更后再批量发送，但这与"始终 exit 0"的简单性目标相悖。Phase 1 保持简单，每次 hook 独立上报。

---

## 验证状态

### 已验证（实机测试 + 源码分析）

- [x] PostToolUse hook 的 JSON 实际格式 — 实机验证，包含 session_id, tool_name, tool_input, tool_response
- [x] Write 工具 tool_input 有完整 content — 实机验证
- [x] Edit 工具 tool_input 格式 — 实机验证，包含 file_path, old_string, new_string（diff 片段）
- [x] Edit 工具 tool_response 有 originalFile — 实机验证，包含编辑前完整文件内容
- [x] tool_response.type 区分 create/update — 实机验证
- [x] JSON 通过 stdin 传入（不是环境变量）— 源码 + 实机验证
- [x] Claude Code hooks 系统可用 — 实机验证
- [x] settings.json 的 hooks 字段被识别 — 实机验证
- [x] Write 工具无 originalFile / structuredPatch — 实机验证

### 未验证（源码确认）

- [ ] Hook 超时后 AI 行为 — 源码有 abort 机制，需实机验证
- [ ] 多文件写入时 hook 触发次数 — 预期每个工具调用一次

### 已知限制

- [x] `--bare` 模式禁用所有 hooks — `isEnvTruthy(CLAUDE_CODE_SIMPLE)` 确认

---

## 安装与使用

### 用户安装流程

```bash
# 1. 安装 changez-hook 统一脚本
cp client/claudecode/changez-hook.js ~/.local/bin/changez-hook
chmod +x ~/.local/bin/changez-hook

# 2. 配置环境变量（可选，脚本有默认值）
export CHANGEZ_URL="http://127.0.0.1:8760"
export CHANGEZ_LOG_FILE="/tmp/changez-claude.log"  # 可选，启用日志

# 3. 在 Claude Code settings 中添加 hooks 配置
# 编辑 ~/.claude/settings.json 或 .claude/settings.json

# 4. 重启 Claude Code
```

**前置条件**：Node.js ≥ 18（Claude Code 本身是 Node 进程，环境一定存在）。

### 脚本分发与版本管理

脚本随 changez 服务一起分发，版本号与服务版本一致。

| 维度 | 方案 |
|------|------|
| 版本管理 | 脚本版本号 = changez 服务版本号，统一部署 |
| 兼容性检查 | 脚本启动时从 `/api/health` 读取服务端版本，不匹配时日志告警 |
| 分发方式 | `~/.local/bin/` 用户手动安装，或 `changez install claudecode` 一键安装 |
| 升级路径 | changez 服务升级时，检查 `~/.local/bin/changez-hook` 版本，提示用户同步升级 |

### 与其他 PostToolUse hook 的兼容性

如果用户已配置其他 `PostToolUse` hook（代码质量检查、安全扫描等），多个 hook **串行执行**。changez-hook 脚本设计为快速返回（<50ms），不会显著影响其他 hook 的执行。

**注意**：如果其他 hook 执行时间过长（>5s），可能导致 Claude Code 的 hook timeout 触发，changez-hook 可能不会执行。

### 一键安装（未来）

```bash
changez install claudecode --url http://127.0.0.1:8760
```

### 故障排查

| 问题 | 排查步骤 |
|------|---------|
| hook 不触发 | 1. 确认未使用 `--bare` 模式<br>2. 检查 `settings.json` 语法是否正确<br>3. 确认 matcher `"Write\|Edit"` 匹配目标工具 |
| snapshot 上报失败 | 1. 查看日志文件 `CHANGEZ_LOG_FILE`<br>2. 确认 changez 服务运行中（`curl http://127.0.0.1:8760/health`）<br>3. 确认 token 匹配（服务端有 token 时） |
| 文件未追踪 | 1. 确认项目已注册（`GET /api/projects`）<br>2. 确认文件路径在 project 的 `rootPath` 下<br>3. 检查文件是否超过 `MAX_FILE_SIZE` |
| 脚本执行报错 | 1. 确认 Node.js ≥ 18<br>2. 检查 `changez-hook` 是否有执行权限<br>3. 查看 stderr 输出 |

**快速验证**：
```bash
# 手动触发脚本测试（PostToolUse）
echo '{"tool_name":"Write","tool_input":{"file_path":"/tmp/test.txt","content":"hello"},"tool_response":{"type":"create"},"session_id":"test"}' | ~/.local/bin/changez-hook
# 手动触发脚本测试（SessionStart）
echo '{"cwd":"/home/user/project"}' | ~/.local/bin/changez-hook
# 检查日志文件
cat /tmp/changez-claude.log
```

---

## 实现计划（待确认）

### Phase 1: 核心功能

1. 编写 `changez-hook.js` Node.js 脚本
2. 编写 hooks 配置模板（`.claude/settings.json`）
3. 实际测试验证 hook JSON 格式
4. 处理 Edit 工具的 content fallback（`fs.readFileSync`）
5. 编写安装文档

### Phase 2: 优化（可选）

1. 文件删除支持（FileChanged hook + unlink）
2. session/model 追踪（SessionStart hook + 临时文件）
3. 一键安装脚本
4. 批量上报（多个文件变更合并为一次请求）
