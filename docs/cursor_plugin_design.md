# Cursor (VS Code) Plugin 设计文档

> 创建日期：2026-05-18
> 状态：讨论中

---

## 目标

在 Cursor IDE（基于 VS Code）中自动追踪文件变更，文件保存后自动上报 snapshot 到 changez 服务。用户零操作，完全无人参与。

---

## 架构总览

```
┌─────────────────────────────────────────────────────┐
│  Cursor / VS Code                                   │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  changez VS Code Extension                    │  │
│  │                                               │  │
│  │  config: VS Code settings (url/token)         │  │
│  │  activate → POST /api/projects (自动注册)      │  │
│  │  FileSystemWatcher → 监听文件变更              │  │
│  │                     per-file debounce         │  │
│  │                     读取文件完整内容            │  │
│  │                     POST → /api/snapshot      │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Cursor AI ──edit/write──▶ Filesystem               │
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

**核心原则：** 扩展与 changez 服务完全解耦。扩展按 VS Code 规范配置，仅通过 HTTP API 与服务交互。changez 服务端不需要为扩展做任何配置或代码改动。

---

## 扩展结构

标准 VS Code 扩展结构：

```
client/cursor/
  package.json          # 扩展清单（name, activationEvents, configuration）
  tsconfig.json         # TypeScript 配置
  src/
    extension.ts        # 扩展入口（activate/deactivate）
    watcher.ts          # FileSystemWatcher + debounce 逻辑
    http.ts             # HTTP 请求辅助
    config.ts           # 配置加载
```

---

## 配置

### VS Code Settings 配置

在 `package.json` 中声明 `configuration`，用户通过 Settings UI 或 `.vscode/settings.json` 配置：

```jsonc
// .vscode/settings.json（项目级）
// 或 ~/.config/Code/User/settings.json（全局）
{
  "changez.url": "http://127.0.0.1:8760",
  "changez.token": "",
  "changez.source": "cursor",
  "changez.projectName": "",         // 可选，省略则用 workspace folder 的 basename
  "changez.debounceMs": 500,         // per-file debounce 间隔（毫秒）
  "changez.maxFileSize": 10485760,   // 最大文件大小（字节），默认 10MB
  "changez.logLevel": "info"         // debug / info / warn / error
}
```

### 配置项说明

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `changez.url` | string | 是 | `http://127.0.0.1:8760` | changez 服务地址 |
| `changez.token` | string | 否 | `""` | Bearer token（服务未设 token 时可省略） |
| `changez.source` | string | 否 | `"cursor"` | source 名称 |
| `changez.projectName` | string | 否 | workspace folder basename | project 名称 |
| `changez.debounceMs` | number | 否 | `500` | per-file debounce 间隔（毫秒） |
| `changez.maxFileSize` | number | 否 | `10485760` | 跳过超过此大小的文件 |
| `changez.logLevel` | string | 否 | `"info"` | 日志级别 |

---

## Project 自动注册

### 时机

扩展激活时（`activate` 函数执行时）自动注册 project。VS Code 扩展在用户首次使用扩展功能时激活（`activationEvents: ["workspaceContains:**/*"]` 或直接 `onStartupFinished`）。

### 流程

1. 取 `vscode.workspace.workspaceFolders[0].uri.fsPath`（workspace 根目录绝对路径）
2. 构造 project name：配置中的 `projectName`，若无则用 `path.basename(workspaceRoot)`
3. `POST /api/projects` → `{ rootPath: workspaceRoot, name: projectName }`
4. 如果已存在（409），静默忽略（幂等）
5. 如果注册失败（网络错误等），记录日志但不阻塞扩展加载

### 多 workspace 场景

VS Code 支持 multi-root workspace（同时打开多个文件夹）。此时为每个 workspace folder 分别注册 project：

```typescript
for (const folder of vscode.workspace.workspaceFolders ?? []) {
  await registerProject(folder.uri.fsPath);
}
```

---

## 文件变更检测

### FileSystemWatcher

使用 VS Code API `vscode.workspace.createFileSystemWatcher` 监听文件变更：

```typescript
// 监听整个 workspace 的 .ts, .go, .py 等文本文件
const watcher = vscode.workspace.createFileSystemWatcher(
  '**/*',  // 匹配所有文件
  false,   // 忽略创建事件
  false,   // 忽略变更事件（用 didSave 代替）
  true     // 忽略删除事件
);
```

### 上报时机：onDidSaveTextDocument

