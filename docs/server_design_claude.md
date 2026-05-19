# SERVER_DESIGN.md 评审意见

## 整体评价

这是一个**合理且务实**的设计，针对"非 git 管理文件的本地版本追踪"这一场景做了很好的取舍。

**设计亮点**：
- 存储方案成熟（blob+delta+zstd），适合文本版本追踪
- 并发模型简单有效（per-file mutex）
- 工具职责清晰，API 覆盖完整

**需要注意的问题**：

---

## 问题 1：外键约束风险（重要）

```sql
CREATE TABLE files (
  ...
  latest_version_id INTEGER,
  FOREIGN KEY (latest_version_id) REFERENCES versions(id)  -- 问题在这里
);
```

**问题**：首次创建文件时，files 记录插入时 latest_version_id 为 NULL，之后插入 versions 记录，再更新 files.latest_version_id。这个外键本身没问题，但如果严格按 SQL 顺序，versions 表创建后才可插入数据。

**更严重的问题**：versions 表外键
```sql
FOREIGN KEY (base_id) REFERENCES versions(id)
```
第一个版本的 base_id = NULL，这没问题。但 SQLite 默认外键约束是关闭的，需要 `PRAGMA foreign_keys = ON;` 才能生效。

**建议**：
- 在数据库初始化时执行 `PRAGMA foreign_keys = ON;`
- 或在应用层手动维护一致性，不依赖数据库外键

---

## 问题 2：API 路径设计存在 URL 编码风险

```
GET /api/files/:path/versions
GET /api/files/:path/restore/:version
GET /api/files/:path/diff?from=A&to=B
```

**问题**：文件路径可能包含 `/`、`%`、`空格`等特殊字符，作为 URL path segment 时需要正确编码，否则路由解析会出错。

**建议**：
- 明确文档要求路径必须经过 URL 编码（encodeURIComponent）
- 或改为查询参数方式：`/api/versions?path=...`

---

## 问题 3：数据一致性风险（重要）

snapshot 流程涉及多个步骤：
1. 加 per-file 锁
2. 查最新版本、比 hash
3. 计算 delta、压缩
4. 写 blobs/ 或 deltas/ 文件
5. 插入 versions 表
6. 更新 files.latest_version_id
7. 释放锁

**问题**：步骤 4 和 5/6 之间服务崩溃，会导致磁盘有数据但数据库无记录（孤儿文件）；或先写数据库再写磁盘失败，导致记录指向不存在的 blob。

**建议**：
- 采用"先写磁盘，再写数据库"的顺序
- 定期清理孤儿文件（ blobs/ 中的文件在 versions 表中无引用的）
- compact 流程也要考虑一致性

---

## 问题 4：compact 策略有数据丢失风险

设计中的 compact（方案 B）：
```
1. 回溯最新版本...重建完整内容
2. 计算 SHA256，zstd 压缩，写入 blobs/{sha256}
3. 就地更新该版本的 versions 记录
```

**问题**：如果在步骤 2 完成、步骤 3 未完成时崩溃，新 blob 已写入但数据库未更新，compact 可以重做。但如果步骤 3 部分完成（如只更新了 storage_mode，没更新 blob_hash），数据会不一致。

**更严重的问题**：步骤 3"就地更新该版本"，将 delta 版本改为 blob 版本。这意味着该版本的 base_id 被设为 NULL。如果此时服务崩溃，后续版本（如果有的话）的 delta 链会断裂，因为它们依赖这个版本的 base_id。

**建议**：
- compact 操作需要事务保护
- 或采用非破坏性方式：生成新 blob 版本，插入为新记录，标记旧链为 archived
- 这样即使失败，也只是多了一个孤儿 blob，不会破坏现有链

---

## 问题 5：hash 比较性能问题

delta 模式下需要重建内容才能比 hash：
```
blob 模式直接用 blob_hash 比对，
delta 模式需先重建内容再算 hash（此时可直接跳过 delta 计算）
```

**问题**：如果 delta 链很长，每次 snapshot 都要重建完整内容才能比 hash，性能会下降。

