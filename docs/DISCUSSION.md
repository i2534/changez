# Changez 设计讨论过程记录

> 复盘用。严格按对话时序记录。基于 dialog.txt 还原。

---

## Round 1 — 项目定位

**用户:** 我不是要审计，现阶段只是为了记录，方便查看变更和恢复文件。有点类似于 git

**我:** 提出简化方案：独立于 git、后台 daemon 自动 watch、新变更存完整快照+定时 GC 去重 delta。问触发方式、存储方案。

**用户:** 1. A 为主（独立于 git）2. A（后台 daemon）3. 最近的文件直接存，定时对历史文件进行去重 delta

---

## Round 2 — 架构升级

**我:** 提出 Go 服务 + MCP + HTTP 接口，多个 IDE 共享一个服务。

**用户:** 仍然要做成 go 服务，提供 mcp 和 http 接口。我可能会多个 ide 一起开，统一提供服务。

---

## Round 3 — 核心架构调整

**用户:** 不要进行文件监听，应该使用各个 ide 自己的 api 或方式让它们上报。mcp 只暴露 log, diff, restore 就行，http 可以暴露更多的接口。全局统一一个服务，端口复用。配置位于服务内，包括数据也跟随服务走。

**我:** 调整为主动上报 + 集中存储。snapshot 通过 HTTP POST 上报，服务读取文件内容。MCP 暴露 log/diff/restore。

---

## Round 4 — 命名与 snapshot 设计

**用户:** snapshot 也加入 mcp，并且直接上传文件内容，防止权限或 ide 和服务不在同一主机。audit 这个名字是不是不够贴切？

**我:** 提出命名候选：codetrail, changez, versio, filetrace, snaplog。

**用户:** 暂定为 changez，并且我们暂时只追踪文本内容，二进制不予考虑。

---

## Round 5 — Git 关系讨论

**用户:** git 本地仓库和远程仓库冲突吗？譬如我项目是远程仓库签出的，我有一些本地变更不想纳入远程，使用本地仓库来管理，这会不会冲突？

**我:** 解释不冲突，changez 和 git 是完全独立的层。

**用户:** 我问的是只是用 git，本地仓库和远程仓库在同一项目下，会冲突吗

**我:** 解释 git 正常用法：不 push = 不冲突，pull 时才可能冲突。

**用户:** 也就是说，git 还是有机会冲突的，所以我们的 changez 存在还是有意义的？

**我:** 确认：git 管协作，changez 管备份。git 搞砸了，changez 兜底。

---

## Round 6 — 项目注册与表结构

**我:** 提出 projects 表（项目注册）、files 表（文件追踪）、versions 表（版本记录）。path 存相对路径。

**用户:** 通过 api 来注册吧，服务端尽量少参与各个 ide 的事情

**我:** 提出未注册文件拒绝（方案 A）。

**用户:** gc 改名成更贴切一些。然后 db 的设计我们还要更细节的讨论

**我:** 改名 compact（压缩整理）。提出 change_summaries 合并到 versions。

---

## Round 7 — Delta 链式 vs 星型

**我:** 提出两种 delta 模式：
- A 链式：每个 delta 指向前一个版本，省空间但读取慢
- B 星型：每个 delta 指向最近的 blob checkpoint，浪费空间但读取快

**用户:** 使用链式 A 没问题

---

## Round 8 — 表数量确认

**用户:** 现在 versions 表有几张，一个 project 一张吗？

**我:** 一张全局 versions 表，所有项目共用。3 张表（projects, files, versions）。

**用户:** 逐表进行讨论

---

## Round 9 — projects 表

**我:** 提出 name 谁来决定、root_path 唯一性、extra 字段、删除处理。

**用户:** name 可以传入，没有则根据 path 生成。root_path 不唯一的情况你觉得会在哪种情况下发生。extra 先加上备用。删除只是标记一下，我们肯定要保留保证恢复。

(定稿：name 可传/自动生成，extra JSON 备用，软删除 is_deleted + deleted_at)

---