**不直接使用 FileSystemWatcher 的 onDidChange**，而是使用 `vscode.workspace.onDidSaveTextDocument`：

- 文件保存时才触发，不会在编辑过程中频繁触发
- 保存时文件内容已是最终状态，无需 debounce
- 覆盖 Cursor AI 写入文件 + 用户手动保存两种场景

**为什么不用 FileSystemWatcher 的 onDidChange：**

Cursor AI 修改文件时会产生多次文件系统事件（临时文件、重命名等），即使加了 debounce 仍有漏报或重复风险。`onDidSaveTextDocument` 是编辑器级别的保存事件，语义更清晰。

**但 Cursor AI 写入文件不一定触发 save 事件。** 所以实际采用双通道：

1. **主通道：`onDidSaveTextDocument`** —— 编辑器保存时触发（用户手动保存 + AI 编辑后自动保存）
2. **兜底通道：FileSystemWatcher `onDidChange`** —— 文件系统级变更，带 per-file debounce

两个通道上报到同一个服务端，SHA256 去重天然去重。

### Debounce 机制

FileSystemWatcher 兜底通道需要 per-file debounce：

```typescript
const debounceTimers = new Map<string, NodeJS.Timeout>();

function scheduleSnapshot(filePath: string) {
  // 清除该文件的旧定时器
  const existing = debounceTimers.get(filePath);
  if (existing) clearTimeout(existing);

  // 设置新定时器
  const timer = setTimeout(async () => {
    debounceTimers.delete(filePath);
    await reportSnapshot(filePath);
  }, debounceMs);

  debounceTimers.set(filePath, timer);
}
```

- 同文件短时间多次变更只上报一次（最终状态）
- debounce 间隔可配置（默认 500ms）
- 不同文件独立计时，互不影响

---

## 请求/响应格式

### Snapshot 请求

```json
POST http://127.0.0.1:8760/api/snapshot
Authorization: Bearer <token>
Content-Type: application/json

{
  "source": "cursor",
  "files": [
    {
      "path": "/home/lan/workspace/go/changez/internal/handler/snapshot.go",
      "content": "package handler\nimport ...",
      "action": "update"
    }
  ]
}
```

- `sessionId` / `model` / `message` 不传（Cursor 无相关 API）
- `action` 统一传 `"update"`（服务端有双重判断兜底）
- 文件删除通过 FileSystemWatcher 的 `onDidDelete` 检测，传 `"delete"` + 空 content

### 响应

```json
{
  "results": [
    { "path": "...", "status": "captured", "versionId": 42 }
  ],
  "summary": { "captured": 1, "unchanged": 0, "errors": 0 }
}
```

---

## 关键设计决策

### 1. onDidSaveTextDocument + FileSystemWatcher 双通道

**选择双通道**。原因：
- `onDidSaveTextDocument`：语义清晰，编辑器保存时触发
- FileSystemWatcher：兜底，覆盖 AI 直接写文件不经过编辑器 save 的场景
- 服务端 SHA256 去重，双通道不会产生重复数据

### 2. Debounce 策略

**per-file debounce（500ms）**。原因：
- FileSystemWatcher 事件可能密集（一个文件写入产生多个事件）
- 500ms 足够覆盖大多数连续写入场景
- 不同文件独立计时，不互相阻塞

### 3. 同步 vs 异步上报

**异步（fire-and-forget）**。原因：
- 不阻塞编辑器操作
- changez 上报失败不影响正常编码工作
- 失败时记录日志，不向用户报错

### 4. action 区分

**统一传 `"update"`**。原因：
- `onDidSaveTextDocument` 无法区分 create/update
- FileSystemWatcher 可以区分 create/change/delete
- 服务端有双重判断兜底（create+已存在→update，update+不存在→create）
- FileSystemWatcher 的 `onDidCreate` 传 `"create"`，`onDidDelete` 传 `"delete"`

### 5. 文件过滤

**跳过大文件 + 二进制文件**。原因：
- changez 只追踪文本文件
- 超过 `maxFileSize`（默认 10MB）的文件跳过
- 通过文件扩展名初步过滤二进制文件（`.png`, `.jpg`, `.woff2` 等）

### 6. 与 OpenCode 插件的关系

**完全独立，代码可复用**。原因：
- 核心逻辑（HTTP 请求、配置类型、snapshot 格式）可以共享
- VS Code 扩展 API 和 OpenCode 插件 API 不同，入口代码需分别编写
- HTTP 辅助函数和类型定义可以提取为共享模块

### 7. session/model 不传

