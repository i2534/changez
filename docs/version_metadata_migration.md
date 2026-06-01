# 版本元数据持久化方案

> 创建日期：2026-05-29
> 状态：📋 待实施

---

## 问题

`session_id` / `model` / `message` 三个元数据字段仅存储在 delta 文件的二进制 payload 中，`versions` 表没有对应列。Compactor 将 delta 版本转换为 blob 检查点后，这些元数据无法再读取。

**当前数据**（2026-05-29 快照，实际以运行时为准）：
| 存储模式 | 版本数 | 元数据可读 |
|---|---|---|
| delta | 502 | ✅ |
| blob | 148 | ❌ |
| delete | 8 | N/A |

---

## 方案：元数据写入 `versions` 表

元数据是版本属性，不应依赖存储模式。无论 blob 还是 delta，元数据都应持久化在数据库中。

### 改动清单

#### 1. Schema 变更

`versions` 表新增三列：

```sql
ALTER TABLE versions ADD COLUMN session_id TEXT;
ALTER TABLE versions ADD COLUMN model TEXT;
ALTER TABLE versions ADD COLUMN message TEXT;
```

#### 2. `internal/db/db.go`

**2.1 `CreateVersion`（DB + Tx 两个类型共 2 处）**

增加 `sessionID`, `model`, `message` 三个参数，INSERT 时写入：

```go
// 修改前
INSERT INTO versions (file_id, storage_mode, blob_hash, delta_offset, base_id, action, source_id)
VALUES (?, ?, ?, ?, ?, ?, ?)

// 修改后
INSERT INTO versions (file_id, storage_mode, blob_hash, delta_offset, base_id, action, source_id,
                       session_id, model, message)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
```

**影响范围**：
- 生产代码：`mcp.go` 中 3 处调用（delete/blob/delta 模式）传入元数据参数
- 测试代码：以下文件共 ~22 处调用需补传空字符串。建议创建 `createTestVersion(...)` helper 集中处理，减少改动点
  - `internal/db/db_test.go`（~12 处）
  - `internal/compact/compactor_test.go`（~4 处）
  - `internal/cleanup/cleanup_test.go`（~4 处）
  - `internal/handler/handler_test.go`（~1 处）
- 事务版本 `Tx.CreateVersion` 同步修改，签名与 `DB.CreateVersion` 保持一致

**空值处理**：Go 函数签名使用 `string` 类型（非 `*string`）。空字符串 `""` 直接存入 SQLite（非 NULL）。查询时 `v["sessionId"]` 返回 `""` 而非 `nil`，`log.go` 中已用 `sid != ""` 做判断，行为一致。

**2.2 `GetVersion`**

SELECT 增加 `session_id`, `model`, `message`；Scan 增加对应变量；返回 map 中携带。

**2.3 `ListVersions`**

SELECT 增加 `session_id`, `model`, `message`；Scan 增加对应变量（允许 NULL）；返回 map 中携带。

```sql
-- 修改前
SELECT v.id, v.action, v.storage_mode, v.changed_at, s.name as source_name
FROM versions v
JOIN sources s ON v.source_id = s.id

-- 修改后
SELECT v.id, v.action, v.storage_mode, v.changed_at, s.name as source_name,
       v.session_id, v.model, v.message
FROM versions v
JOIN sources s ON v.source_id = s.id
```

Scan 新增 `*string` 类型变量接收（ALTER TABLE 后历史行这些列为 NULL）。返回 map key 为 `sessionId`, `model`, `message`。

**2.4 `UpdateVersionMeta`（新增）**

```sql
UPDATE versions SET session_id = ?, model = ?, message = ? WHERE id = ?
```

供迁移脚本使用。

#### 3. `internal/handler/mcp.go`

**3.1 `snapshotSingleFile` — 所有 CreateVersion 调用点传入元数据**

| 调用位置 | 模式 | 元数据 |
|---|---|---|
| ~L94 | delete | `sessionID, model, message`（当前请求的，可能为空） |
| ~L141 | blob（首次创建） | `sessionID, model, message` |
| ~L187 | delta | `sessionID, model, message` |

**3.2 `DeltaStore.Append` 调用（~L199）不再传入 `meta`**

```go
// 修改前
offset, _, err := h.DeltaStore.Append(fileID, versionID, diffs, meta, threshold)

// 修改后
offset, _, err := h.DeltaStore.Append(fileID, versionID, diffs, threshold)
```

`meta` 变量及其构造代码（~L171-178）一并删除。

**测试代码影响**：
- `internal/storage/storage_test.go` 中 ~15 处 `Append` 调用需移除 `meta` 参数：
  - `TestDeltaStore_DeltaMeta`（~L259-285）：验证 meta 写入/读取 delta 文件。迁移后移除（meta 不再写入 delta）
  - `TestDeltaStore_NilMeta`（~L419-432）：验证 nil meta 写入后读取为 nil。移除 meta 参数后变为普通无 meta 测试，保留
  - `TestDeltaStore_PartialMeta`（~L434-453）：验证部分 meta 字段写入/读取。迁移后移除
  - 其余调用传入 `nil` 的改为直接移除参数
- `internal/compact/compactor_test.go` 中 1 处 `Append` 调用（~L101）需移除 `nil` 参数

#### 4. `internal/handler/log.go`

**4.1 `doLog`（~L130-151）简化**

修改前：对每个 delta 版本调用 `readDeltaMeta` 从 delta 文件读取元数据
修改后：从 `ListVersions` 返回的 map 直接读取（需要安全处理 NULL）：