## Round 10 — files 表

**我:** 提出 5 个问题：path 相对/绝对、inode/device、软删除、latest_version_id 维护时机、UNIQUE 策略。

**用户:** 1. 相对路径 2. 服务需要感知重命名吗 3. 直接删除了文件还能恢复吗 4. version id 永远递增 5. 你给几个方案我们讨论

**我:** 提出删除/重建方案：A（不删 file 记 action）vs B（软删除 file）vs C（物理删除）

**用户:** 3. 服务只管 snapshot 上报的，别的不管。5. 方案 A 更好。2. snapshot 上报重命名是不是有些复杂？

(定稿：不感知重命名，方案 A 历史连续，files 表简化无 inode/device/is_deleted)

---

## Round 11 — latest_version_id

**用户:** latest_version_id 是做什么的？

**我:** 快速定位最新版本，避免排序查询。用于快照去重（比 hash）和查当前内容。

**用户:** 保留，继续讨论

---

## Round 12 — versions 表与 sources

**我:** 提出 versions 表完整字段，含 size_bytes/line_count 等。

**用户:** delta 只记录 offset，length 和 compressed 都在数据 header 里标记。size, lines 这些记录是做什么的？

**我:** 精简 versions 表，去掉冗余字段。

**我:** 提出 sources 表（来源标记 lookup table）

(定稿：4 张表 — projects, files, versions, sources。versions 精简为 9 列)

---

## Round 13 — Delta Header 格式

**我:** 提出 header 格式，问字节序和 magic number。

**用户:** 字节序一般的文本和源代码都选择哪种？加一个带 header 版本号的 4b magic number，方便以后更新的兼容

**我:** 大端序。magic + version 合并 4B：`0x43440001`。

**用户:** isCompressed 改为 compressMethod，2b 用来为以后的算法优化做兼容

(定稿：header 14B，magic 4B + length 4B + versionId 4B + compressMethod 2B)

---

## Round 14 — zstd 优势

**用户:** zstd 压缩的好处是什么？

**我:** 对比 gzip/bz2/zstd：解压最快、压缩率高、可调级别 1-22、Go 有纯 Go 实现。

---

## Round 15 — 并发写入方案

**我:** 提出 4 个方案：A 排队串行、B 乐观并发、C 时间戳排序、D 全存 blob。

**用户:** 除了 B，其他三个方案对比

**我:** 详细对比 A/C/D，推荐 A（per-file mutex）。

**用户:** A 看起来确实很好，它记录下的 delta 能看出来是哪个 IDE 做的更改吗？

**我:** 确认：每个版本有 source_id，清清楚楚。

---

## Round 16 — Cherry-pick 讨论

**用户:** 如果我某个 IDE 只想恢复它自己的修改，可以跳过别人的 delta 恢复吗？

**我:** 不能直接跳过（delta 链是累积的）。提出 cherry-pick 需要额外工作。

**用户:** cherry pick 是什么，我们这里实现需要做哪些工作？

**我:** 解释 cherry-pick = 挑选性恢复。需可逆 delta + 中间重新 diff + API 设计。建议 Phase 1 不做。

**用户:** 也就是说，我们只要保证链的完整，并且每个 delta 记录的 source 也对，我们完全可以实现这个功能，只是现在没有必要？

**我:** 确认：数据结构已预留，零成本。

---

## Round 17 — 服务启停

**用户:** 服务启停

**我:** 提出启动流程和关闭流程，5 个讨论点。

**用户:** 1. 不做（启动不 compact）2. 有改动了再考虑（迁移）3. -c 就够，空参数也能默认跑 4. 目前看没有需要 flush 的 5. 10 秒怎么样？

(定稿：启动 6 步，关闭 10s 优雅退出)

---

## Round 18 — MCP Tool Schema 讨论

**用户:** mcp

**我:** 提出 4 个工具的完整 schema。

**用户:** 逐 tool 给我解释，我们讨论

---

## Round 19 — snapshot 工具细化