**VS Code 扩展方案：不传 sessionId 和 model**。原因：
- Cursor 没有暴露 session/model 相关的扩展 API
- changez 服务的 snapshot 接口中这些字段是可选的
- source 标记为 `"cursor"` 已足够区分来源

**Cursor Hooks 方案：可以传 sessionId 和 model**。`sessionStart` 钩子提供 `session_id` 和 `model`，`afterFileEdit` 钩子也携带 `conversation_id` 和 `model` 字段。

---

## 扩展激活与生命周期

### Activation Events

```json
{
  "activationEvents": [
    "onStartupFinished"
  ]
}
```

`onStartupFinished`：VS Code 启动完成后激活，不阻塞启动。

### Deactivate

扩展卸载时清理资源：

```typescript
export function deactivate(): Thenable<void> {
  watcher.dispose();           // 停止文件监听
  for (const timer of debounceTimers.values()) {
    clearTimeout(timer);       // 清除 pending 的定时器
  }
  return Promise.resolve();
}
```

---

## 错误处理

| 场景 | 行为 |
|------|------|
| changez 服务不可达 | 静默失败，记录日志 |
| token 认证失败 | 静默失败，记录日志 |
| 文件读取失败（权限/不存在） | 跳过该文件 |
| 文件大小超限 | 跳过该文件 |
| project 注册失败（网络错误） | 记录日志，不阻塞扩展加载 |
| project 已存在（409） | 静默忽略（幂等） |
| 网络超时（5s） | 放弃本次上报 |
| 文件已被删除（delete 事件后读取失败） | 正常传 delete action + 空 content |

---

## 日志

扩展使用 VS Code 的 `OutputChannel` 输出日志：

```typescript
const outputChannel = vscode.window.createOutputChannel("Changez");

function log(level: string, message: string) {
  if (LOG_LEVELS[level] > LOG_LEVELS[cfg.logLevel]) return;
  outputChannel.appendLine(`[${level.toUpperCase()}] ${message}`);
}
```

用户可以通过 "View → Output → Changez" 查看日志。

日志级别：

| 级别 | 内容 |
|------|------|
| `debug` | 每次 snapshot 请求详情（文件路径、debounce 触发） |
| `info` | 扩展激活、project 注册成功、snapshot 上报成功 |
| `warn` | snapshot 失败、文件读取失败、服务不可达 |
| `error` | 不应发生的异常（代码 bug、未捕获错误） |

---

## 安装方式

### 开发模式

```bash
cd client/cursor
npm install
npm run watch    # TypeScript 编译 + 监听
# 在 VS Code 中 F5 启动调试扩展
```

### 用户安装

1. 打包为 `.vsix`：`npm run package`
2. 用户通过 `Extensions → ... → Install from VSIX` 安装
3. 或在 VS Code Extensions Marketplace 发布（可选）

---

## 设计决策汇总

| # | 问题 | 结论 |
|---|------|------|
| 1 | 扩展形式 | 标准 VS Code 扩展（.vsix） |
| 2 | 变更检测 | onDidSaveTextDocument + FileSystemWatcher 双通道 |
| 3 | debounce | per-file debounce，默认 500ms |
| 4 | session/model | 不传（Cursor 无相关 API） |
| 5 | 配置方式 | VS Code settings（package.json configuration） |
| 6 | action 区分 | save 传 update，create/delete 事件对应传 create/delete |
| 7 | 同步/异步 | 异步 fire-and-forget |
| 8 | 文件过滤 | 跳过 >10MB + 二进制扩展名 |
| 9 | 日志输出 | VS Code OutputChannel |
| 10 | 与 OpenCode 插件 | 独立扩展，HTTP 辅助代码可复用 |
| 11 | Cursor Hooks | 作为补充方案，精确追踪 AI 编辑（详见下文） |

---

## 与 OpenCode 插件对比

