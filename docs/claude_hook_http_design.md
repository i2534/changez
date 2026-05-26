# Claude Code Hook HTTP 模式设计

> 创建日期：2026-05-20
> 状态：❌ 已搁置（DSV4 review 通过，决定保留 command hook 方案）

---

## 搁置原因（2026-05-20 DSV4 Review）

DSV4 代码审查后决定搁置此方案，原因如下：

1. **性能收益不显著**：Node 进程启动 ~100-200ms，占 Claude Code hook timeout（10s）的 1-2%，不值得引入 daemon 复杂度
2. **并发竞态**：多个 hook 进程同时启动 daemon 导致端口冲突
3. **进程泄漏**：detached daemon 无清理机制，配置变更不生效
4. **配置不一致**：端口号在 hook 和 daemon 之间硬编码，容易出错
5. **代码膨胀**：从 ~150 行膨胀到 ~530 行

**结论**：保留 `changez-daemon.js` 文件备用，当前使用纯 command hook 方案。

---

## 方案：HTTP Hook + 本地持久化服务

### 架构

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
│              │ Type: http       │                    │
│              │ URL:             │                    │
│              │ http://127.0.0.1:8761/hook  │                    │
│              └────────┬─────────┘                    │
│                       │ HTTP POST                    │
│                       ▼                              │
│              ┌──────────────────┐                    │
│              │ changez-daemon   │                    │
│              │ (Node.js 常驻)    │                    │
│              │ :8761            │                    │
│              │                  │                    │
│              │ 解析 hook JSON   │                    │
│              │ 重试逻辑         │                    │
│              │ POST → changez   │                    │
│              └──────────────────┘                    │
└─────────────────────────────────────────────────────┘
                      │ HTTP POST
                      ▼
                ┌──────────────────┐
                │  changez         │
                │  service         │
                │  :8760/api/      │
                └──────────────────┘
```

**核心优势：**
- daemon 持久化运行，无进程创建开销
- 内存中维护连接池和重试状态
- 支持批量处理和更复杂的重试策略

---

## Claude Code HTTP Hook 规范

### 配置格式

```jsonc
// ~/.wps_claude/settings.json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:8761/hook",
            "timeout": 5000,
            "headers": {
              "X-Changez-Token": "${CHANGEZ_TOKEN}"
            }
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:8761/hook",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

### HTTP 请求格式

Claude Code 通过 `axios.post()` 发送请求：

```http
POST http://127.0.0.1:8761/hook
Content-Type: application/json

{
  "session_id": "06e8062a-...",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write",
  "tool_input": { ... },
  "tool_response": { ... }
}
```

**与 command hook 的差异：**
- command: JSON 通过 stdin 传入
- http: JSON 通过 POST body 传入
- http: 支持自定义 headers（环境变量插值 `${VAR}`）
- http: 有 SSRF 保护（允许 loopback）

---

## changez-daemon 设计

### 端口

默认 `8761`，通过环境变量 `CHANGEZ_DAEMON_PORT` 配置。

### 接口

```
POST /hook          — 接收 hook 事件
GET  /health        — 健康检查
GET  /stats         — 统计信息
```

### 请求处理

```javascript
// POST /hook
app.post("/hook", async (req, res) => {
  const hook = req.body

  // 立即返回 202 Accepted（fire-and-forget）
  res.status(202).json({ accepted: true })

  // 异步处理
  processHook(hook).catch(logError)
})

async function processHook(hook) {
  if (hook.hook_event_name === "SessionStart") {
    await handleSessionStart(hook)
  } else {
    await handlePostToolUse(hook)
  }
}
```

### 关键设计点

1. **立即响应 202**：不阻塞 Claude Code 的 hook timeout
2. **异步处理**：后台队列处理，支持并发
3. **重试逻辑**：复用现有 `post()` 函数的重试机制
4. **日志持久化**：写入配置文件指定的日志文件

---

## 配置方式

