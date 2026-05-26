# Changez Design — 文件版本追踪服务

## 概述

轻量级文件版本追踪服务，为不在 git 管理中或不方便 push 的本地变更提供独立备份和恢复能力。
核心思路：基于文本 diff 的增量存储，不依赖 git 仓库。只追踪文本文件，不考虑二进制。

## 数据目录

所有文件和服务二进制同目录，便于迁移。

```
changez/                  # 服务目录
  changez                 # 二进制
  config.yaml             # 配置
  data/
    changez.db            # SQLite 数据库
    blobs/                # 完整文件快照（zstd 压缩）
    deltas/               # delta 文件（按 file_id 组织）
  SERVER_DESIGN.md
  DISCUSSION.md
```

配置文件 `config.yaml`：

```yaml
listen: "127.0.0.1:8760"
token: ""

storage:
  max_file_size: 10485760   # 10MB

compact:
  enabled: true
  interval: "24h"
  max_delta_chain: 50
  delta_compress_threshold: 512

log:
  level: "info"
  file: "changez.log"
```

`data/` 下所有数据文件（changez.db、WAL、blobs、deltas）。路径相对当前目录。

## Go 依赖

- `github.com/mark3labs/mcp-go` — MCP JSON-RPC server
- `modernc.org/sqlite` — pure Go SQLite（免 CGO）
- `github.com/klauspost/compress/zstd` — zstd 压缩/解压
- `github.com/sergi/go-diff` — diff 生成（DiffMain → []Diff）、patch 应用（PatchApply）、[]Diff 序列化存储。unified diff 文本需自行遍历 []Diff 构造（库无现成渲染函数）

## 数据库表

### projects

项目注册，通过 API 动态注册。

