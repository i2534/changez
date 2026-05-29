# Changez × Claude Code

通过 hook 机制自动追踪 Claude Code 的文件变更，上报到 Changez 服务器。

## 工作模式

| 模式 | 说明 | 适用场景 |
|---|---|---|
| **stdin hook**（推荐） | Claude Code 通过管道传入 JSON，脚本 fire-and-forget 上报 | 大多数场景 |
| **daemon HTTP** | 独立 HTTP 服务接收 hook 请求，异步处理 | 需要持久化连接、统计监控 |

## 快速安装

```bash
# 预览安装（不执行写入）
bash setup.sh --dry-run

# 自动安装（合并现有配置，复制文件）
bash setup.sh

# 通过参数指定服务器地址和 Token
bash setup.sh --url http://127.0.0.1:8760 --token your-token

# 通过环境变量指定（命令行参数优先级更高）
CHANGEZ_URL="http://127.0.0.1:8760" CHANGEZ_TOKEN="your-token" bash setup.sh

# Daemon 模式 / 项目级安装 / 自定义路径
bash setup.sh --daemon
bash setup.sh --project
bash setup.sh --claude-dir /path/to/claude-config

# 卸载
bash setup.sh --uninstall
bash setup.sh --uninstall --claude-dir /path/to/claude-config
```

## 手动安装

### 1. 复制 Hook 脚本

```bash
# 全局安装（所有项目共享）
mkdir -p ~/.claude/changez
cp changez-hook.js ~/.claude/changez/

# 或项目级安装
cp changez-hook.js .claude/changez-hook.js
```

### 2. 配置环境变量

在 shell 配置文件（`~/.bashrc` / `~/.zshrc`）或 Claude Code 启动环境中设置：

```bash
export CHANGEZ_URL="http://127.0.0.1:8760"    # Changez 服务器地址
export CHANGEZ_TOKEN="your-secret-token"       # Bearer Token（可选）
export CHANGEZ_SOURCE="claudecode"             # 来源标识（默认 claudecode）
export CHANGEZ_LOG_FILE=""                     # 日志文件路径（可选）
```

可选环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CHANGEZ_URL` | `http://127.0.0.1:8760` | Changez 服务器地址 |
| `CHANGEZ_TOKEN` | `""` | Bearer Token |
| `CHANGEZ_SOURCE` | `claudecode` | 来源标识 |
| `CHANGEZ_LOG_FILE` | `<claude-dir>/changez/changez.log` | 日志文件路径 |
| `CHANGEZ_MAX_FILE_SIZE` | `10485760` | 单文件最大字节数（10MB） |
| `CHANGEZ_MAX_RETRIES` | `2` | 请求最大重试次数 |
| `CHANGEZ_RETRY_DELAY` | `500` | 重试基础延迟（毫秒） |

### 3. 配置 Claude Code Hook

在项目或全局的 `.claude/settings.json` 中添加：

```jsonc
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "CHANGEZ_URL=http://127.0.0.1:8760 CHANGEZ_TOKEN=your-secret-token node ~/.claude/changez/changez-hook.js",
            "async": true
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "CHANGEZ_URL=http://127.0.0.1:8760 CHANGEZ_TOKEN=your-secret-token node ~/.claude/changez/changez-hook.js",
            "async": true
          }
        ]
      }
    ]
  }
}
```

### 4. Daemon 模式（可选）

如果需要使用 HTTP 服务模式：

```bash
# 启动 daemon（默认监听 127.0.0.1:8761）
node ~/.claude/changez/changez-daemon.js

# 带 token 启动
CHANGEZ_TOKEN="your-token" node ~/.claude/changez/changez-daemon.js

# 自定义端口
CHANGEZ_PORT=9000 node ~/.claude/changez/changez-daemon.js
```

Daemon 模式下的 hook 配置改为指向 daemon 地址：

```jsonc
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "curl -sf -X POST -d @- -H 'Content-Type: application/json' http://127.0.0.1:8761/hook",
            "async": true
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "curl -sf -X POST -d @- -H 'Content-Type: application/json' http://127.0.0.1:8761/hook",
            "async": true
          }
        ]
      }
    ]
  }
}
```

## 验证安装

```bash
# 运行自动化测试（需要 Changez 服务正在运行）
bash test-hook.sh

# 手动测试 SessionStart
echo '{"hook_event_name":"SessionStart","cwd":"/path/to/project"}' | node changez-hook.js

# 手动测试 Write
echo '{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"test.txt","content":"hello"},"tool_response":{"type":"create"},"session_id":"test-001"}' | node changez-hook.js
```

查看 daemon 状态：

```bash
curl http://127.0.0.1:8761/health
curl http://127.0.0.1:8761/stats
```

## 工作原理

```
Claude Code                    changez-hook.js              Changez Server
    │                                │                            │
    │  SessionStart (JSON via stdin) │                            │
    ├────────────────────────────────┼──────── POST /api/projects ──┤
    │                                │                            │
    │  Write/Edit (JSON via stdin)   │                            │
    ├────────────────────────────────┼──────── POST /api/snapshot ──┤
    │                                │                            │
    │  (始终 exit 0)                 │  fire-and-forget           │
```

- **SessionStart** → 自动注册项目（`POST /api/projects`）
- **PostToolUse (Write/Edit)** → 上报文件快照（`POST /api/snapshot`）
- **其他工具** → 静默跳过
- **始终 exit 0** → 不阻塞 Claude Code 正常流程