| 维度 | OpenCode 插件 | Cursor VS Code 扩展 | Cursor Hooks |
|------|--------------|-------------------|--------------|
| 触发方式 | `tool.execute.after` hook | FileSystemWatcher + onDidSave | `afterFileEdit` 等钩子 |
| 确定性 | 100%（hook 必触发） | 100%（文件系统事件必触发） | 100%（Agent 编辑必触发） |
| session/model | 有（chat.message hook） | 无（Cursor 未暴露 API） | 有（sessionStart 钩子） |
| action 精度 | 高（从工具类型推断） | 中（从文件系统事件推断） | 中（有 diff 但无法区分 create/update） |
| 文件删除捕获 | ✅ bash rm 命令 | ✅ FileSystemWatcher onDidDelete | ❌ 无 afterFileDelete 钩子 |
| Subagent 追踪 | ❌ 不支持 | ❌ 不支持 | ✅ subagentStart/Stop |
| Shell 命令文件变更 | ⚠️ 仅 rm | ❌ 不覆盖 | ⚠️ afterShellExecution（仅有输出文本） |
| Token 开销 | 零 | 零 | 零 |
| 用户配置 | opencode.json | VS Code settings | hooks.json（可版本控制） |
| 代码量 | 单文件 ~400 行 | 多文件 ~600 行 | 脚本 ~100 行 + JSON 配置 |
| 手动编辑覆盖 | ❌ 不支持 | ✅ 覆盖 | ❌ 不覆盖 |
| TUI 状态展示 | ✅ 有 | 可扩展 | ❌ 不适用 |

### OpenCode 插件的 Action 判定（参考）

OpenCode 插件通过 `tool.execute.after` 钩子拦截 5 种工具，从工具类型精确推断 action：

| 工具 | 提取方式 | 判定 action |
|------|---------|:---:|
| `write` / `edit` | `args.filePath` 或 `args.path` | `update` |
| `multiedit` | `args.edits[].path` / `changes[].path` | `update` |
| `apply_patch` | 正则解析 `** Add/Update/Delete File:` | `create`/`update`/`delete` |
| `bash` | 正则 `\brm\s+...` | `delete` |

服务端有双重判断兜底（`snapshotSingleFile`）：create+已存在→update，update+不存在→create。

### Cursor Hooks 的 Action 判定

Cursor Hooks 的 `afterFileEdit` 只在新文件创建或修改时触发，**不区分 create 和 update**，**文件删除不会触发**。

| 场景 | Hooks 能做什么 | 服务端兜底 |
|------|:---:|:---:|
| 首次创建文件 | ✅ 触发，统一传 `update` | ✅ 服务端检测到文件不存在→自动修正为 `create` |
| 修改已有文件 | ✅ 触发，传 `update` | — |
| 删除文件 | ❌ 不触发 | ❌ 无法捕获 |
| apply_patch 新增文件 | ✅ 触发（Write 工具会触发 afterFileEdit） | — |
| apply_patch 删除文件 | ❌ 不触发（Delete 操作不经过 Write） | ❌ 无法捕获 |
| bash rm 删除文件 | ❌ 不触发 | ❌ 无法捕获 |

### Hooks 相比 OpenCode 插件的差距总结

| 层级 | 评估 | 说明 |
|------|:---:|------|
| **核心功能（AI 编辑追踪 + session/model 上下文）** | ✅ 完全覆盖 | `afterFileEdit` + `sessionStart` 可覆盖 AI 编辑和上下文 |
| **Action 精度（create vs update vs delete）** | ⚠️ 较弱 | 无法区分 create/update，无法捕获 delete。create/update 可由服务端兜底修正，但 delete 无法弥补 |
| **Shell 命令文件变更** | ⚠️ 两者都弱 | OpenCode 仅捕获 rm；Hooks 有 `afterShellExecution` 但只有命令+输出文本，不直接告知哪些文件被修改 |
| **Subagent 追踪** | ✅ Hooks 独有 | `subagentStop` 提供 `modified_files` 列表，OpenCode 无此能力 |
| **用户手动编辑** | ❌ 两者都不覆盖 | 需要 VS Code 扩展方案 |

---

## 待验证

### VS Code 扩展 API 细节

1. `onDidSaveTextDocument` 是否能捕获 Cursor AI 的文件写入？Cursor AI 可能绕过编辑器直接写文件，此时只有 FileSystemWatcher 能捕获。
2. `onStartupFinished` activation event 在 Cursor 中是否正常工作？Cursor 是 VS Code fork，大部分 API 兼容，但需实测。
3. 多 workspace folder 时，每个 folder 是否独立触发 activate？还是只触发一次？

### FileSystemWatcher 性能

1. 大项目（数万文件）下 watcher 的内存占用和 CPU 开销是否在可接受范围内？VS Code 自身也用同样的 API，应该没问题。
2. debounce 500ms 是否足够？如果 AI 在 500ms 内写完一个文件又开始写另一个，两个文件是独立计时互不影响的。

### Cursor Hooks 细节

