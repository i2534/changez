# SERVER_DESIGN.md 设计评审

> 评审日期：2026-05-16
> 范围：仅针对 `SERVER_DESIGN.md` 设计文档本身

---

## 总体评价

设计质量很高。思路清晰、技术选型合理、边界情况考虑充分、决策有记录。核心路径完整，备选方案的取舍有明确原因。

---

## 需要关注的 6 个问题

### 1. per-file mutex 实现细节 — 值类型 vs 指针类型

**问题**：设计说"Go `sync.Map` of `sync.Mutex`"。但 `sync.Mutex` 是**值类型**，存入 `sync.Map` 后每次 `Load` 返回的是副本，各 goroutine 拿到的是不同 mutex，失去互斥效果。

```go
// ❌ 错误 — 值类型，Load 返回副本
mu := sync.Mutex{}
m.LoadOrStore(key, mu)   // 存入的是副本
mu.Lock()                 // 锁的是本地副本，不是 map 中的

// ✅ 正确 — 用指针
mu := &sync.Mutex{}
actual, _ := m.LoadOrStore(key, mu)
actual.(*sync.Mutex).Lock()
```

**建议**：明确改为 `sync.Map` of `*sync.Mutex`，或用 `map[FileID]*sync.Mutex` + `sync.Mutex` 保护 map 本身。

---

### 2. 并发读写锁缺失

**问题**：per-file mutex 只保护 **snapshot 写入**流程。但 `changez_restore` / `changez_log` / `changez_diff` 需要**读取** delta 文件。同一个 `deltas/{file_id}.delta` 文件，snapshot 追加写入 + restore 随机读取同时发生时，可能读到不完整的 entry（部分写入状态）。

**建议**：用 `sync.RWMutex`（读写锁）替代 `sync.Mutex`：
- snapshot 写 → `Lock()`（排他）
- restore/log/diff 读 → `RLock()`（共享）

---

### 3. max_file_size 校验位置不明

**问题**：配置定义了 `max_file_size: 10485760`（10MB），但在 snapshot 执行流程中（第 277-293 行），没有提到在哪个环节校验 content 大小、超限时返回什么错误码。

**建议**：在 snapshot 流程中明确：
- 校验时机：收到 content 后、写入 blob/delta 之前
- 超限行为：返回 `INVALID_REQUEST` 错误，跳过该文件

---

### 4. metadata 区域格式描述模糊

**问题**：Delta header 中 `meta_length` 字段（4B）的描述说"`[1B flag][3B length]`"。该描述存在歧义：

- 是指 `meta_length` 这个 uint32 字段内部拆成 `[1B flag | 3B length]`？
- 还是指 `meta_length` **之后**的 metadata 区域以 `[1B flag | 3B length]` 开头？

**建议**：统一描述方式。如果 `meta_length = 0` 就代表无 metadata，这是最直观的做法，无需拆 flag 位。

---

### 5. delta 压缩兜底策略

**问题**：第 288 行描述：

> `delta_compress_threshold` 判断：[]Diff 序列化后的原始字节 ≤512B 则无压缩，否则 zstd

对于某些小数据，zstd 压缩后体积可能**反超**原始体积（压缩膨胀）。当前逻辑没有处理这种情况。

**建议**：加一个兜底判断："如果压缩后体积 ≥ 原始体积，则存储未压缩版本"。header 中的 `compress_method` 字段已预留 `0x0000`（无压缩），格式上支持。

---

### 6. blobs 目录孤儿文件积累

**问题**：Compact 机制创建新的 blob 文件后（第 203 行），旧的 blob 文件不再被任何版本引用。设计说"旧 delta 不主动清理"，但没有提旧 blob 文件的清理策略。blob 是完整文件，体积较大，长期运行会积累无用磁盘占用。

**建议**：
- Phase 1：可接受，标注为已知限制
- Phase 2：可考虑 reference-count 清理（blob 被多少版本引用，归零后删除）
- 或简单方案：增加 `--prune` 命令手动清理

---

## 设计亮点

| 决策 | 优点 |
|------|------|
| path 相对路径存储 | 项目搬位置历史不丢 |
| delete action 继承 blob_hash | 防止链路中断，后续恢复可追溯 |
| project 路径允许重叠 + 最长前缀匹配 | 灵活且合理 |
| 双重 action 判断（客户端 + 服务端） | 鲁棒性好，客户端传错可修正 |
| snapshot 返回顺序对应请求顺序 | 客户端按索引匹配，简单可靠 |
| changez_restore 只返回不写盘 | 职责单一，写盘由客户端决定 |
| action 用 versions 记录，不删 file 记录 | 历史连续，一条时间线 |
| go-diff 存储 []Diff 序列化（非 unified diff 文本） | 存储/apply 直接用，返回时才渲染 |
| compact 就地转换（方案 B） | 版本号连续，旧链保留可读 |

---

## 结论

核心设计合理，可直接进入实现。上述 6 个问题中：

- **#1 #2 #3** → 建议实现前修正（影响正确性或完整性）
- **#4 #5** → 描述性模糊，建议澄清
- **#6** → 后续版本考虑，Phase 1 可标注为 known limitation
