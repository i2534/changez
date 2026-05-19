# Claude Code (wps_claude) 接入设计文档

> 创建日期：2026-05-18
> 状态：讨论中

---

## 目标

在 Claude Code / wps_claude 中自动追踪文件变更，AI 修改文件后自动上报 snapshot 到 changez 服务。用户零操作，完全无人参与。

---

## 架构总览

```
┌─────────────────────────────────────────────────────┐
│  Claude Code / wps_claude                           │
│                                                     │
│  AI ──Write/Edit──▶ Tool Execution                  │
│                      │                              │
│                      ▼ PostToolUse hook              │
│              ┌──────────────────┐                    │
│              │ Hook Matcher:    │                    │
│              │ "Write|Edit"     │                    │
│              │ Type: command    │                    │
│              │ Command:         │                    │
│              │ changez-report   │                    │
│              │ $ARGUMENTS JSON  │                    │
│              └────────┬─────────┘                    │
│                       │ shell exec                   │
│                       ▼                              │
│              ┌──────────────────┐                    │
│              │ changez-report   │                    │
│              │ (shell 脚本)     │                    │
│              │ 解析 $ARGUMENTS  │                    │
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

从 wps_claude cli.js 逆向分析得到的关键事件：

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
// Claude Code settings（~/.wps_claude/settings.json 或 .claude/settings.json）
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "changez-report",
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
| `command` | 执行 shell 命令 | **JSON 通过 stdin 传入**（`child.stdin.write(jsonInput)`） |
| `http` | POST webhook | POST body = JSON |
| `prompt` | LLM 评估 | 命令字符串中 `$ARGUMENTS` 占位符被替换 + stdin |
| `agent` | 完整 agent | 命令字符串中 `$ARGUMENTS` 占位符被替换 + stdin |

**command 类型通过 stdin 接收 JSON**，脚本用 `cat` 或 `read` 从 stdin 读取即可。

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

**重要：** changez-report 不需要返回控制 JSON，只需 fire-and-forget。

### 关键限制

1. `CLAUDE_CODE_SIMPLE=1`（即 `--bare` 模式）会禁用所有 hooks
2. `http` 类型不支持 `SessionStart` 和 `Setup` 事件
3. Hook 执行是阻塞的（有 timeout 保护，默认 10s）
4. matcher 支持管道分隔的模式匹配：`"Write|Edit|Bash"`

---

## 方案：PostToolUse Hook + Shell 脚本

### 为什么选择 PostToolUse + command

| 方案 | 优点 | 缺点 |
|------|------|------|
| **PostToolUse + command** | 工具执行成功后触发，有完整 input+output，无需额外读取文件 | hook 阻塞 AI 等待返回 |
| PostToolUse + http | 直接 POST，无需中间脚本 | http hook 不改造 payload 格式，changez 服务需适配 |
| PreToolUse | 写入前拦截 | 工具可能失败，文件未实际写入 |
| FileChanged | 文件系统级监听 | 只有 file_path 无 content，需额外读取 |
| MCP tool | 标准 MCP 集成 | AI 不会自动调用，需 Rule/Skill 引导，不可靠 |

**结论：PostToolUse + command 类型最可靠。**

### Hook 输入 JSON（实机验证）

PostToolUse hook 通过 **stdin** 接收 JSON，实际格式：

```json
{
  "session_id": "06e8062a-603e-45be-affb-92674aef3850",
  "transcript_path": "/home/lan/.wps_claude/projects/-home-lan-workspace-go-changez/06e8062a.jsonl",
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

**Edit 工具（源码确认，未实机验证）：**
- `tool_input.file_path` — 文件绝对路径
- `tool_input.old_string` / `tool_input.new_string` — diff 片段（非完整内容）
- `tool_response.originalFile` — 编辑前的完整内容
- **没有编辑后的完整 content**，需要从文件系统读取

---

## changez-report 脚本设计

### 位置

```
~/.local/bin/changez-report
```

或随 changez 服务一起分发：

```
changez-data/changez-report    # 与服务数据同级
```

### 脚本逻辑

```bash
#!/usr/bin/env bash
set -euo pipefail

# 配置（从环境变量或配置文件读取）
CHANGEZ_URL="${CHANGEZ_URL:-http://127.0.0.1:8760}"
CHANGEZ_TOKEN="${CHANGEZ_TOKEN:-}"
CHANGEZ_SOURCE="${CHANGEZ_SOURCE:-claude_code}"

# JSON 通过 stdin 传入
ARGS=$(cat)

# 提取字段（使用 jq）
TOOL_NAME=$(echo "$ARGS" | jq -r '.tool_name // empty')

# 只处理 Write 和 Edit 工具
case "$TOOL_NAME" in
  Write|Edit) ;;
  *) exit 0 ;;  # 忽略其他工具
esac

# 提取文件路径
FILE_PATH=$(echo "$ARGS" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# 提取 action（tool_response.type 精确区分 create/update）
ACTION=$(echo "$ARGS" | jq -r '.tool_response.type // "update"')

# 提取 content 和 session_id
SESSION_ID=$(echo "$ARGS" | jq -r '.session_id // empty')
CONTENT=$(echo "$ARGS" | jq -r '.tool_input.content // empty')

# Edit 工具没有完整 content 时，从文件系统读取编辑后的文件
if [ -z "$CONTENT" ] && [ -f "$FILE_PATH" ]; then
  CONTENT=$(cat "$FILE_PATH")
fi

# 构建请求体
BODY=$(jq -n \
  --arg source "$CHANGEZ_SOURCE" \
  --arg sid "$SESSION_ID" \
  --arg path "$FILE_PATH" \
  --arg content "$CONTENT" \
  --arg action "$ACTION" \
  '{
    source: $source,
    sessionId: $sid,
    files: [{
      path: $path,
      content: $content,
      action: $action
    }]
  }')

# 发送 snapshot（fire-and-forget）
curl -s --max-time 5 \
  -X POST "${CHANGEZ_URL}/api/snapshot" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${CHANGEZ_TOKEN}" \
  -d "$BODY" > /dev/null 2>&1 || true

exit 0
```

### 关键设计点

1. **JSON 从 stdin 读取**：`ARGS=$(cat)` 接收 Claude Code 传入的 JSON
2. **fire-and-forget**：curl 失败不影响 Claude Code（`|| true`）
3. **超时保护**：`--max-time 5` 防止 hook 卡住
4. **action 精确区分**：`tool_response.type` 返回 `"create"` 或 `"update"`，不需要服务端双重判断
5. **Edit 工具 fallback**：如果 tool_input 没有完整 content，从文件系统读取
6. **jq -n 构建 JSON**：安全序列化，避免 shell 注入
7. **sessionId 可用**：hook JSON 包含 session_id，可以传给 changez
8. **始终 exit 0**：不阻塞 Claude Code 执行流

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
            "command": "changez-report",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

### 方案 B：全局配置

在 `~/.wps_claude/settings.json` 中添加 hooks 配置，对所有项目生效。

### 方案 C：一键安装脚本

提供安装脚本，用户执行一次即可自动配置：

```bash
# changez install claude-code
# 自动：
# 1. 安装 changez-report 到 ~/.local/bin/
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

### 简化方案：只用 PostToolUse，不处理删除

Phase 1 只处理 Write/Edit，不处理文件删除。文件删除是低频操作，且可以通过 git 追踪。

---

## 错误处理

| 场景 | 行为 |
|------|------|
| changez 服务不可达 | curl 超时/失败，`|| true` 忽略 |
| jq 解析失败 | 脚本 exit 0，不阻塞 Claude Code |
| 文件读取失败（Edit fallback） | 跳过该文件 |
| $ARGUMENTS 为空 | 脚本 exit 0 |
| hook timeout | Claude Code 自动跳过，不影响主流程 |

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

**可接受。** 原因：
- curl --max-time 5 限制最长等待
- `|| true` 保证快速返回
- 正常情况 curl 在几十毫秒内完成
- 服务不可达时 5s 超时后继续

### 4. sessionId 传，model 不传

**传 sessionId，不传 model。** 原因：
- hook JSON 包含 `session_id`（实机验证），直接提取即可
- hook JSON 中没有 model 信息，Claude Code 未暴露
- sessionId 对 changez 有用（关联同一会话的变更）

### 5. 文件删除不处理（Phase 1）

**Phase 1 不处理。** 原因：
- Claude Code 的文件删除通常是 Bash(rm) 命令，不是独立的 Delete 工具
- FileChanged hook 可以捕获 unlink 但没有 content
- 低频操作，git 已经追踪

---

## 验证状态

### 已验证（实机测试 + 源码分析）

- [x] PostToolUse hook 的 JSON 实际格式 — 实机验证，包含 session_id, tool_name, tool_input, tool_response
- [x] Write 工具 tool_input 有完整 content — 实机验证
- [x] tool_response.type 区分 create/update — 实机验证
- [x] JSON 通过 stdin 传入（不是环境变量）— 源码 + 实机验证
- [x] wps_claude hooks 系统可用 — 实机验证
- [x] settings.json 的 hooks 字段被识别 — 实机验证

### 未验证（源码确认）

- [ ] Edit 工具的 tool_input 格式 — 源码确认为 diff 片段，需实机验证
- [ ] Hook 超时后 AI 行为 — 源码有 abort 机制，需实机验证
- [ ] 多文件写入时 hook 触发次数 — 预期每个工具调用一次

### 已知限制

- [x] `--bare` 模式禁用所有 hooks — `isEnvTruthy(CLAUDE_CODE_SIMPLE)` 确认

---

## 安装与使用

### 用户安装流程

```bash
# 1. 安装 changez-report 脚本
cp changez-report ~/.local/bin/
chmod +x ~/.local/bin/changez-report

# 2. 配置环境变量（可选，脚本有默认值）
export CHANGEZ_URL="http://127.0.0.1:8760"

# 3. 在 Claude Code settings 中添加 hooks 配置
# 编辑 ~/.wps_claude/settings.json 或 .claude/settings.json

# 4. 重启 Claude Code / wps_claude
```

### 一键安装（未来）

```bash
changez install claude-code --url http://127.0.0.1:8760
```

---

## 实现计划（待确认）

### Phase 1: 核心功能

1. 编写 `changez-report` shell 脚本
2. 编写 hooks 配置模板
3. 实际测试验证 $ARGUMENTS 格式
4. 处理 Edit 工具的 content fallback
5. 编写安装文档

### Phase 2: 优化（可选）

1. 文件删除支持（FileChanged hook + unlink）
2. session/model 追踪（SessionStart hook + 临时文件）
3. 一键安装脚本
4. 批量上报（多个文件变更合并为一次请求）