1. `afterFileEdit` 钩子是否能覆盖所有 Agent 文件编辑场景？包括 subagent 的文件编辑。
2. `sessionStart` 返回的 `env` 是否能在后续所有钩子中正确传递？Cursor 文档说是的，但需实测。如果不可靠，每个 hook 脚本需从输入 JSON 自行提取 `conversation_id`。
3. 项目级 hooks（`.cursor/hooks.json`）是否只在"受信任的工作区"中运行？如果是，首次打开时需要用户确认。
4. Hooks 脚本的执行超时（默认值？）是否足够完成 Bun 启动 + HTTP 请求？
5. `afterFileEdit` 的 `edits` 字段确认只包含变更片段（diff），不包含完整文件内容。验证：AI 大范围重写文件时，edits 是否仍可靠。
6. 当 Agent 通过 shell 命令（如 `echo "x" > file.txt`）修改文件时，是否会触发 `afterFileEdit`？还是只能靠 `afterShellExecution`？
7. `afterFileEdit` 是否会在文件删除时触发？预期不会，但需确认。
8. `conversation_id` 与 `session_id` 是否确为同一值？Cursor 文档如是说，但需实机验证。
9. Bun 的 `bun run` 冷启动耗时是否可接受？afterFileEdit 高频触发时，Bun 的进程启动开销是否会造成延迟。

---

## 实现计划（待确认）

### Phase 1: 核心功能

1. 创建 VS Code 扩展骨架（package.json, tsconfig.json）
2. 实现配置加载（VS Code settings）
3. 实现 HTTP 请求辅助（复用 OpenCode 插件逻辑）
4. 实现 Project 自动注册
5. 实现 onDidSaveTextDocument + FileSystemWatcher 双通道
6. 实现 per-file debounce
7. 实现文件过滤（大小 + 二进制扩展名）
8. 实现日志输出（OutputChannel）

### Phase 2: 优化（可选）

1. 状态栏指示器（显示 changez 连接状态）
2. 命令面板命令（手动触发 snapshot、查看最近上报记录）
3. 批量合并上报（短时间内多个文件变更合并为一次请求）

---

## Cursor Hooks 集成方案

> Cursor 原生支持的 Hooks 机制，通过自定义脚本观察和控制 Agent 循环。
> 相比 VS Code 扩展方案，Hooks 更轻量、更精确地追踪 AI 编辑行为。

### 架构总览

```
┌─────────────────────────────────────────────────────┐
│  Cursor IDE                                         │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  .cursor/hooks.json                           │  │
│  │  ─────────────────────────────────           │  │
│  │  afterFileEdit  → hooks/report.ts            │  │
│  │  sessionStart   → hooks/session-init.ts      │  │
│  │  afterAgentResponse → hooks/audit.ts         │  │
│  │  subagentStart/Stop → hooks/audit.ts         │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Cursor AI Agent ──edit/file/shell──▶ Filesystem   │
│       │                                 │           │
│       │ Hooks 拦截 (Bun runtime)         │           │
│       ▼                                 ▼           │
│  ┌──────────────┐              ┌──────────────┐    │
│  │ report.ts    │              │  Filesystem  │    │
│  │ (AI 编辑)    │              │  (直接写入)   │    │
│  └──────┬───────┘              └──────────────┘    │
└─────────┼──────────────────────────────────────────┘
          │ HTTP POST
          ▼
      ┌────────────────┐
      │  changez       │
      │  service       │
      │  :8760/api/    │
      │  (无需任何改动) │
      └────────────────┘
```

**核心优势：**
- 无需编写 VS Code 扩展，只需 TypeScript 脚本 + JSON 配置
- `afterFileEdit` 钩子直接提供编辑详情（old/new string），精确追踪变更
- `sessionStart` 钩子提供 session_id、model、composer_mode 等上下文
- 项目级（`.cursor/hooks.json`）或用户级（`~/.cursor/hooks.json`）配置
- TypeScript 提供类型安全、可靠的文件 I/O 和 HTTP 调用

### 配置

在项目根目录创建 `.cursor/hooks.json`：