**我:** 逐字段解释 snapshot，提出 3 个问题：action 客户端/服务端判断、delete 的 content、批量部分失败。

**用户:** 1. 我不确定所有的客户端都能准确区分上报，所以客户端可以自己填，服务端也要做判断。2. 可以省略或空。3. 继续处理，让客户端再单独处理失败的文件。

(定稿：action 双重判断，delete content 可选，部分失败继续处理)

---

## Round 20 — snapshot 返回值格式

**用户:** snapshot 返回值严格按照请求的文件顺序对应结果。session 的信息还需要进一步讨论

**我:** 改为 results 数组按请求顺序一一对应。提出 session 结构化方案（独立 sessions 表）。

**用户:** session 可选，但要增加字段，id, model 等，作为 delta 的扩展数据

**我:** 提出 sessions 表 + versions.session_id 外键。

**用户:** 不存表，作为 delta log 的扩展数据，和 delta 存一起

(定稿：session 数据存在 delta entry 的 metadata 区域，不建 sessions 表)

---

## Round 21 — Delta Header 格式再讨论

**用户:** header 格式我们还需要再讨论，要考虑如果没有 metadata，怎么节省空间，并且要确认 metadata 是否需要压缩

**我:** 提出 metadata_length 用 `[1B flag][3B length]`，flag=0 表示无 metadata（省空间）。metadata 明文不压缩。

---

## Round 22 — 数据结构留到最后

**用户:** 数据结构留到最后详细讨论

---

## Round 23 — changez_log 工具设计

**我:** 提出 inputSchema 和返回格式，抛出 4 个问题：时间格式、action 过滤、分页、sizeBytes。

**用户:**
1. "哪个格式对 AI 友好?" → 选严格 ISO 8601
2. 可选 action 过滤
3. 分页可选
4. 不要

---

## Round 24 — message 字段来源

**用户:** 返回的 message 是哪里来的？

**我:** 来自 snapshot 时写入的 metadata。snapshot 流程写入 delta entry 的 metadata 区域。

---

## Round 25 — 保存讨论成果

**用户:** 为了防止意外，你先把我们讨论的结果保存好。并把我们讨论的过程也保存到一个文件中。

**我:** 保存了 DESIGN.md 和 DISCUSSION.md（但当时只记录了 deltas + versions 两张表，projects 和 sources 丢失了）。

---

## Round 26 — DISCUSSION.md 格式调整（第一次）

**用户:** DISCUSSION.md 记录按照我们讨论的过程记录，而不是记录结果。我要事后复盘用

**我:** 改成 Q&A 对话式，但按主题归类了。

---

## Round 27 — DISCUSSION.md 格式调整（第二次）

**用户:** 可以按照 QA 方式记录。但要根据我们讨论的时序来排列

**我:** 按实际对话轮次重写。

---

## Round 28 — 发现缺失补全

**用户:** 发现 DISCUSSION.md 少了会话压缩之前的讨论

**我:** 确认压缩前摘要生成失败（169 条消息丢失），只能从 DESIGN.md 和记忆反推补写。

**用户:** 尽量补上

(本次补写 Round 1-5，但仍有遗漏)

---

## Round 29 — 移动项目目录

**用户:** 把 changez 移动到 workspace/go 下

**我:** 已移动至 `~/workspace/go/changez/`

---

## Round 30 — DISCUSSION.md 记录规则

**用户:** 你记住，我们的每轮对话你都要补充到 DISCUSSION.md 中

**我:** 记住了，写入记忆。

---

## Round 31 — changez_restore 工具设计

**我:** 提出 design：path + version_id + dry_run，写盘前自动 snapshot？要不要 diff 预览？要不要 source 字段？

**用户:** 服务只管返回，写盘做什么？restore 只负责输出完整内容。

(定稿：restore 是纯读取操作，返回 path, version, timestamp, content。不写盘。)

---

## Round 32 — 发现少了 changez_diff

**用户:** 不是还有一个 diff 吗？

**我:** 对，changez_diff。提出两个版本的 diff 工具设计。问要不要支持省略 from_version。

