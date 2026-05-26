# Changez 代码与文档 Review

> 初次 Review：2026-05-20
> 二次 Review（含修复结果）：2026-05-20
> 范围：`cmd/`、`internal/`、`client/`、`docs/`、`Makefile`、`go.mod`
> 验证：`gofmt -l .` 干净；`go vet ./...`、`go build ./...`、`go test ./...` 全绿

---

## 状态总览

| # | 优先级 | 问题 | 最终状态 |
|---|--------|------|----------|
| 1 | ~~P0~~ | claude-code hook 上报"旧"内容 | **误判，已撤销**——`originalFile` 实机验证为"编辑后"内容；已加注释 |
| 2 | P0 | `dbcheck` 路径错误 | ✅ 已修复 |
| 3 | P1 | `router.writeError` 错误格式与文档不符 | ✅ 已修复（嵌套 `error` 对象） |
| 4 | P1 | `baseID` typed-nil 陷阱（多处） | ✅ 已修复（统一通过 `dbutil.AsInt64Ptr / AsStringPtr`） |
| 5 | P2 | `storage_mode` 文档不一致 | ✅ 已修复 |
| 6 | P2 | claude-code 设计文档与实现分叉 | ✅ 已大幅同步 |
| 7 | P3 | `snapshot.go` 冗余 hash 检查 + 变量遮蔽 | ✅ 已修复（switch 分支重构） |
| 8 | P3 | `delta.go` metaHeader 死代码 | ✅ 已删 |
| 9 | P3 | URL 路径解析重复（`extractPathFromURL` / `parseRestorePath`） | ⚪ 暂未处理（不阻塞） |
| 10 | P3 | `config.FindConfig` 无人调用 | ✅ 已修复 |
| 11 | P3 | 仓库残留二进制/日志 | ⚪ 暂未处理 |
| 12 | P3 | `0644` → `0o644` 风格统一 | ✅ 主要文件已修复 |

---

## 关键修复回顾

### 1. claude-code hook 的 `originalFile` 字段语义（误判勘误）

实机验证表明 Edit 工具的 `tool_response.originalFile` 实际携带的是**编辑后**的完整文件内容（命名反直觉）。原 review 中根据字段名做出的"上报旧内容"判断不成立。已在 `client/claude-code/changez-hook.js` 加上 inline 注释，避免后人再次误读：

```javascript
// 注意：Edit 工具的 `originalFile` 字段命名反直觉——实机验证里它装的是
// **编辑后**的完整文件内容，并非"编辑前"的原始内容。详见
// docs/claude_code_plugin_design.md "Edit vs Write 文件内容获取策略"。
```

### 2. typed-nil 陷阱与代码去重

新增公共包 `internal/dbutil/`，提供：

- `AsInt64Ptr(v any) (int64, bool)`
- `AsStringPtr(v any) (string, bool)`

实现不再依赖 `reflect`，直接借类型断言失败时 `p` 为 typed nil 的特性做单次 nil 检查，更短更快。`compact` 与 `handler` 包通过 `var asInt64Ptr = dbutil.AsInt64Ptr` 复用，调用点不变；并配套 `dbutil/ptr_test.go` 覆盖五种边界（nil interface / typed nil / 正常值 / 错类型 / 错指针类型）。

修复涉及的调用点：

- `internal/compact/compactor.go`：`checkAndCompactLocked`、`rebuildContent`、`rebuildFromDeltaChain`
- `internal/handler/snapshot.go`：`rebuildContent`、`rebuildFromDeltaChain`
- `internal/handler/log.go`：`readDeltaMeta` 中 `*offset.(*int64)`
- `internal/handler/diff.go`：相邻 delta 判定中的 `baseID` / `deltaOffset`
- `internal/handler/mcp.go`（即 `ProcessSnapshot`）：blob hash 比较

### 3. `ProcessSnapshot` 的 hash 比对重构

原代码在 blob 模式下做了一次 `blob_hash` 比对，紧接着又对 blob/delta 都重建一次完整内容再算 hash，blob 路径属于冗余，且内层 `if h := latestVer["blobHash"]; h != nil` 还遮蔽了外层 `*Handler h`。重构成 `switch storageMode` 两条分支：

- `blob`：直接比对 `blob_hash`（通过 `asStringPtr`）。
- `delta`：重建上一版本完整内容再算 hash。

### 4. `gofmt` 缩进破坏

`internal/compact/compactor.go` 在引入 typed-nil 修复时 for 循环出现混合缩进（gofmt 报告不合规）。`gofmt -w` 已经一次性修复，并把整个仓库扫了一遍确保无遗漏。

### 5. 路径与构建产物

- `Makefile`：构建产物输出到 `dist/`；`clean` 用 `rm -rf $(DIST)`。
- `.gitignore`：`changez` 改 `dist/`，`*.pid` / `*.log` 通配。
- `cmd/dbcheck/main.go`：`db.Open` 接收的是 `dataDir`，已改为传 `data` 目录。

---

## 仍待处理的轻量项

| 项 | 建议 |
|----|------|
| `extractPathFromURL` / `parseRestorePath` 重复 | 抽到公共 helper（与 `dbutil` 同思路） |
| `config.FindConfig` 死代码 | 要么在 `cmd/changez/main.go` 实际接入（让 `-c` 支持相对路径），要么删 |
| 根目录残留 `changez` 二进制 / `changez.log` | 一次性清掉，后续走 `dist/` |
| `internal/**/changez.log` 测试副产物 | 测试改用 `t.TempDir()` 或在 `.gitignore` 加 `internal/**/*.log` |
| `cmd/dbcheck/main.go` | `_ = sql.ErrNoRows` 这条语句可去除，移除多余的 `database/sql` 导入；并把硬编码路径改成 `flag.String("dir", "data", ...)` |

这些都不阻塞，可在后续清理回合一起做。

---

## 整体亮点

- `db.Open` → `RecoverOrphans` → `RemoveOrphanBlobs` 的启动恢复链路设计周全。
- per-file `sync.RWMutex` + `sync.Map` 在 handler 与 compactor 之间共享，并发设计踩点准确。
- 写 delta 走 `CreateVersion` → `DeltaStore.Append` → `UpdateVersionStorage` 三步事务，配合启动 RecoverOrphans，是一套完整的 crash safety 流程。
- HTTP / MCP 共享 `Process*` 内核（snapshot / log / restore / diff），双协议出口语义一致。
- 测试覆盖了 handler/router/db/storage/compact/mcp 全部包，新增 `dbutil` 也补齐了单测。
