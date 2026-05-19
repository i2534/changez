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

**不传 sessionId 和 model**。原因：
- Cursor 没有暴露 session/model 相关的扩展 API
- changez 服务的 snapshot 接口中这些字段是可选的
- source 标记为 `"cursor"` 已足够区分来源

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

---

## 与 OpenCode 插件对比

| 维度 | OpenCode 插件 | Cursor 扩展 |
|------|--------------|-------------|
| 触发方式 | `tool.execute.after` hook | FileSystemWatcher + onDidSave |
| 确定性 | 100%（hook 必触发） | 100%（文件系统事件必触发） |
| session/model | 有（chat.message hook） | 无（Cursor 未暴露） |
| action 精度 | 高（从工具类型推断） | 中（从文件系统事件推断） |
| Token 开销 | 零 | 零 |
| 用户配置 | opencode.json | VS Code settings |
| 代码量 | 单文件 ~400 行 | 多文件 ~600 行（含 package.json） |

---

## 待验证

### VS Code 扩展 API 细节

1. `onDidSaveTextDocument` 是否能捕获 Cursor AI 的文件写入？Cursor AI 可能绕过编辑器直接写文件，此时只有 FileSystemWatcher 能捕获。
2. `onStartupFinished` activation event 在 Cursor 中是否正常工作？Cursor 是 VS Code fork，大部分 API 兼容，但需实测。
3. 多 workspace folder 时，每个 folder 是否独立触发 activate？还是只触发一次？

### FileSystemWatcher 性能

1. 大项目（数万文件）下 watcher 的内存占用和 CPU 开销是否在可接受范围内？VS Code 自身也用同样的 API，应该没问题。
2. debounce 500ms 是否足够？如果 AI 在 500ms 内写完一个文件又开始写另一个，两个文件是独立计时互不影响的。

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