**用户:** 省略 from_version 的话，IDE 文件和 server 文件不一定在同一主机上，是不是还要把 IDE 的文件上传到服务？

**我:** 提出三种方案：A=独立 changez_diff 工具，B=在 changez_log 里加 with_diff，C=两者都要。

**用户:** 使用方案 A 没问题。

(定稿：独立 changez_diff 工具，path + versionA + versionB 必填，返回 unified diff。)

---

## Round 33 — 工具命名统一

**用户:** 四个工具的命名需要统一

**我:** `snapshot` 没有前缀，其他三个都是 `changez_`。统一加前缀。

(定稿：`changez_snapshot`, `changez_log`, `changez_restore`, `changez_diff`)

---

## Round 34 — 服务架构与数据目录

**讨论要点:**
- 源代码放 `src/` 下
- MCP 用第三方库 `github.com/mark3labs/mcp-go`
- 端口、认证、数据目录通过 config.yaml 配置
- 所有配置文件和数据文件和服务在同级目录

**用户:** db 文件也放到 data 下

---

## Round 35 — snapshot 的 content 字段

**我:** snapshot 要不要加 content 字段？如果服务跑在独立主机上，文件不在本地怎么办？

**用户:** 我们是服务，需要用户传递 content 过来，我们不能直接读取用户的任何文件

(定稿：snapshot 的 inputSchema 加 `content` 字段，path + content 必填。服务不碰文件系统。)

---

## Round 36 — 全面检查：服务不碰文件系统

**用户:** 基于服务不能直接读写用户文件这一点，彻底检查一遍前面的设计和结论

**检查发现两处违反：**
1. snapshot 流程："用 `diff` 命令生成 unified diff" → 调了外部命令
2. changez_diff 流程：同上

**修正：** 两处改为"在内存中生成 unified diff"，用 `github.com/sergi/go-diff` 库。

---

## Round 37 — metadata 存储位置调整

**用户:** metadata 跟随每条 delta log，不要放到表里

**我:** 提出 versions 表删掉 sessionId 和 message，只保留 source（查询过滤用）。

**用户:** source 保留在表中，不要放到 delta metadata 中

(定稿：source 在 sources 表，sessionId 和 message 在 delta entry metadata 区域)

---

## Round 38 — 基于 dialog.txt 完整还原

**用户:** 把完整对话放在 dialog.txt 中，去分析并更新两个文档

**我:** 读取 dialog.txt（5520 行），发现原始设计是 4 张表（projects, files, versions, sources）。
完整还原了所有讨论内容，更新 DESIGN.md 和 DISCUSSION.md。

---

## Round 39 — 待讨论项补齐

**用户:** 根据对话历史，确定 4 个待讨论项。

**我:** 通读 dialog.txt，发现这 4 项在对话中已有结论，只是没整理进文档：

1. **Delta header 字段顺序和命名** — 用户在 Round 21 要求 `deltaLength` 和 `metaLength` 挨在一起放到 `compressMethod` 后，Round 22 要求统一 snake_case。DESIGN.md 的 header 图示未更新（仍用旧版 camelCase + 旧顺序）。已修正。

2. **Snapshot 的 session 参数结构** — Round 20 定稿为 `{id, model}` 对象，不建 sessions 表，存在 delta metadata 区域。DESIGN.md 已正确。

3. **HTTP API 详细路由** — Round 17 已定稿 10 个路由，MCP 和 HTTP 共用 token 鉴权。DESIGN.md 已有。

4. **错误处理与幂等性** — 幂等通过 SHA256 hash 去重（snapshot 步骤 3 已实现），per-file mutex 天然防并发冲突。错误格式未定义，新增统一 `{error: {code, message}}` 格式 + 5 个错误码。

(更新 DESIGN.md：修正 delta header 图示、新增错误处理与幂等性章节、补充设计决策表)

---

## Round 40 — Session Bridge 插件实现

**用户:** 读取 SESSION-BRIDGE-DESIGN.md，要实现它