### 方式 A：HTTP Hook（推荐）

```jsonc
// ~/.wps_claude/settings.json
{
  "env": {
    "CHANGEZ_TOKEN": "HelloChangez",
    "CHANGEZ_LOG_FILE": "/tmp/changez-daemon.log"
  },
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:8761/hook",
            "timeout": 5000,
            "headers": {
              "X-Changez-Token": "${CHANGEZ_TOKEN}"
            }
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "http",
            "url": "http://127.0.0.1:8761/hook",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

### 方式 B：Command Hook（兼容）

```jsonc
// ~/.wps_claude/settings.json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "CHANGEZ_TOKEN=HelloChangez node changez-hook.js",
            "async": true
          }
        ]
      }
    ]
  }
}
```

### 方式 C：双模式（过渡期）

```jsonc
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          // 优先 HTTP（daemon 运行时）
          {
            "type": "http",
            "url": "http://127.0.0.1:8761/hook",
            "timeout": 2000
          },
          // 降级 command（daemon 不可用时）
          {
            "type": "command",
            "command": "CHANGEZ_TOKEN=HelloChangez node changez-hook.js",
            "async": true
          }
        ]
      }
    ]
  }
}
```

---

## daemon 启动方式

### 方式 1：手动启动

```bash
# 前台运行
node client/claude-code/changez-daemon.js

# 后台运行
nohup node client/claude-code/changez-daemon.js &
```

### 方式 2：systemd 服务

```ini
# ~/.config/systemd/user/changez-daemon.service
[Unit]
Description=Changez Hook Daemon
After=network.target

[Service]
ExecStart=/usr/bin/node /path/to/changez-daemon.js
Restart=always
RestartSec=3
Environment=CHANGEZ_TOKEN=HelloChangez
Environment=CHANGEZ_LOG_FILE=/tmp/changez-daemon.log

[Install]
WantedBy=default.target
```

```bash
systemctl --user enable --now changez-daemon
```

### 方式 3：changez 服务集成

```bash
# changez 服务启动时自动拉起 daemon
changez --start-daemon
```

---

## 实现范围

### Phase 1：daemon 基础
- [ ] `changez-daemon.js` — HTTP 服务器
- [ ] `/hook` 端点 — 接收并处理 hook 事件
- [ ] `/health` 端点 — 健康检查
- [ ] 复用现有 `post()` 重试逻辑
- [ ] 日志配置

### Phase 2：生产就绪
- [ ] systemd 服务模板
- [ ] 进程守护（pm2 / forever）
- [ ] 启动脚本 `changez daemon start`
- [ ] 配置模板生成

---

## 与现有方案对比

| 维度 | command hook | http hook + daemon |
|------|-------------|-------------------|
| 进程开销 | 每次 spawn node | 零（持久化进程） |
| 启动延迟 | ~100-200ms | ~1ms（已运行） |
| 内存占用 | 临时（GC 回收） | 常驻（~10MB） |
| 重试能力 | 进程内（已实现） | 进程内 + 队列 |
| 配置复杂度 | 低 | 中（需管理 daemon） |
| 可靠性 | 高（无单点） | 中（daemon 崩溃影响） |
| 适用场景 | 低频使用 | 高频使用 |

---

## 迁移策略

1. **保留 command hook** 作为兼容方案
2. 用户提供两种配置模板
3. 过渡期支持双模式配置
4. daemon 不可用时自动降级到 command

---

## 设计决策

| # | 问题 | 结论 |
|---|------|------|
| 1 | daemon 端口 | 8761（changez 是 8760） |
| 2 | 响应策略 | 立即 202 Accepted，异步处理 |
| 3 | 重试逻辑 | 复用现有 post() 函数 |
| 4 | 配置传递 | headers 环境变量插值 |
| 5 | command hook | 保留作为降级方案 |
| 6 | 启动方式 | 提供 systemd 模板 + 手动启动 |
