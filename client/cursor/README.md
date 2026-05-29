# Changez × Cursor

通过 Cursor 原生 Hooks 系统自动追踪文件变更，上报到 Changez 服务器。

## 快速安装

```bash
# 预览安装（不执行写入）
bash setup.sh --dry-run

# 自动安装（合并现有 hooks.json，复制文件）
bash setup.sh

# 通过参数指定服务器地址和 Token
bash setup.sh --url http://127.0.0.1:8760 --token your-token

# 通过环境变量指定（命令行参数优先级更高）
CHANGEZ_URL="http://127.0.0.1:8760" CHANGEZ_TOKEN="your-token" bash setup.sh

# 卸载
bash setup.sh --uninstall
```

## 手动安装

### 1. 复制 Hook 文件到 Cursor 配置目录

```bash
# Cursor hooks 目录（全局）
mkdir -p ~/.cursor/hooks/changez
cp hooks/report.js ~/.cursor/hooks/changez/
cp hooks/session-init.js ~/.cursor/hooks/changez/
```

文件结构：

```
~/.cursor/
├── hooks.json              ← Hook 入口配置
└── hooks/
    └── changez/
        ├── session-init.js     ← 会话启动：注册项目 + 注入环境变量
        └── report.js           ← 文件编辑后：上报快照
```

### 2. 配置 hooks.json

在 `~/.cursor/hooks.json` 中添加：

```jsonc
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "command": "CHANGEZ_URL=http://127.0.0.1:8760 CHANGEZ_TOKEN=your-token CHANGEZ_SOURCE=cursor node hooks/changez/session-init.js",
        "timeout": 10
      }
    ],
    "afterFileEdit": [
      {
        "command": "CHANGEZ_URL=http://127.0.0.1:8760 CHANGEZ_TOKEN=your-token CHANGEZ_SOURCE=cursor node hooks/changez/report.js",
        "timeout": 10
      }
    ]
  }
}
```

### 3. 重启 Cursor

重启 Cursor 使 hooks 生效。

## 验证安装

### 手动测试 hook 脚本

```bash
# 测试 session-init.js
echo '{"workspace_roots":["/path/to/project"],"session_id":"test","model":"claude-sonnet-4-20250514"}' | node ~/.cursor/hooks/changez/session-init.js

# 测试 report.js
echo '{"file_path":"/path/to/file.txt","conversation_id":"test","model":"claude-sonnet-4-20250514"}' | node ~/.cursor/hooks/changez/report.js
```

### 在 Cursor 中验证

1. **使用 Composer 模式**（`Cmd+I` 或 `Ctrl+I`）
2. **让 AI 修改文件** — 这会触发 `sessionStart` 和 `afterFileEdit` hooks
3. **查看执行日志**：
   - 打开 Cursor 设置 → Hooks
   - 查看 Execution Log 面板
   - 点击条目展开查看 Input/Output/Error 详情

**注意**：hooks 只在 Composer/Agent 模式下触发。普通文件编辑（手动保存）不会触发 hook。

### 查看 Changez 服务

```bash
# 查看文件列表
curl "http://127.0.0.1:8760/api/files?project=你的项目名" \
  -H "Authorization: Bearer your-token"
```

## 工作原理

```
Cursor                         hooks/changez/                Changez Server
  │                                  │                              │
  │  sessionStart (JSON via stdin)   │                              │
  ├──────────────────────────────────┼── POST /api/projects ─────────┤
  │                                  │  注册项目 + 返回 env 注入      │
  │                                  │                              │
  │  afterFileEdit (JSON via stdin)  │                              │
  ├──────────────────────────────────┼── POST /api/snapshot ─────────┤
  │                                  │  读取文件内容 → 上报          │
```

- **sessionStart** → 注册项目 + 向 Cursor 注入 `CHANGEZ_*` 环境变量
- **afterFileEdit** → 从文件系统读取文件内容，上报快照
- **fire-and-forget** → 不阻塞 Cursor 正常操作流程
- **结构化输出** → hook 脚本输出 JSON 到 stdout，Cursor 记录到执行日志

## 故障排查

| 问题 | 解决方案 |
|---|---|
| 执行日志为空 | 确保使用 Composer 模式（`Cmd+I`），普通编辑不触发 hook |
| hook 超时 | 检查 Changez 服务是否运行，增加 `timeout` 值 |
| 脚本报错 | 查看执行日志的 Error Output 详情 |
| 手动测试正常但 Cursor 不触发 | 重启 Cursor，确认 hooks.json 格式正确 |