**我:** 验证了源码中三个 hook 的实际签名：
- `on_session_start` (line 12081): `session_id, model, platform`
- `post_llm_call` (line 15577): `session_id, user_message, assistant_response, conversation_history, model, platform`
- `on_session_end` (line 15692): `session_id, completed, interrupted, model, platform`

**设计审查发现:**
1. `provides_hooks` 字段 Hermes 不消费，保留仅作文档用途
2. index 中的 `topic` 字段没有生成机制，暂时去掉
3. `on_session_end` 实际上每个 turn 结束后都会触发（不是仅在 session 真正结束时），但因为核心数据在 `post_llm_call` 已持久化，不影响功能
4. conversation_history 确实包含完整消息列表（含 reasoning/tool_calls），与源码确认一致

**实现内容:**
- 创建 `~/.hermes/plugins/session-bridge/` 目录
- 写入 `plugin.yaml`（standalone 插件）
- 写入 `__init__.py`（register + 3个 hook 回调）
- 在 `config.yaml` 的 `plugins.enabled` 中添加 `session-bridge`

**注意事项:**
- index 文件的更新是 O(n) 操作（读取全量后写回），当前量级可接受
- 数据存储在 `~/.hermes/session-bridge/sessions/`，每 turn 写盘一次（tmp+rename 原子写入）
- 需要新 session 才能加载插件（Hermes 在 session 启动时加载插件）

(完成：插件已创建并启用，用户重启 Hermes 后即可生效)

---

## Round 41 — 文档一致性审查与 Compact 机制

**我:** 通读 DESIGN.md 发现 5 处不一致：
1. versions 表有 session_key 和 message 字段（应删，存在 delta metadata）
2. snapshot inputSchema required 应为 `["path", "content"]` 非 `["path", "action"]`
3. changez_diff step 3 链式模式下 delta 只存在于相邻版本之间
4. snapshot 执行流程缺 blob 写入步骤
5. metadata JSON session 结构与 log 返回不一致

**修正:** 全部修复。session 参数从嵌套对象扁平化为 `sessionId` + `model`。

**Compact 讨论:**
- 提出方案 A（追加新 checkpoint）vs 方案 B（就地转换最新版本）
- **用户选择方案 B**：版本号连续，旧链可读，旧 delta 不清理

**Blob 文件格式:**
- **用户要求:** 和 delta 一样预留压缩算法更换能力
- **定稿:** 6B header（magic 4B `0x424C0001` + compress_method 2B）

---

## Round 42 — Go 项目结构与 diff/patch 库

**Go 项目结构:**
- **用户:** 典型 Go 项目结构即可，cmd 下不需要再加一层 changez
- **定稿:** `cmd/main.go` + `internal/` (config, db, storage, server)

**diff/patch 库:**
- 验证 go-diff 的 PatchApply 可用，但 patch 格式非标准 unified diff
- **用户指出:** MCP diff tool 必须返回 unified diff
- **讨论结论:** delta 存 `[]Diff` 原始格式（方便 apply），仅返回时渲染 unified diff
- PatchApply 由 go-diff 完成，unified diff 渲染器自实现

**其他确认:**
1. delete action 记录继承上一版本 storage_mode/blob_hash/delta_offset（防链路中断）
2. 禁止 project 路径重叠（注册时检查父子关系）
3. /api/stats 仅返回 projects/files/versions 数量
4. 加 GET /health 健康检查端点
5. delta_compress_threshold 先用 512B

---

## 待讨论

- Project 路径重叠的具体校验逻辑
- 开始编写 Go 代码？

---

## Phase 1 开发启动 (2026-05-16)

- 设计文档审计完成（Claude + OpenCode 双审）
- 文档已归档至 docs/ 目录
- opencode 后台启动，模型: deepseek/deepseek-v4-flash
- 任务范围：完整服务端实现（config, db, storage, handler, main）
- 设计文档：docs/SERVER_DESIGN.md

---

## Round 43 — 代码审查与 Bug 修复

**审查发现:**