```jsonc
{
  "version": 1,
  "hooks": {
    /* AI 文件编辑后触发 —— 核心上报入口 */
    "afterFileEdit": [
      {
        "command": "bun run .cursor/hooks/report.ts",
        "timeout": 10
      }
    ],
    /* 会话开始时触发 —— 自动注册 project + 注入上下文 */
    "sessionStart": [
      {
        "command": "bun run .cursor/hooks/session-init.ts",
        "timeout": 10
      }
    ],
    /* Agent 响应后触发 —— 审计/分析 */
    "afterAgentResponse": [
      {
        "command": "bun run .cursor/hooks/audit.ts",
        "timeout": 5
      }
    ],
    /* Subagent 生命周期 —— 追踪子任务 */
    "subagentStart": [
      { "command": "bun run .cursor/hooks/audit.ts", "timeout": 5 }
    ],
    "subagentStop": [
      { "command": "bun run .cursor/hooks/audit.ts", "timeout": 5 }
    ],
    /* Shell 命令执行前后 —— 可选，追踪命令级变更 */
    "beforeShellExecution": [
      { "command": "bun run .cursor/hooks/audit.ts", "timeout": 5 }
    ],
    "afterShellExecution": [
      { "command": "bun run .cursor/hooks/audit.ts", "timeout": 5 }
    ]
  }
}
```

### 运行时依赖

- **Bun**（`bun.run` 安装）：TypeScript 脚本的运行时。Cursor 官方文档推荐在需要类型化 JSON、可靠文件 I/O 和 HTTP 调用时使用 Bun。
- 项目已有 `client/opencode/` 下的 TypeScript 代码基础，类型定义和 HTTP 辅助逻辑可复用。

### 核心钩子详解

#### afterFileEdit（核心：文件变更上报）

Agent 完成文件编辑后触发。这是文件变更追踪的主要入口。

**输入：**

```json
{
  "conversation_id": "conv-xxx",
  "generation_id": "gen-yyy",
  "model": "claude-sonnet-4-20250514",
  "hook_event_name": "afterFileEdit",
  "workspace_roots": ["/home/lan/workspace/go/changez"],
  "file_path": "/home/lan/workspace/go/changez/internal/handler/snapshot.go",
  "edits": [
    {
      "old_string": "package handler\nimport \"fmt\"",
      "new_string": "package handler\nimport (\n\t\"fmt\"\n\t\"log\"\n)"
    }
  ]
}
```

**输出：** 无需返回特定字段（fire-and-forget）。

**⚠️ edits 字段说明：** `edits` 是 diff 片段数组（`old_string`/`new_string` 对），**不包含完整文件内容**。即使 AI 只改了一行，`edits` 也只有那一行的 old/new，没有上下文。因此文件内容必须从文件系统读取，`edits` 仅用于构建 diff 和 enrich metadata。

**关键优势 vs FileSystemWatcher：**
- 可靠：Agent 编辑必触发，不存在文件系统事件的竞态问题
- 上下文丰富：自带 `conversation_id`、`model` 等信息
- 无需 debounce：一次编辑一次触发，不会密集刷屏

#### sessionStart（会话初始化）

创建新的 composer 会话时触发。以 fire-and-forget 方式运行，不阻塞 Agent 循环。

**输入：**

```json
{
  "session_id": "conv-xxx",
  "model": "claude-sonnet-4-20250514",
  "is_background_agent": false,
  "composer_mode": "agent",
  "workspace_roots": ["/home/lan/workspace/go/changez"],
  "user_email": "user@example.com"
}
```

**输出：**

```json
{
  "env": {
    "CHANGEZ_URL": "http://127.0.0.1:8760",
    "CHANGEZ_TOKEN": "xxx"
  },
  "additional_context": "You are working on the changez project."
}
```

**用途：**
- 自动注册 project（`POST /api/projects`）
- 通过 `env` 为后续所有 hook 设置环境变量（CHANGEZ_URL、CHANGEZ_TOKEN 等）
- 通过 `additional_context` 注入对话上下文
- **session_id → conversation_id 映射**：`sessionStart` 的 `session_id` 与 `afterFileEdit` 的 `conversation_id` 是同一个值（Cursor 文档确认）。可通过 `env` 传递，使后续 hook 无需重复解析。

**⚠️ 待验证：** Cursor 是否将 `sessionStart` 返回的 `env` 传递到后续 hook 的进程环境中？如果不可靠，需要在每个 hook 脚本中从输入 JSON 自行提取 `conversation_id` 作为 `sessionId`。

#### afterAgentResponse（审计）

Agent 完成一条助手消息后触发。

**输入：**

```json
{
  "text": "<assistant final text>"
}
```

**用途：** 审计 Agent 行为、记录对话摘要。

#### subagentStart / subagentStop（子任务追踪）

**subagentStart 输入：**

```json
{
  "subagent_id": "abc-123",
  "subagent_type": "generalPurpose",
  "task": "Explore the authentication flow",
  "parent_conversation_id": "conv-456",
  "subagent_model": "claude-sonnet-4-20250514"
}
```

