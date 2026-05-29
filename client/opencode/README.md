# Changez × OpenCode

通过 OpenCode 插件系统自动追踪文件变更，上报到 Changez 服务器。

包含两个插件：
- **Server 插件**（必需）— 拦截文件修改工具，自动上报快照
- **TUI 插件**（可选）— 侧边栏实时显示 Changez 状态

## 快速安装

```bash
# 预览安装（不执行写入）
bash setup.sh --dry-run

# 自动安装（插件安装到 ~/.opencode/changez/）
bash setup.sh

# 通过参数指定服务器地址和 Token
bash setup.sh --url http://127.0.0.1:8760 --token your-token

# 通过环境变量指定（命令行参数优先级更高）
CHANGEZ_URL="http://127.0.0.1:8760" CHANGEZ_TOKEN="your-token" bash setup.sh

# 仅安装 Server 插件 / 卸载
bash setup.sh --server-only
bash setup.sh --uninstall
```

## 手动安装

### 1. Server 插件（必需）

```bash
mkdir -p ~/.opencode/changez
cp changez.server.ts ~/.opencode/changez/
```

### 2. TUI 插件（可选）

```bash
cp changez.tui.tsx ~/.opencode/changez/
```

### 3. 配置插件参数

在 `opencode.json` 或 `opencode.jsonc` 中配置 Server 插件参数：

```jsonc
{
  "plugin": [
    ["file://~/.opencode/changez/changez.server.ts", {
      "url": "http://127.0.0.1:8760",
      "token": "your-secret-token",
      "source": "opencode",
      "logLevel": "info"
    }]
  ]
}
```

TUI 插件配置（`tui.json`）：

```jsonc
{
  "plugin": [
    ["file://~/.opencode/changez/changez.tui.tsx", {
      "url": "http://127.0.0.1:8760",
      "token": "your-secret-token"
    }]
  ]
}
```

### 配置参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `url` | `http://127.0.0.1:8760` | Changez 服务器地址 |
| `token` | `""` | Bearer Token（可选） |
| `source` | `opencode` | 来源标识 |
| `project.name` | 目录名 | 项目名称覆盖 |
| `logLevel` | `info` | 日志级别（debug / info / warn / error） |

## 验证安装

```bash
# 重启 OpenCode，查看日志输出
# Server 插件加载时会输出: "changez loaded (http://127.0.0.1:8760, project: xxx)"

# 执行一次文件编辑操作，检查 Changez 是否收到快照
curl "http://127.0.0.1:8760/api/files?project=你的项目名" \
  -H "Authorization: Bearer your-token"
```

## 工作原理

### Server 插件

```
OpenCode Agent                  changez.server.ts            Changez Server
     │                                 │                           │
     │  启动                            │                           │
     ├──────────────────────────────────┼── POST /api/projects ─────┤
     │                                 │  注册项目（失败则 30s 重试）│
     │                                 │                           │
     │  tool.execute.after (write/edit) │                           │
     │  tool.execute.after (multiedit)  │                           │
     │  tool.execute.after (apply_patch)│                           │
     │  tool.execute.after (bash rm)    │                           │
     │                                 ├─ 提取文件路径              │
     │                                 ├─ 读取文件内容              │
     │                                 ├─ POST /api/snapshot ───────┤
     │                                 │  fire-and-forget          │
```

拦截的工具：

| 工具 | 行为 |
|---|---|
| `write` | 文件写入后上报 |
| `edit` | 文件编辑后上报 |
| `multiedit` | 批量编辑后上报所有文件 |
| `apply_patch` | 解析 patch 中的文件列表并上报 |
| `bash` (rm) | 解析 rm 命令中的文件路径，上报删除 |

### TUI 插件

在 OpenCode 侧边栏显示：
- 连接状态（Connected / Waiting / Service offline）
- 最近快照时间
- 已追踪文件列表及其版本号

## 文件忽略

Server 插件会自动跳过：
- 大于 10MB 的文件
- 不存在的文件（如 rm 后的文件）
- 通配符路径（如 `*.log`）