1. **路径匹配 Bug**: `extractPathFromURL` 给路径加了前导 `/`，导致 `/src/hello.txt` 无法匹配项目 root_path `/tmp/testproj`。`FindProjectByPath` 需要同时支持 files 表精确匹配（handler 场景）和 root_path 前缀匹配（snapshot/测试场景）。

2. **修复方案**: `FindProjectByPath` 改为双模式——先查 files 表精确匹配，失败后回退到 root_path 前缀匹配。Handler 中 path 可能是绝对或相对路径，统一通过 root_path 计算 relPath 后再查文件。

3. **验证结果**: 编译通过，全部测试通过（compact/config/db/handler/mcp/router/storage），E2E 端到端测试通过。

**最终状态:**
- 编译: OK (binary 17MB)
- 测试: 7 个包全部 PASS
- E2E: create project → snapshot v1/v2 → version log → restore → diff 全部正常

## Round 44 — OpenCode Plugin 集成方案设计

**用户:** 我们继续讨论 notice/hermes 会话讨论的方案。先以 opencode 开始。

**调研发现:**
- opencode 1.15.2，插件 API 在 `~/.config/opencode/node_modules/@opencode-ai/plugin/dist/index.d.ts`
- 有 `tool.execute.before` 和 `tool.execute.after` 两个 hook
- 现有 backup 插件（file-backup-plugin.ts）使用 before hook，在修改前备份文件
- `tool.execute.after` 签名：`input: { tool, sessionID, callID, args }`, `output: { title, output, metadata }`
- `chat.message` hook 可获取 model 信息：`{ providerID, modelID }`

**用户:** 独立插件，放哪里看用户自己的选择，在我这里可以放全局。配置统一放到配置文件中。把设计文档写入到 docs 下。

**我:** 初版设计用了环境变量 + 服务 config.yaml 新增 plugin 段。

**用户纠正:** 配置按照 opencode 插件的规范放到指定位置，各个 IDE 的 plugin 实际只是通过 API 和 changez 产生联系，它们的配置不必和 changez 放在一起。

**修正后设计:**
- 插件配置走 opencode 规范：`opencode.json` 的 `plugin` 数组
- 插件通过 `options` 参数接收配置，不读环境变量
- changez 服务端**不需要任何配置改动或新增端点**

**用户:** opencode.json 中就叫 `changez`。1. action 区分 create/update/delete。2-4 进一步讨论 3（project 注册）。

**Project 注册讨论:**
- 提出 4 个方案：A 手动 / B 启动时自动 / C 惰性 / D 服务端自动
- **用户选择方案 B**：插件启动时自动注册 project，幂等（已存在则忽略）
- project name 在配置中可选指定，省略则用 directory 的 basename

**输出文档:** docs/opencode_plugin_design.md（已更新）

---

## Round 45 — OpenCode Plugin 细节讨论

**Action 区分:**
- 用户要求区分 create/update/delete
- `apply_patch` 从 patch 文本直接判断（Add/Update/Delete File）
- `bash rm` 明显是 delete
- `edit/multiedit` 是 update
- `write` 较难区分（after hook 时文件已存在），统一传 `update`，服务端兜底

**Debouncing:**
- 用户明确不需要 debouncing，每次修改独立上报

**日志级别:**
- 用户要求插件内部做级别过滤
- 配置项 `logLevel`：`debug` / `info` / `warn` / `error`，默认 `info`
- debug: snapshot 请求详情；info: 加载/注册成功；warn: 失败/异常；error: 代码 bug

**(更新 docs/opencode_plugin_design.md)**

---

## Round 46 — 设计文档审查

**审查发现 7 个问题：**

1. **类型名冲突**：`PluginOptions` 与 SDK 重名 → 改为 `ChangezConfig`，options 用 `Record<string, unknown>`
2. **`chat.message` hook 签名少 output 参数** → 补上 `(input, _output)`
3. **action 表格与结论矛盾**：write 写"文件不存在→create" → 统一改为 `update`
4. **`multiedit` 代码空缺** → 补上 `collectMultieditPaths`，检查 `edits/changes/files` 三个 key
5. **bash command 缺 fallback** → 增加 `args.bash_command` 回退
6. **"Map 自动清理"不实** → 改为"插件生命周期内存在，无需显式清理"
7. **导出名**：`Changez` → `ChangezPlugin`

