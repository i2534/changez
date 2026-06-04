# OpenCode 子 Agent 文件追踪方案

## 背景

Changez OpenCode 插件使用 `tool.execute.after` 钩子追踪文件编辑。但该钩子是 **per-session** 的，子 agent 运行在独立 session 中，不会触发主 session 的钩子，导致子 agent 的文件编辑无法被追踪。

## 问题分析

### 当前架构

```
OpenCode Agent                  changez.server.ts            Changez Server
     │                                 │                           │
     │  tool.execute.after (write/edit) │                           │
     │  tool.execute.after (multiedit)  │                           │
     │  tool.execute.after (apply_patch)│                           │
     │                                 ├─ 提取文件路径              │
     │                                 ├─ 读取文件内容              │
     │                                 ├─ POST /api/snapshot ───────┤
     │                                 │  fire-and-forget          │
```

**问题**：`tool.execute.after` 签名包含 `sessionID`，只在当前 session 触发：

```typescript
"tool.execute.after"?: (input: {
    tool: string;
    sessionID: string;   // ← 绑定到特定 session
    callID: string;
    args: any;
}) => Promise<void>;
```

子 agent 运行在独立 session（如 `ses_16da327bbffeov2wXPBXSwi3qP`），`parentID` 指向主 session，但 `tool.execute.after` 不会跨 session 传播。

### 验证结果

| 编辑来源 | 是否记录 | 原因 |
|---|---|---|
| 主 agent | ✅ 正常 | `tool.execute.after` 在主 session 触发 |
| 子 agent | ❌ 未记录 | 子 agent 独立 session，钩子不跨 session |

## 方案：双钩子追踪

### 核心思路

保留 `tool.execute.after` 追踪主 session，新增 `event` 钩子监听 `session.diff` 事件追踪子 session。

```
┌─────────────────────────────────────────────────────────┐
│  tool.execute.after (per-session)                       │
│  ✅ 主 session 编辑 → 完整元数据 (sessionID, model)      │
│  ❌ 子 agent 编辑 → 不触发                               │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│  event 钩子 (global)                                    │
│  ✅ session.diff → 所有 session（含子 agent）            │
│  ✅ 有 sessionID → 可关联 parentID                       │
│  ✅ 有 FileDiff[] → 包含 before/after 内容               │
└─────────────────────────────────────────────────────────┘
```

### 关键事件类型

#### `session.diff`（推荐）

```typescript
export type EventSessionDiff = {
    type: "session.diff";
    properties: {
        sessionID: string;
        diff: Array<FileDiff>;
    };
};

export type FileDiff = {
    file: string;
    before: string;
    after: string;
    additions: number;
    deletions: number;
};
```

> ✅ **已验证** — 类型定义来自 `@opencode-ai/sdk/dist/gen/types.gen.d.ts`（line 511, 32），数据结构与文档描述完全一致。

**优势**：
- ✅ 有 `sessionID` → 可关联 `parentID` 判断是否子 session
- ✅ 有 `FileDiff[]` → 包含 before/after 内容，无需额外读取文件
- ✅ 天然去重 → 每个 session 只触发一次（session 完成时）

#### `file.edited`（备选）

```typescript
export type EventFileEdited = {
    type: "file.edited";
    properties: {
        file: string;
    };
};
```

> ✅ **已验证** — 类型定义来自 `@opencode-ai/sdk/dist/gen/types.gen.d.ts`（line 425）。

**劣势**：
- ❌ 无 `sessionID` → 无法区分主/子 session
- ❌ 只有文件路径 → 需额外读取内容
- ❌ 需时间窗口去重 → 竞态风险

### 实现方案

#### 1. Session 上下文管理

```typescript
// 维护 session → 元数据映射
const sessionContext = new Map<string, {
    parentID?: string;
    model?: { providerID: string; modelID: string };
    source: string;
    startTime: number;
}>();

// 监听 session.created → 记录 parentID
// 监听 message.updated → 记录 model
// 监听 session.diff → 获取 sessionID + 文件差异
```

#### 2. Event 钩子实现