```go
// 修改后
entry := versionEntry{
    VersionID: v["versionId"].(int64),
    Timestamp: v["timestamp"].(string),
    Source:    v["source"].(string),
    Action:    v["action"].(string),
}
if sid := v["sessionId"]; sid != nil && sid != "" {
    entry.SessionID = sid.(string)
}
if m := v["model"]; m != nil && m != "" {
    entry.Model = m.(string)
}
if msg := v["message"]; msg != nil && msg != "" {
    entry.Message = msg.(string)
}
```

**4.2 `readDeltaMeta`（~L159-179）删除**

该函数通过 `GetVersion` → `DeltaStore.ReadEntry` 从 delta 文件读取元数据，不再需要。

#### 5. `internal/handler/snapshot.go`

无需改动。该文件包含 `HandleSnapshot`（HTTP handler）和 `rebuildContent`，不涉及元数据读写。

#### 6. `internal/compact/compactor.go`

**无需改动**。`UpdateVersionStorage` 只更新 `storage_mode`/`blob_hash`/`delta_offset`，新增的元数据列不受影响。

```
compact 前后对照：
  v4(id=100, delta, session=ses_abc, model=claude-sonnet-4)
  →
  v4(id=100, blob, session=ses_abc, model=claude-sonnet-4)  ← 同一行，元数据保留
```

`rebuildFromDeltaChain` 调用 `deltaStore.ReadEntry` 时不再使用返回的 meta，忽略即可。

#### 7. `internal/storage/delta.go` — Delta 格式变更

**Delta 格式版本保持 v1 不变**（Magic `0x43440001`）。Meta 区域本来就是 optional 的，新写入的 delta entry 不再携带 meta payload 即可。

当前 delta entry 结构：
```
Header (14B):  Magic(v1) | versionID | compressMethod | deltaLength
Diff Data:     <variable length>
Meta Header:   flag(1bit) | metaLength(31bit)  ← 4B
Meta JSON:     <optional, only if flag=1>
```

| 改动 | 说明 |
|---|---|
| `Append` | 移除 `meta *DeltaMeta` 参数；始终写入 4B 零值作为 meta header（flag=0, length=0），不写 meta JSON |
| `ReadEntry` | 签名不变，返回的 `*DeltaMeta` 调用方忽略 |
| `DeltaMeta` struct | 保留（迁移脚本仍需读取旧 delta 文件的 meta） |
| Magic / HeaderSize | 不变 |

兼容性：
- **读旧文件**：flag=1 的 meta 区域正常解析，meta 返回但被调用方丢弃
- **写新文件**：meta header 为 4B 零值，无 meta JSON
- **混合读写**：迁移期间新旧 entry 共存，完全兼容

#### 8. 数据迁移（一次性）

**8.1 `ListDeltaVersions`（`db.go` 新增）**

```sql
SELECT id, file_id, delta_offset, session_id, model, message
FROM versions
WHERE storage_mode = 'delta'
```

返回所有 delta 版本的 `id`, `fileID`, `deltaOffset`, `session_id`, `model`, `message`。

**8.2 迁移逻辑**

启动时执行，遍历所有 delta 版本，从 delta 文件读取元数据后回填到 `versions` 表：

```go
func (h *Handler) migrateMetadataToDB(ctx context.Context) {
    deltaVersions, err := h.DB.ListDeltaVersions(ctx)
    if err != nil {
        h.Logger.Warn("metadata migration: list delta versions failed", "error", err)
        return
    }
    migrated, skipped := 0, 0
    for _, v := range deltaVersions {
        // 幂等：已有数据的跳过
        if v["session_id"] != nil || v["model"] != nil || v["message"] != nil {
            skipped++
            continue
        }
        offset := v["deltaOffset"].(int64)
        fileID := v["fileID"].(int64)
        _, _, meta, err := h.DeltaStore.ReadEntry(fileID, offset)
        if err != nil {
            h.Logger.Warn("metadata migration: read delta failed",
                "version_id", v["id"], "error", err)
            continue
        }
        if meta != nil && (meta.SessionID != "" || meta.Model != "" || meta.Message != "") {
            if err := h.DB.UpdateVersionMeta(ctx, v["id"].(int64),
                meta.SessionID, meta.Model, meta.Message); err != nil {
                h.Logger.Warn("metadata migration: update failed",
                    "version_id", v["id"], "error", err)
                continue
            }
            migrated++
        }
    }
    h.Logger.Info("metadata migration complete",
        "migrated", migrated, "skipped", skipped)
}
```

**调用时机**：`main.go` 中 Handler 创建完成后、HTTP 服务 `ListenAndServe` 之前阻塞执行：

```go
// main.go 插入位置
handler := handler.New(database, blobStore, deltaStore, cfg, logger, fileMuMap)
handler.MigrateMetadataToDB(ctx)  // 阻塞执行，迁移完成前不监听 HTTP
srv := &http.Server{Addr: cfg.Listen, Handler: router}
srv.ListenAndServe()
```

**无竞态**：迁移在 HTTP 端口监听前阻塞完成。迁移期间不接受任何请求，不存在"部分迁移时 ListVersions 读到空元数据"的问题。502 个 delta 版本的迁移耗时在秒级以内。

**注意**：已转换为 blob 的 148 个版本，其原始 delta 文件已被清理，元数据无法恢复。

---

## 收益

1. **Compactor 安全** — 元数据与版本记录绑定，in-place 转换不丢失
2. **Log 查询简化** — 一次 SQL 查询，不再逐条读取 delta 文件
3. **性能提升** — 版本列表从 N 次文件 IO 降为纯 SQL

## 风险

| 风险 | 缓解 |
|---|---|
| SQLite ALTER TABLE 锁表 | 操作毫秒级，影响可忽略 |
| 迁移阻塞启动 | 502 个 delta 版本耗时秒级，可接受；如数据量增长可改为后台异步 + fallback 读取 |
| 已有 blob 版本元数据不可恢复 | 接受损失，仅影响历史数据 |