**同时删除了 `determineAction` 函数代码块**（已用简化方案替代，保留旧代码造成混淆）

**(更新 docs/opencode_plugin_design.md)**

1. **`chat.message` 没返回 Promise** → 加 `async`
2. **网络超时声明了但没实现** → `httpRequest` 补完整实现（`AbortController` + 5s `setTimeout`）
3. **文件读取失败完全静默** → catch 中加 `log("debug", ...)`
4. **`args.bash_command === null` 边界** → 加 `!= null` 守卫
5. **大文件没提前过滤** → `statSync` 预检查，>10MB 直接 skip

**确认 OK 的：**
- `sessionModels` 模块级变量不会跨 workspace 混用
- 绝对路径与服务端 root_path 前缀匹配兼容
- snapshot 请求多余字段服务端自动忽略
- fire-and-forget 并发安全（服务端 per-file mutex + SHA256 幂等）

---

## Round 48 — Cursor Plugin 方案设计

**用户:** 准备开始设计 Cursor IDE 内的实现计划。

**调研发现:**
- Cursor 基于 VS Code，使用标准 VS Code 扩展 API
- Cursor **没有**提供类似 OpenCode `tool.execute.after` 的 AI 专属 hook
- Cursor 文档中的 "Hooks" 是 git hooks，不是 AI 事件钩子
- Cursor 的 "Skills" 是按需加载的能力声明，不如 Rules 可靠
- Cursor 支持 MCP server 集成（SSE/stdio 模式）

**讨论路径:**

1. **初始方案：VS Code 扩展** — FileSystemWatcher 监听文件变更，异步上报 snapshot。用户问 FileSystemWatcher 是否有性能问题。

2. **用户提出替代方案：Skill + MCP** — 让 AI 在修改文件后主动调用 changez_snapshot MCP 工具。

3. **Rule vs Skill 讨论** — Rule (`.cursorrules`) 比 Skill 更可靠，因为 Rule 每轮必注入，Skill 是按需加载。

4. **Rule + MCP 的问题暴露：**
   - `changez_snapshot` 需要完整文件 content
   - AI 做 partial edit 后不一定有全文，需要额外 read 操作
   - Rule 是概率性指令，AI 可能忘记调用
   - 每次修改多一次 read + snapshot 调用，浪费 token 和增加延迟

5. **用户回归 VS Code 扩展方案** — 无需用户干预、零 token 开销、确定性触发。

6. **FileSystemWatcher 性能讨论** — 用户担心性能问题。确认 VS Code 的 FileSystemWatcher 基于 OS 原生通知（inotify），性能没问题。需要 per-file debounce 防止同一文件多次事件。

**定稿方案：VS Code 扩展，双通道检测**

- **主通道：** `onDidSaveTextDocument` — 编辑器保存时触发，语义清晰
- **兜底通道：** FileSystemWatcher `onDidChange` + per-file debounce（500ms） — 覆盖 AI 直接写文件的场景
- 服务端 SHA256 去重天然处理双通道重复
- session/model 不传（Cursor 无相关 API）
- 配置走 VS Code settings（`package.json` configuration）
- action：save 传 update，create/delete 事件对应传 create/delete

**输出文档：** docs/cursor_plugin_design.md（已创建）

**待验证项：**
1. `onDidSaveTextDocument` 是否能捕获 Cursor AI 的文件写入
2. `onStartupFinished` activation event 在 Cursor 中的兼容性
3. 多 workspace folder 的 activate 行为

---

## Round 49 — TUI 实时通知：状态文件通信方案

**问题背景：**
- changez 插件核心功能已跑通（hook 稳定触发，版本链完整，delta 连续存储）
- TUI 的 toast 提示只在启动时出现，编辑时没有实时通知
- 原因：Server 插件和 TUI 插件是隔离的不同进程，`tool.execute.after` hook 无法直接触发 TUI toast