**subagentStop 输入：**

```json
{
  "subagent_type": "generalPurpose",
  "status": "completed",
  "task": "Explore the authentication flow",
  "summary": "<subagent output summary>",
  "duration_ms": 45000,
  "modified_files": ["src/auth.ts"]
}
```

**用途：** 追踪 subagent 修改的文件列表，补充上报。

### 脚本实现示例

> 实际代码位于 `client/cursor/hooks/`。部署时复制到项目根目录 `.cursor/hooks/`。

#### report.ts（afterFileEdit 上报脚本）

```typescript
// client/cursor/hooks/report.ts
type AfterFileEditInput = {
  conversation_id: string;
  generation_id: string;
  model: string;
  hook_event_name: string;
  workspace_roots: string[];
  file_path: string;
  edits: Array<{ old_string: string; new_string: string }>;
};

type SnapshotRequest = {
  source: string;
  sessionId: string;
  model: string;
  files: Array<{
    path: string;
    content: string;
    action: string;
  }>;
};

const CHANGEZ_URL = process.env.CHANGEZ_URL ?? "http://127.0.0.1:8760";
const CHANGEZ_TOKEN = process.env.CHANGEZ_TOKEN ?? "";
const CHANGEZ_SOURCE = process.env.CHANGEZ_SOURCE ?? "cursor";
const MAX_FILE_SIZE = 10 * 1024 * 1024;
const MAX_RETRIES = 1;
const RETRY_DELAY_MS = 500;

async function fetchWithRetry(url: string, options: RequestInit): Promise<Response | null> {
  for (let i = 0; i <= MAX_RETRIES; i++) {
    try { return await fetch(url, options); }
    catch { if (i < MAX_RETRIES) await new Promise(r => setTimeout(r, RETRY_DELAY_MS)); }
  }
  return null;
}

async function main() {
  const input = JSON.parse(await stdin.text()) as AfterFileEditInput;
  let content: string;
  try { content = await Bun.file(input.file_path).text(); }
  catch (e) { console.error(`[changez] read failed: ${e}`); process.exit(0); }
  if (Buffer.byteLength(content, "utf8") > MAX_FILE_SIZE) process.exit(0);

  const body: SnapshotRequest = {
    source: CHANGEZ_SOURCE,
    sessionId: input.conversation_id,
    model: input.model,
    files: [{ path: input.file_path, content, action: "update" }],
  };
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (CHANGEZ_TOKEN) headers["Authorization"] = `Bearer ${CHANGEZ_TOKEN}`;

  const resp = await fetchWithRetry(`${CHANGEZ_URL}/api/snapshot`, {
    method: "POST", headers, body: JSON.stringify(body), signal: AbortSignal.timeout(5000),
  });
  if (!resp?.ok) console.error(`[changez] snapshot failed: ${resp?.status}`);
}
main().catch(e => console.error(`[changez] report.ts error: ${e}`));
```

（重复节，已删除）

### Hooks 方案 vs VS Code 扩展方案

| 维度 | VS Code 扩展 | Cursor Hooks |
|------|-------------|--------------|
| **AI 编辑检测** | FileSystemWatcher 兜底（debounce，可能漏报） | `afterFileEdit` 精确触发（含 diff 信息） |
| **用户手动编辑** | `onDidSaveTextDocument` 覆盖 | ❌ 不覆盖（Hooks 只响应 Agent/Tab 操作） |
| **Session/Model 上下文** | ❌ Cursor 未暴露相关 API | ✅ `sessionStart` 提供 session_id、model、composer_mode |
| **编辑精度** | 读取完整文件内容 | 从文件系统读取完整内容（edits 仅用于 metadata） |
| **实现复杂度** | 高（TypeScript 扩展，~600 行） | 中（TypeScript + Bun，~200 行） |
| **脚本语言** | TypeScript（VS Code API） | TypeScript（Bun runtime） |
| **运行时依赖** | VS Code/Cursor（已有） | Bun（需额外安装） |
| **部署方式** | .vsix 安装包 | `.cursor/hooks.json` + .ts 脚本（可提交到版本控制） |
| **团队分发** | 每人安装扩展 | 随代码提交，clone 即用（需装 Bun） |
| **配置管理** | VS Code settings | hooks.json（可版本控制） |
| **失败处理** | 扩展内日志 | 退出码 0=放行，2=阻止，其他=放行 |
| **适用场景** | 全量追踪（手动 + AI） | AI Agent 操作追踪 |

