# 孤立 Delta 文件分析

## 背景

Compactor 将长 delta 链合并为 blob 检查点后，磁盘上残留了对应的 delta 文件。本次清理了 29 个孤立文件，剩余 71 个 delta 文件与 DB 中的 71 个有 delta 版本的文件一一对应。

## 当前状态

| 指标 | 数值 |
|---|---|
| DB 中 delta 版本数 | 610 |
| DB 中 blob 版本数 | 163 |
| DB 中已删除版本数 | 10 |
| 磁盘 delta 文件数（清理前） | 100 |
| 磁盘 delta 文件数（清理后） | 71 |
| 孤立 delta 文件（已清理） | 29 |

## 根因分析

### Compactor 行为 (`compact/compactor.go`)

`compactLatest` (L157-176) 的工作流程：

```
1. rebuildContent(latest)  — 从 delta 链回溯到 blob checkpoint，重建完整内容
2. blobStore.Store(content) — 存储为新 blob
3. UpdateVersionStorage(versionID, "blob", blobHash, nil) — 更新 DB 记录
4. 返回（不删除磁盘上的 delta 文件）
```

**问题**：只更新 DB，不删除磁盘文件。

### 孤立文件的产生路径

假设 file_id=42 的 delta 链：

```
v1(blob) → v2(delta) → v3(delta) → v4(delta) → v5(delta)
```

Compactor 触发（delta 链长度 >= 50）：

```
v1(blob) → v2(delta) → v3(delta) → v4(delta) → v5(blob)  ← v5 转为 blob
```

此时 `42.delta` 仍然被 v2-v4 引用，**不是孤立文件**。

但随着更多版本写入和 compactor 再次运行：

```
v1(blob) → v5(blob) → v6(delta) → v7(delta) → v8(blob)
```

当最后一个 delta 版本也被转换：

```
v1(blob) → v5(blob) → v8(blob) → v9(blob)
```

此时 `42.delta` **没有任何 delta 版本引用**，变成孤立文件。

### 现有 Cleanup 的覆盖范围 (`cleanup/cleanup.go`)

`cleanupOrphanDeltas` (L191-232) 的逻辑：

```go
// 1. 读取 files 表所有 file_id
rows, _ := c.db.Query(ctx, "SELECT id FROM files")
fileIDs := make(map[int64]bool)

// 2. 遍历磁盘 delta 文件，删除 file_id 不在 files 表中的文件
for _, entry := range entries {
    fmt.Sscanf(entry.Name(), "%d.delta", &fileID)
    if !fileIDs[fileID] {
        os.Remove(path)  // 只删除 file_id 不存在的
    }
}
```

**盲区**：只能清理 file_id 已被删除的 delta 文件。对于 file_id 仍存在但所有 delta 版本都已转换为 blob 的情况，**无法清理**。

## 影响评估

### 已清理的 29 个文件是否安全？

**安全。** 验证方法：

1. 遍历所有 610 个 delta 版本，提取 `file_id` + `delta_offset`
2. 检查每个 offset 是否指向有效的 delta magic (`0x43440001`)
3. 确认所有 delta 版本引用的文件在磁盘上存在

结论：610 个 delta 版本引用的 71 个文件全部有效，29 个被清理的文件没有任何版本引用。

### 未来影响

| 场景 | 是否产生孤立文件 | 说明 |
|---|---|---|
| Compactor 转换 delta→blob | ⚠️ 极少 | Compactor 每次只转最新版本，需全链逐步转换完毕才产生孤立文件 |
| 文件软删除 + cleanup | ❌ 不会 | `cleanupDeletedFiles` 会删除 delta 文件 |
| 项目软删除 + cleanup | ❌ 不会 | `cleanupDeletedProjects` 会删除 delta 文件 |

孤立文件属于历史遗留数据。Compactor 每次仅转换最新版本为 blob，中间 delta 版本仍引用原 delta 文件，只有全链全部转换完毕后才会产生孤立文件。实际发生频率极低。

## 修复方案

### 方案 A：增强 cleanupOrphanDeltas（推荐）

在现有逻辑基础上增加一层检查：

```go
func (c *Cleanup) cleanupOrphanDeltas(ctx context.Context) error {
    // 1. 获取所有有 delta 版本的 file_id
    rows, _ := c.db.Query(ctx, `
        SELECT DISTINCT file_id FROM versions WHERE storage_mode='delta'
    `)
    deltaFileIDs := make(map[int64]bool)
    for rows.Next() { /* ... */ }
    rows.Close()

    // 2. 遍历磁盘，删除不在 deltaFileIDs 中的文件
    for _, entry := range entries {
        fmt.Sscanf(entry.Name(), "%d.delta", &fileID)
        if !deltaFileIDs[fileID] {
            os.Remove(path)
        }
    }
}
```

**优点**：
- 覆盖所有孤立场景（compactor 残留 + file_id 删除）
- 逻辑简单，与现有代码风格一致
- 清理频率与现有 cleanup 一致（默认一周一次）

**缺点**：
- 需要遍历 versions 表（当前 ~780 行，影响可忽略）

### 方案 B：Compactor 转换时删除

在 `compactLatest` 中，转换后检查是否还有 delta 版本引用该文件，若无则删除。

**优点**：即时清理，不依赖 cleanup 定时器。

**缺点**：
- 需要额外查询 DB
- 增加 compactor 逻辑复杂度

### 方案对比

| 维度 | 方案 A（cleanup 增强） | 方案 B（compactor 即时清理） |
|---|---|---|
| 覆盖场景 | compactor 残留 + file_id 删除 | compactor 残留 + file_id 删除 |
| 执行时机 | 定时（默认一周一次） | 即时（compactor 触发时） |
| 逻辑位置 | cleanup 模块（职责集中） | compactor 模块（增加复杂度） |
| DB 查询 | 一次 DISTINCT 查询 | 每次 compact 后查询 |

### 建议

采用**方案 A**。两者覆盖场景相同，方案 A 逻辑集中在 cleanup 模块，职责清晰，实现成本低。