**建议**：
- 在 versions 表中增加 content_hash 字段，记录该版本的完整内容 hash
- 这样 delta 模式也能直接比 hash，无需重建
- 这会略微增加存储，但显著提升性能

---

## 问题 6：diff 工具的性能优化点

```
若两版本相邻且 versionB 为 delta 模式 → 从 delta entry 读取 []Diff，渲染为 unified diff 返回
```

**问题**：这个优化只适用于相邻版本。如果 versionA 和 versionB 不相邻，需要分别重建两个完整内容再 DiffMain。

**建议**：
- 这是合理的设计，无需修改
- 只是实现时注意：diff 结果不要存储，现算现返回

---

## 问题 7：delete 版本的处理细节

```
delete action：复制上一版本的 storage_mode/blob_hash/delta_offset，
写入 action=delete 版本记录（不计算 delta，base_id = 上一版本 id）
```

**问题**：delete 版本复制了上一版本的 blob_hash/delta_offset，但 blob_hash 指向的是旧内容的 blob。restore 时如果检测到 delete 版本要报错，这没问题。但 changez_log 时如何展示 delete 版本？它不应该有 content。

**建议**：
- delete 版本的 blob_hash/delta_offset 应该设为 NULL，表示无内容
- 或明确约定 delete 版本不可 restore，log 时只显示 action=delete 标记

---

## 问题 8：并发 compact 与 snapshot 的竞争

compact 流程：
```
1. 锁定该文件（per-file mutex）
2. 回溯最新版本...
3. 就地更新该版本的 versions 记录
...
7. 释放锁
```

**问题**：compact 和 snapshot 共用同一个 per-file mutex，这意味着 compact 期间无法 snapshot。如果 compact 耗时较长（大文件、长链），会阻塞客户端请求。

**建议**：
- 使用读写锁（RWMutex）：snapshot 读锁，compact 写锁
- 或 compact 在后台进行，用 copy-on-write 策略（生成新 blob，原子替换指针）

---

## 问题 9：定时 compact 的实现细节

```
定时器：每 24h 检查所有文件，处理写入时没赶上的链过长
```

**问题**：如果文件很多，遍历所有文件检查 delta 链长度可能耗时很长。

**建议**：
- 维护一个"需要 compact 的文件队列"
- 写入时发现链过长就加入队列，定时器只处理队列中的文件
- 或增加数据库索引优化查询

---

## 问题 10：配置项的合理性

```yaml
compact:
  max_delta_chain: 50
  delta_compress_threshold: 512
```

**问题**：
- `max_delta_chain: 50` —— 如果文件频繁修改（如每秒一次），50 个版本只覆盖 50 秒，compact 会很频繁
- `delta_compress_threshold: 512` —— diff 序列化后 ≤512B 不压缩，这个阈值可能偏小（zstd 对 JSON 也有压缩效果）

**建议**：
- 将默认值调高（如 max_delta_chain=1000，threshold=4096）
- 或根据实际测试调整

---

## 修订后的评分

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构合理性 | 良好 | 整体分层清晰 |
| 存储效率 | 良好 | blob+delta+zstd 适合文本场景 |
| 并发安全 | 一般 | per-file mutex 简单但有阻塞风险，需考虑 RWMutex |
| 数据一致性 | 需关注 | compact 就地更新有风险，建议事务保护或 COW |
| 可维护性 | 良好 | 表结构简单 |
| 扩展性 | 一般 | 预留了扩展空间 |

---

## 结论

**设计可以按此实现，但需关注以下实现细节**：

1. **数据一致性**：snapshot 和 compact 都需要考虑崩溃恢复，建议先写磁盘再写数据库
2. **compact 安全性**：就地更新版本记录有风险，建议改为非破坏性方式或加强事务保护
3. **API 路径编码**：明确 URL 路径的编码要求
4. **性能优化**：考虑增加 content_hash 字段避免频繁重建
5. **外键约束**：记得启用 SQLite 外键检查，或在应用层维护一致性