### Hooks 方案的局限

1. **不覆盖用户手动编辑**：Hooks 只在 Agent/Tab 操作时触发。纯手动编辑（不通过 AI）不会被追踪。需要 VS Code 扩展的 `onDidSaveTextDocument` 来覆盖。
2. **无法捕获文件删除**：`afterFileEdit` 只在文件创建或修改时触发。文件删除（无论是 Agent 的 Delete 工具还是 bash rm）不会触发此钩子。没有 `afterFileDelete` 钩子。
3. **无法区分 create vs update**：`afterFileEdit` 对两种场景都触发，且输入中没有字段区分。只能统一传 `update`，依赖服务端双重判断兜底（文件不存在→自动修正为 create）。
4. **edits 字段 ≠ 完整文件内容**：`edits` 是 diff 片段数组（old_string/new_string 对），不包含完整文件内容。文件内容必须从文件系统读取（`fs.readFile`），与 OpenCode 插件的 `fs.readFileSync` 策略一致。
5. **Shell 命令文件变更难以捕获**：`afterShellExecution` 提供命令和输出文本，但不直接告知哪些文件被修改。需要解析命令（如 `echo >> file`、`sed -i`、`cat > file`）来推断，难度高且容易误判。
6. **session_id 传递依赖 env 机制**：`afterFileEdit` 输入中的 `conversation_id` 等同于 `sessionStart` 的 `session_id`（Cursor 文档确认），但字段名不同。脚本中需做映射（`conversation_id` → `sessionId`）。`sessionStart` 返回的 `env` 是否能可靠传递到后续 hook 进程环境，需实机验证。
7. **source 名称必须匹配服务端预注册列表**：服务端 `seedSources()` 硬编码 `{"opencode", "claude-code", "cursor", "human"}`，未注册的 source 会返回 400。必须使用 `"cursor"`。
8. **Bun 运行时依赖**：TypeScript 脚本需要 Bun 运行时。用户需提前安装 Bun（`curl -fsSL https://bun.sh/install | bash`）。这是相比 Shell 脚本的额外依赖。
9. **退出码语义**：非 before/permission 类钩子（如 afterFileEdit）的输出不会被 Cursor 强制执行，属于 fire-and-forget。
10. **loop_limit**：`stop`/`subagentStop` 的 followup_message 默认限制 5 次循环。

### 混合方案（VS Code 扩展 + Hooks）

结合两者优点，实现 100% 覆盖率：

```
┌─────────────────────────────────────────────┐
│  Cursor IDE                                 │
│                                             │
│  用户手动编辑                                 │
│    → onDidSaveTextDocument (VS Code 扩展)   │
│    → POST /api/snapshot                     │
│                                             │
│  AI Agent 编辑                               │
│    → afterFileEdit (Cursor Hooks)           │
│    → POST /api/snapshot                     │
│                                             │
│  AI Shell 命令                               │
│    → afterShellExecution (Cursor Hooks)     │
│    → （可选）扫描命令影响的文件 → snapshot   │
│                                             │
│  兜底：FileSystemWatcher (VS Code 扩展)     │
│    → 确保遗漏的变更仍能被捕获                │
│    → SHA256 去重，不会产生重复数据           │
└─────────────────────────────────────────────┘
```

混合方案的优势：
- **100% 覆盖**：手动编辑 + AI 编辑 + Shell 命令影响
- **最优精度**：AI 编辑走 Hooks（精确），手动编辑走扩展（可靠）
- **SHA256 去重**：服务端天然去重，多渠道不会产生重复数据
- **渐进式部署**：可以先部署 Hooks，后续再加扩展

### Hooks 实现计划

#### Phase 1: 核心 Hooks

1. 创建 `.cursor/hooks.json` 配置
2. 实现 `report.ts`（afterFileEdit → snapshot 上报，TypeScript + Bun）
3. 实现 `session-init.ts`（sessionStart → project 注册 + 环境变量注入）
4. 实现 `audit.ts`（审计日志）
5. 复用 `client/opencode/changez.server.ts` 中的类型定义和 HTTP 辅助逻辑

#### Phase 2: 增强（可选）

1. `subagentStop` 追踪子任务修改的文件
2. `afterShellExecution` 扫描命令影响的文件
3. 提取共享模块（类型定义、HTTP 客户端、日志工具）到 `.cursor/hooks/lib/`
4. 与 VS Code 扩展组合部署（混合方案）