```sql
CREATE TABLE projects (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT UNIQUE NOT NULL,
  root_path   TEXT UNIQUE NOT NULL,
  extra       TEXT DEFAULT '{}',           -- JSON，备用扩展
  is_deleted  BOOLEAN DEFAULT 0,           -- 软删除标记
  deleted_at  TIMESTAMP,
  created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

- name 可传入，不传则取 filepath.Base(root_path)
- 删除是软删除，数据保留保证恢复
- root_path UNIQUE 约束防止重复注册
- 注册时检查：root_path 规范化后注册（ filepath.Clean，去 trailing slash），重复则拒绝
- 允许父子路径重叠，path 匹配时选最长前缀（最深层 project 优先）

### files

文件注册，属于某个 project。路径存相对路径（相对于 root_path）。

```sql
CREATE TABLE files (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id        INTEGER NOT NULL,
  path              TEXT NOT NULL,          -- 相对路径
  latest_version_id INTEGER,               -- 快速定位最新版本
  created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(project_id, path),
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY (latest_version_id) REFERENCES versions(id)
);
```

- 不追踪重命名（无 inode/device），改名当删旧建新处理
- 无 is_deleted（方案 A：文件删除/重建只在 versions 记 action，file 记录永远保留）
- latest_version_id 每次新版本写入时更新

### versions

版本记录，链式 delta。

```sql
CREATE TABLE versions (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  file_id         INTEGER NOT NULL,
  storage_mode    TEXT NOT NULL,            -- 'blob' | 'delta'
  blob_hash       TEXT,                     -- blob 模式必填；delete action 保留旧值
  delta_offset    INTEGER,                  -- blob 模式为 NULL，delta 模式指向 deltas/{file_id}.delta 中的 entry offset
  base_id         INTEGER,                  -- 前一版本 id（链式）
  action          TEXT NOT NULL DEFAULT 'update',  -- 'create' | 'update' | 'delete'
  source_id       INTEGER NOT NULL DEFAULT 4,      -- 默认 human
  changed_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
  FOREIGN KEY (base_id) REFERENCES versions(id),
  FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE INDEX idx_versions_file_time ON versions(file_id, changed_at DESC);
CREATE INDEX idx_versions_source ON versions(source_id);
```

- 链式 delta：每个版本指向前一个版本（base_id）
- action 在 versions 记，不删 file 记录（create/update/delete 都在 versions 体现）
- sessionId、model、message 存在 delta entry 的 metadata 区域，blob/delete 版本无此信息
- source_id 外键关联 sources 表

### sources

来源标记，预置 4 条记录。

```sql
CREATE TABLE sources (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT UNIQUE NOT NULL,
  created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

服务启动时自动插入：opencode, claude-code, cursor, human。如果已存在则跳过。

## Blob 文件格式

每个 blob 文件 `blobs/{sha256}`，sha256 为原始文件内容（未压缩前）的 SHA256 hash。含固定 header。

Header 格式（固定 6B，big-endian）：

```
+--------+-----------------+
| magic  | compress_method |
| 4B     | 2B              |
```

- `magic`: `0x424C0001` ("BL" + v1)
- `compress_method`: `0x0000`=无压缩, `0x0001`=zstd, `0x0002+`=预留

Blob 文件布局：`[6B header][compressed/raw content]`

## Delta 文件格式

每个文件一个 delta 文件 `deltas/{file_id}.delta`，追加写入。

每条 entry 的 header 格式（固定 14B，big-endian）：

```
+--------+------------+---------------+--------------+
| magic  | version_id | compress_method | delta_length |
| 4B     | 4B         | 2B              | 4B           |
```

- `magic`: `0x43440001` ("CD" + v1)
- `version_id`: versions.id，用于校验
- `compress_method`: `0x0000`=无压缩, `0x0001`=zstd, `0x0002+`=预留
- `delta_length`: diff 内容长度（压缩后）

**metadata 区域（紧跟 diff content 之后，独立于 header）：**

delta 版本才写 metadata，blob/delete 版本不写。

metadata 区域结构：`[4B metaHeader][meta JSON]`

- `metaHeader`（4B，big-endian）：最高 bit (0x80000000) 为 flag 位，其余 31 bit 为 JSON 长度
  - flag=0（整个 uint32 为 0）→ 无 metadata，JSON 部分不存在
  - flag=1 → 有 metadata，后续紧跟 JSON 字节

Entry 整体布局：`[14B header][delta_length bytes diff][4B metaHeader][meta JSON]`

```json
{
  "sessionId": "sess_abc123",
  "model": "deepseek-v4-flash",
  "message": "refactor: extract parser function"
}
```

metadata 明文 JSON，不压缩。flag=0 时 metadata 区域不存在。

## Compact 机制

Delta 链过长时的压缩整理。

**双触发：**
1. 写入时：检查该文件的 delta 链长（连续 delta 模式版本数，不含 blob checkpoint），超过 max_delta_chain → 立即 compact
2. 定时器：每 24h 检查所有文件，处理写入时没赶上的链过长

**compact 操作（方案 B — 就地转换最新版本）：**
1. 锁定该文件（per-file mutex）
2. 回溯最新版本：从最近 blob checkpoint 开始（若最新版本本身是 blob 则直接用），沿 delta 链逐个 apply，重建完整内容
3. 计算 SHA256，zstd 压缩，构造 6B blob header + 压缩内容，写入 `blobs/{sha256}`
4. 就地更新该版本的 versions 记录：storage_mode=blob，blob_hash=sha256，delta_offset=NULL，base_id=NULL
5. 旧 delta entries 变为孤儿（不可达），暂不物理清理（后续讨论）
6. 后续新版本以该 blob 为基线计算 delta
7. 释放锁

- 版本号连续，log 输出干净
- 旧链保留可读（v1→v2→...→vn-1 的 delta 仍可 restore）
- 旧 delta 文件不主动清理，文本 diff 体积小，积累开销可接受

## 并发写入

**方案 A：per-file RWMutex**

同文件的写操作（snapshot）用排他锁，读操作（restore/log/diff）用共享锁。不同文件天然隔离。
Go `sync.Map` of `*sync.RWMutex`，粒度小，不影响不同文件的并发。
写操作 <10ms，同文件+同时刻概率极低；读操作不阻塞其他读操作。

## 服务启停

**启动：**
1. 解析命令行参数 `-c config.yaml`（可选，无参数走默认路径）
2. 加载 config.yaml
3. 初始化日志
4. 初始化 SQLite：CREATE TABLE IF NOT EXISTS (4 张表) + 索引
5. 预置 sources 数据（INSERT OR IGNORE）
6. 检查/创建存储目录 (blobs/, deltas/)
7. 启动 compact 定时器（不做即时 compact）
8. 启动 HTTP server（含 MCP SSE endpoint）

**关闭：**
1. 收到 SIGINT/SIGTERM
2. 停止 HTTP server（不再接受新请求）
3. 停止 compact 定时器
4. 等待进行中的请求完成（超时 10s 强杀）
5. 关闭 SQLite
6. 退出

## 工具定义

### changez_snapshot

捕获指定文件的当前状态，与上次快照对比生成增量。

```json
{
  "name": "changez_snapshot",
  "description": "Capture a snapshot of file changes. Upload file contents for version tracking.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "source":   { "type": "string", "enum": ["opencode", "claude-code", "cursor", "human"] },
      "sessionId": { "type": "string" },
      "model":     { "type": "string" },
      "files": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "path":    { "type": "string" },
            "content": { "type": "string" },
            "action":  { "type": "string", "enum": ["create", "update", "delete"] },
            "message": { "type": "string" }
          },
          "required": ["path"]
        },
        "minItems": 1
      }
    },
    "required": ["source", "files"]
  }
}
```

**执行流程：**
1. 校验 source 在 sources 表中
2. 遍历 files，对每个文件：
   - 校验 content 大小（create/update 时检查，超过 max_file_size → 返回 `INVALID_REQUEST` 报错跳过）
   - 根据 path 匹配 project（未匹配 → 报错跳过）
   - action 双重判断：客户端填了服务端校验修正（create+已存在→update，update+不存在→create）
   - 加 per-file 锁
   - 查最新版本，比 hash（首次无上一版本则跳过此步；相同 → unchanged，跳过）；blob 模式直接用 blob_hash 比对，delta 模式需先重建内容再算 hash（此时可直接跳过 delta 计算）；delete action 跳过 hash 比较（无 content），但若最新版已是 delete 则直接返回 unchanged
   - delete action：写入 storage_mode="delete" 版本记录（blob_hash=NULL, delta_offset=NULL, base_id = 上一版本 id），不计算 delta
   - create/update：
     · 首次 → zstd 压缩完整 content（含 6B blob header），写入 `blobs/{sha256}` 文件，storage_mode=blob
     · 有历史 → 在内存中重建上一版本完整内容，DiffMain 生成 []Diff
     · delta_compress_threshold 判断：[]Diff 序列化后的原始字节 ≤512B 则无压缩，否则 zstd 压缩；若压缩后体积 ≥ 原始体积则存未压缩版本
     · 序列化 []Diff + 压缩，构造 14B header + diff 数据 + 4B metaHeader + metadata JSON（sessionId, model, message），追加写入 deltas/{file_id}.delta
   - 写入 versions 表（含 source_id, storage_mode, blob_hash/delta_offset, base_id（create=NULL, update=上一版本 id）, action）
   - 更新 files.latest_version_id
   - 释放锁
3. 返回结果（按请求顺序一一对应）

**返回格式：**

```json
{
  "results": [
    { "path": "/proj/main.go", "status": "captured", "versionId": 42 },
    { "path": "/proj/util.go", "status": "captured", "versionId": 43 },
    { "path": "/unknown/x.go", "status": "error", "reason": "no project matches this path" }
  ],
  "summary": { "captured": 2, "unchanged": 0, "errors": 1 }
}
```

- results 数组顺序和请求的 files 数组完全一致
- status: captured（新建或更新）、unchanged（内容没变跳过）、error（失败）
- 部分失败继续处理，客户端单独处理失败文件

### changez_log

查看文件版本历史，支持时间和来源过滤。

```json
{
  "name": "changez_log",
  "description": "View the version history of a file, optionally filtered by time range or source.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "path":     { "type": "string" },
      "since":    { "type": "string", "format": "date-time" },
      "until":    { "type": "string", "format": "date-time" },
      "source":   { "type": "string", "enum": ["opencode", "claude-code", "cursor", "human"] },
      "action":   { "type": "string", "enum": ["create", "update", "delete"] },
      "limit":    { "type": "integer", "default": 20 },
      "offset":   { "type": "integer", "default": 0 }
    },
    "required": ["path"]
  }
}
```

**返回格式：**

```json
{
  "file": "/home/lan/workspace/go/notice/server/main.go",
  "project": "notice",
  "totalVersions": 47,
  "versions": [
    {
      "versionId": 12,
      "timestamp": "2026-05-16T14:32:00Z",
      "source": "opencode",
      "action": "update",
      "sessionId": "sess_abc123",
      "message": "refactor: extract parser function"
    }
  ]
}
```

- 时间格式：严格 ISO 8601
- action 字段：create（首次）、update（修改）、delete（文件删除）
- source 来自 versions.source_id → sources.name
- sessionId、model 和 message 来自 delta entry 的 metadata 区域；blob/delete 版本无此字段

### changez_restore

恢复到指定版本，返回完整文件内容（不写盘）。

```json
{
  "name": "changez_restore",
  "description": "Restore a file to a specific version. Returns the full file content.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "path":       { "type": "string" },
      "version":    { "type": "integer" }
    },
    "required": ["path", "version"]
  }
}
```

**执行流程：**
1. 根据 path 定位 project + file
2. 查 versions 表确认 version 存在且 file 匹配
3. 若该版本 action=delete → 返回错误（delete 版本无内容可恢复）
4. storage_mode=blob → 从 blobs/{hash} 解压即完整文件
5. storage_mode=delta → 回溯到前一个 blob checkpoint，逐个 apply delta 重建
6. 返回重建后的完整文件内容
7. 若 blob 或 delta entry 缺失/损坏 → 返回 `CORRUPTED_DATA` 错误

**错误码补充：**
| `CORRUPTED_DATA` | blob/delta 数据文件缺失或损坏 |

**返回格式：**

```json
{
  "path": "/home/lan/workspace/go/notice/server/main.go",
  "version": 12,
  "timestamp": "2026-05-16T14:32:00Z",
  "content": "<完整文件文本内容>"
}
```

### changez_diff

对比两个版本之间的差异。

```json
{
  "name": "changez_diff",
  "description": "Show the unified diff between two versions of a file.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "path":         { "type": "string" },
      "versionA":     { "type": "integer" },
      "versionB":     { "type": "integer" }
    },
    "required": ["path", "versionA", "versionB"]
  }
}
```

**执行流程：**
1. 根据 path 定位 project + file
2. 分别查两个 version，确认存在且 path 匹配
3. 若两版本相邻且 versionB 为 delta 模式 → 从 delta entry 读取 []Diff，渲染为 unified diff 返回
4. 否则分别重建两个版本的完整内容，DiffMain 生成 []Diff，渲染为 unified diff
5. 返回 diff 结果

**返回格式：**

```
--- a/server/main.go	2026-05-16 09:20:00
+++ b/server/main.go	2026-05-16 09:30:00
@@ -10,5 +10,8 @@
 func main() {
        fmt.Println("hello")
+       // new comment
+       x := 42
+       fmt.Println(x)
 }
```

## HTTP API

MCP 端点复用 HTTP server，额外暴露 RESTful 接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | /health | 健康检查 |
| POST | /mcp | MCP JSON-RPC endpoint |
| POST | /api/snapshot | 上报文件快照 |
| GET  | /api/files | 列出所有追踪文件 |
| GET  | /api/files/:path/versions | 版本列表 |
| GET  | /api/files/:path/restore/:version | 恢复指定版本 |
| GET  | /api/files/:path/diff?from=A&to=B | 版本对比 |
| POST | /api/projects | 注册项目 |
| DELETE | /api/projects/:id | 删除项目 |
| GET  | /api/projects | 列出所有项目 |
| GET  | /api/stats | 统计信息（projects/files/versions 数量 + sources 分类统计） |
| GET  | /api/snapshots/latest | 最新 snapshot 状态（TUI 轮询用） |

认证：所有接口共用 `Authorization: Bearer <token>`。token 为空时不认证。

## 错误处理与幂等性

### 错误响应格式

所有接口统一错误格式：

```json
{
  "error": {
    "code": "PROJECT_NOT_FOUND",
    "message": "no project matches path /unknown/x.go"
  }
}
```

错误码：

| 代码 | 含义 |
|------|------|
| `INVALID_REQUEST` | 参数校验失败 |
| `PROJECT_NOT_FOUND` | path 未匹配任何已注册项目 |
| `FILE_NOT_FOUND` | 文件无版本记录 |
| `VERSION_NOT_FOUND` | 指定 version 不存在 |
| `CORRUPTED_DATA`    | blob/delta 数据文件缺失或损坏 |
| `INTERNAL_ERROR`    | 服务端异常 |

### 幂等性

通过 SHA256 hash 去重实现：

1. snapshot 时计算 content 的 SHA256
2. 与最新版本 hash 对比，相同则返回 `unchanged`，不写入新记录
3. 客户端重试不会产生重复版本

注意：per-file mutex 保证同文件串行处理，天然避免并发冲突。
不需要额外的 idempotency key。

### 部分失败

snapshot 批量上报时，单个文件失败不影响其他文件：
- 失败文件在 results 中标记 `status: "error"` + `reason`
- summary 统计 captured/unchanged/errors 数量
- 客户端可针对失败文件单独重试

## 设计决策记录

| 决策 | 选择 | 原因 |
|------|------|------|
| 表/字段命名 | snake_case | 用户偏好 + SQLite 惯例 |
| 表数量 | 4 张（projects, files, versions, sources） | 项目隔离、文件追踪、版本记录、来源标记 |
| path 存储 | 相对路径 | 项目搬位置历史不丢 |
| 时间格式 | 严格 ISO 8601 | AI 友好，零歧义 |
| delta 存储 | full + patch 混合 | 首次存完整 blob，后续只存 diff，节省空间 |
| delta 链 | 链式（方案A） | 省空间，compact 截断控读性能 |
| 压缩 | zstd | 速度快、压缩率高，适合文本 diff |
| metadata 存储 | sessionId/model/message 在 delta entry metadata 区域，source 在 sources 表 | session 信息只跟 delta 绑定，blob/delete 版本不携带 |
| versions.size_bytes | 不要 | 现算开销大且非必需 |
| action 过滤 | 支持 | 可选过滤 create/update/delete |
| 分页 | limit + offset | 简单够用 |
| 重命名追踪 | 不做 | 客户端复杂度高，当删旧建新处理 |
| 文件删除 | 方案 A（versions 记 action，file 不删） | 历史连续，一条时间线 |
| 并发写入 | per-file RWMutex（方案A） | 写排他读共享，阻塞概率极低，读写不互相阻塞 |
| changez_restore | 只返回内容不写盘 | 服务职责单一，写盘由客户端决定 |
| changez_diff | 独立工具（方案A） | 职责单一，from/to version 必填 |
| snapshot.content | 客户端传入 | 服务不直接读取文件系统 |
| snapshot 返回 | 按请求顺序一一对应 | 客户端按索引匹配结果 |
| action 判断 | 双重判断（客户端填+服务端校验修正） | 客户端可能填错，服务端兜底 |
| compact | 双触发（写入时+定时器） | 写入时即时处理链过长，定时器兜底 |
| cherry-pick | Phase 1 不做 | 数据结构已预留，后续按需加 |
| 数据库迁移 | 暂不处理 | CREATE TABLE IF NOT EXISTS 够用，有改动再说 |
| 启动 compact | 不做 | 保证启动速度 |
| 优雅关闭 | 10s | 足够处理进行中的请求 |
| delta header 字段顺序 | magic, version_id, compress_method, delta_length（14B） | meta_length 不在 header 中，是 diff 之后的独立 4B 字段 |
| delta header 命名 | snake_case | 统一命名风格 |
| 错误响应格式 | 统一 `{error: {code, message}}` | 结构化错误便于客户端处理 |
| 幂等性 | SHA256 hash 去重 | 内容相同不写新记录，重试安全 |
| blob 文件 header | 6B (magic 4B + compress_method 2B) | 与 delta 压缩算法预留对齐 |
| delta 存储格式 | []Diff 序列化（非 unified diff 文本） | 存储/apply 直接用，仅返回时渲染 unified diff |
| diff 库 | go-diff (diffmatchpatch) | DiffMain 生成 []Diff + PatchApply 还原 + 自行遍历 []Diff 构造 unified diff |
| compact 策略 | 就地转换最新版本（方案 B） | 版本号连续，旧链可读 |
| delete action 记录 | storage_mode="delete"（独立模式） | 简洁，rebuildContent 直接返回错误 |
| project 路径重叠 | 允许 + 最长前缀匹配 | 灵活，注册时规范化路径防重复 |
| /api/stats | projects/files/versions 数量 + sources 分类统计 | 简单够用 |
| 健康检查 | GET /health | MCP client 连接探测 |
| Go 项目结构 | cmd/main.go + internal/ | 标准 Go 布局 |
| 认证 | Bearer token（可空） | MCP 和 HTTP 共用同一套鉴权 |