**方案对比：**
1. **TUI 直接轮询 changez 服务** — 需要 `/api/stats` 端点，服务端需改动
2. **状态文件 + TUI 轮询** — 零服务端改动，轻量，只需一个 marker 文件
3. **SSE/WebSocket 通知** — 实时推送但复杂度高

**用户选择方案 2。**

**具体设计：**

Server 端（`changez.server.ts`）：
- 在 `tool.execute.after` hook 末尾，snapshot 捕获成功后写 `.opencode/.changez-status.json`
- 内容：`{ updated, total, files: [{ path, action }] }`
- 使用已有的 `client.fs.writeFile` API，约 5 行新增代码

TUI 端（`changez.tui.ts`）：
- 启动时除 health check toast 外，新增 `setInterval` 轮询（2s 间隔）
- 纯字符串比较 `content !== lastContent` 检测变化
- 30s 防 spam：频繁编辑时每 30 秒最多弹 1 次 toast
- 约 25 行新增代码

**关键设计点：**
- 轮询间隔 2s：足够快又不耗资源（只是读一个小 JSON 文件）
- 30s 防 spam：避免频繁编辑时 toast 过多
- `.opencode/` 目录：opencode 本地状态目录，不会被 git 跟踪
- 多窗口竞争：状态文件被覆盖写但内容幂等，不会出问题

**改动范围：** Server 端约 5 行，TUI 端约 25 行，changez 服务端零改动。

**(更新 docs/opencode_plugin_design.md：TUI 架构图和"实时通知方案"章节)**

---

## Round 49 — Claude Code (wps_claude) 接入方案设计

**用户:** 调研 Claude Code / wps_claude 如何接入 changez。

**调研方法:**
1. 找到 wps_claude 安装位置：`~/.local/bin/wps_claude` → `~/.wps-claude-code/dist/cli.js`（11MB 编译产物）
2. 对 cli.js 进行逆向分析，发现完整 hooks 系统
3. 确认 wps_claude 是 Claude Code 逆向恢复版本，支持 hooks 配置

**关键发现 — Claude Code Hooks 系统：**

- **27 种 hook 事件**，包括 PreToolUse、PostToolUse、FileChanged、SessionStart 等
- **4 种 hook 类型：** command（shell）、http（webhook）、prompt（LLM）、agent（完整 agent）
- Hook 输入通过 `$ARGUMENTS` 环境变量（command）或 POST body（http）传递 JSON
- Hook matcher 支持管道分隔：`"Write|Edit"`
- `--bare` 模式禁用所有 hooks

**定稿方案：PostToolUse Hook + Shell 脚本**

- **PostToolUse hook** matcher `"Write|Edit"` → command 类型执行 `changez-report` 脚本
- `$ARGUMENTS` 包含 tool_name, tool_input（含 file_path + content）, tool_response
- changez-report 脚本解析 JSON，提取文件信息，POST 到 changez /api/snapshot
- Edit 工具可能只有 diff 无完整 content，fallback 从文件系统读取
- fire-and-forget：curl --max-time 5 || true，始终 exit 0

**与 OpenCode 插件对比：**

| 维度 | OpenCode | Claude Code |
|------|----------|-------------|
| 触发 | tool.execute.after hook | PostToolUse hook |
| session/model | 有 | 无（hook 不暴露） |
| 阻塞 AI | 否（异步） | 是（hook 阻塞，有 timeout） |
| 配置 | opencode.json | settings.json hooks |

**输出文档：** docs/claude_code_plugin_design.md（已创建）

**待验证项：**
1. PostToolUse 的 $ARGUMENTS 实际字段格式（需实测）
2. Edit 工具的 tool_input 是 diff 还是完整 content
3. Hook 超时后 AI 是否等待还是跳过
4. --bare 模式对 hooks 的影响范围
5. wps_claude 逆向版本 hooks 功能是否完整