```typescript
"event": async (input: { event: Event }) => {
    const { event } = input;

    // 1. session.created → 记录父子关系
    if (event.type === "session.created") {
        const { info } = event.properties;
        sessionContext.set(info.id, {
            parentID: info.parentID,
            source: "opencode",
            startTime: Date.now(),
        });
    }

    // 2. session.diff → 追踪子 session 编辑
    if (event.type === "session.diff") {
        const { sessionID, diff } = event.properties;
        const ctx = sessionContext.get(sessionID);

        // 只处理子 session（有 parentID）
        if (ctx?.parentID) {
            for (const fileDiff of diff) {
                if (fileDiff.after) {
                    // 上报快照
                    await sendSnapshot({
                        source: "opencode",
                        sessionId: sessionID,
                        model: ctx.model?.modelID ?? "",
                        files: [{
                            path: fileDiff.file,
                            content: fileDiff.after,
                            action: fileDiff.before ? "update" : "create",
                        }],
                    });
                }
            }
        }
    }
}
```

#### 3. 去重机制

`session.diff` 天然去重：
- 每个 session 只在完成时触发一次
- 包含该 session 的所有文件差异
- 不会与 `tool.execute.after` 冲突（主 session 走 `tool.execute.after`，子 session 走 `session.diff`）

### 元数据

主/子 session 编辑的元数据对比：

| 字段 | 主 session | 子 session |
|---|---|---|
| source | ✅ `"opencode"` | ✅ `"opencode"` |
| sessionId | ✅ 完整 sessionID | ✅ 子 sessionID |
| model | ✅ 完整 model | ⚠️ 需从 `message.updated` 获取 |
| parentID | ❌ | ✅ 父 sessionID |

> **设计决策**：主/子 session 统一使用 `"opencode"` 作为 source，无需在服务端 `seedSources` 中新增条目。版本历史中不区分调度来源。

### 触发时机

| 事件 | 触发时机 | 频率 |
|---|---|---|
| `tool.execute.after` | 每次工具调用后 | 实时 |
| `session.diff` | session 完成时 | 延迟（session 结束时） |

**注意**：`session.diff` 是延迟触发，子 agent 编辑在 session 结束时才上报。如需实时追踪，需结合 `file.edited` + 时间窗口去重。

## 实现步骤

1. **新增 `event` 钩子** → 监听 `session.created` + `session.diff`
2. **维护 session 上下文表** → 记录 `parentID`、`model`
3. **子 session 编辑上报** → 通过 `session.diff` 获取文件差异
4. **元数据处理** → 子 session 使用 `"opencode"` source（与主 session 统一）

## 替代方案对比

| 方案 | 原理 | 优点 | 缺点 |
|---|---|---|---|
| **session.diff**（推荐） | 监听 session 完成事件 | 有 sessionID、文件差异、天然去重 | 延迟触发（session 结束时） |
| **file.edited** | 监听文件编辑事件 | 实时触发 | 无 sessionID、需时间窗口去重 |

> 注：`inotify 监控` 和 `主动扫描` 方案需要新增系统级依赖或平台支持，与 changez 现有的"无外部依赖"模式冲突，不推荐采用。

## 平台改进建议

向 OpenCode 平台提交改进：

1. **`file.edited` 增加 `sessionID`** → 可精准追踪来源
2. **全局 `tool.execute.after`** → 跨 session 触发
3. **`event` 钩子增加 `sessionID`** → 通用上下文传递

## SDK 验证状态

| 事件/类型 | SDK 路径 | 行号 | 状态 |
|---|---|---|---|
| `EventSessionDiff` | `@opencode-ai/sdk/dist/gen/types.gen.d.ts` | 511 | ✅ 已验证 |
| `FileDiff` | `@opencode-ai/sdk/dist/gen/types.gen.d.ts` | 32 | ✅ 已验证 |
| `EventSessionCreated` | `@opencode-ai/sdk/dist/gen/types.gen.d.ts` | 493 | ✅ 已验证 |
| `EventFileEdited` | `@opencode-ai/sdk/dist/gen/types.gen.d.ts` | 425 | ✅ 已验证 |
| `Session.parentID` | `@opencode-ai/sdk/dist/gen/types.gen.d.ts` | 465 | ✅ 已验证 |
| `event` 钩子 | `@opencode-ai/plugin/dist/index.d.ts` | 171 | ✅ 已验证 |

## 相关文件

- `client/opencode/changez.server.ts` — 插件主文件
- `~/.opencode/node_modules/@opencode-ai/plugin/dist/index.d.ts` — 插件 Hooks 接口
- `~/.opencode/node_modules/@opencode-ai/sdk/dist/gen/types.gen.d.ts` — SDK 事件类型
