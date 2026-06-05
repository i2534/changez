# AI 集成方案 — 内置可选模块

## 背景

Changez 采集了 AI 编程工具（opencode、claudecode、cursor）的文件变更数据，包括版本内容、diff、source、session_id、model、message 等元数据。这些数据具有 AI 分析价值（变更摘要、会话报告、趋势分析等）。

**核心问题**：AI 分析能力应该放在哪里？

## 三种方案对比

### 方案 A：外部 Agent 编排

AI 分析完全在 changez 外部，由 opencode subagent / 独立 MCP server / CLI 工具实现。

```
外部 Agent ──MCP/REST──▶ changez(纯数据) ──返回──▶ 原始 JSON
     │
     ▼
  LLM API ──返回──▶ AI 增强结果
```

**优点**：changez 零改动，保持零依赖。
**缺点**：每处消费者重复实现（调 API → 拼 prompt → 调 LLM → 缓存），数据不一致，调用频率不可控。

### 方案 B：独立 AI 服务

单独的 AI 服务进程，消费 changez 数据，提供 AI 增强的 API。

```
changez(数据) ──同步──▶ AI Service ──返回──▶ AI 结果
```

**优点**：解耦清晰。
**缺点**：+1 个服务需要部署维护，数据同步增加复杂度。

### 方案 C：内置可选模块（推荐）

AI 能力作为 changez 的可选模块，默认关闭，配置开启后生效。

```
┌──────────────────────────────────────────────────────┐
│                  changez 服务                         │
│                                                      │
│  ┌─────────────┐    原始数据     ┌────────────────┐  │
│  │  数据层      │ ──────────────▶│  AI 模块 (可选)  │  │
│  │  (SQLite)   │                │                │  │
│  └─────────────┘                │  • 调用 LLM     │  │
│                                 │  • 缓存结果     │  │
│                                 │  • 生成摘要     │  │
│                                 └───────┬────────┘  │
│                                         │            │
│              ┌──────────────────────────┼──────────┐ │
│              │        统一 API 层        │          │ │
│              │                          │          │ │
│  ┌───────────┤    GET /api/summary      │          │ │
│  │           │    GET /api/session      │          │ │
│  │           │    GET /api/trends       │          │ │
│  │           └──────────────────────────┘          │ │
│  │                                                 │ │
│  │  MCP: changez_summary  │  REST: /api/summary   │ │
│  │  (agent 用)            │  (Web UI / CLI 用)    │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

**优点**：一处实现，所有消费者共享；AI 结果落库，不重复调 LLM；默认关闭=零依赖。
**缺点**：增加一个可选模块，需要维护 provider 接口。

## 推荐方案：C — 内置可选模块

### 设计原则

1. **默认关闭**：`ai.enabled: false`，不配置时 changez 行为和之前完全一致
2. **接口抽象**：`Provider` 接口，编译期不依赖任何 LLM SDK
3. **结果落库**：AI 摘要写入数据库，一次计算，多处消费
4. **统一 API**：MCP + REST 双通道，agent 和 Web UI 都能用
5. **按需触发**：新快照异步生成 + 手动刷新，不强制批量计算

### 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                     changez 服务                             │
│                                                             │
│  Hook 脚本 ──POST──▶ /api/snapshot                          │
│                          │                                  │
│                          ▼                                  │
│                   ┌──────────┐                              │
│                   │ Handler  │                              │
│                   │ (快照入库) │                              │
│                   └────┬─────┘                              │
│                        │                                    │
│                        ▼                                    │
│                   ┌──────────┐     异步触发     ┌──────────┐│
│                   │  SQLite  │ ──────────────▶ │  AI      ││
│                   │ (versions)│                │  Worker  ││
│                   └────┬─────┘                └────┬─────┘│
│                        │                           │       │
│                        │         写入 ai_summaries  ▼       │
│                        │                    ┌──────────┐   │
│                        │                    │  SQLite  │   │
│                        ▼                    │(ai_summaries)││
│                   ┌──────────┐              └──────────┘   │
│                   │  API 层  │                              │
│                   │          │                              │
│  MCP ◀────────────┤ /summary │                              │
│  REST ◀───────────┤ /session │                              │
│  Web UI ◀─────────┤ /trends  │                              │
│                   └──────────┘                              │
└─────────────────────────────────────────────────────────────┘
```

### 配置

```yaml
# config.yaml
ai:
  enabled: false                    # 默认关闭
  provider: "openai"                # OpenAI 兼容接口
  base_url: "${LLM_BASE_URL}"       # 支持环境变量（OpenAI/llama-server/Ollama）
  api_key: "${LLM_API_KEY}"         # 本地服务可留空
  model: "gpt-4o-mini"              # 推荐小模型（摘要任务）
  timeout: 30s                      # LLM 调用超时
  triggers:
    on_snapshot: true               # 新快照自动触发
    batch_size: 5                   # 批量处理大小
    max_retries: 3                  # 失败重试次数
```

### 数据库扩展

```sql
-- 独立 ai_summaries 表（1:1 关联 versions，避免 ALTER 现有表）
CREATE TABLE ai_summaries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id    INTEGER NOT NULL UNIQUE,
    summary       TEXT,
    model         TEXT,
    status        TEXT NOT NULL DEFAULT 'pending',  -- pending/completed/failed
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE
);

CREATE INDEX idx_ai_summaries_status ON ai_summaries(status);
CREATE INDEX idx_ai_summaries_version ON ai_summaries(version_id);
```

### Provider 接口

```go
// internal/ai/provider.go
package ai

// OpenAI-compatible provider（支持 OpenAI、llama-server、Ollama --api 等）
type Provider interface {
    // Summarize 生成单个 diff 变更摘要（Phase 1）
    // Phase 2 可新增 SummarizeBatch 支持跨文件批量摘要
    Summarize(ctx context.Context, diff string, contextInfo SummaryContext) (string, error)

    // AnalyzeSession 生成会话级变更报告（包含跨文件上下文）
    AnalyzeSession(ctx context.Context, changes []SessionChange) (string, error)

    // AnalyzeTrends 生成趋势分析
    AnalyzeTrends(ctx context.Context, stats TrendStats) (string, error)
}

// OpenAILike 兼容 OpenAI Chat Completions API
type OpenAILike struct {
    baseURL   string        // "https://api.openai.com/v1" 或 "http://localhost:8080/v1"
    apiKey    string        // 本地服务可留空
    model     string        // "gpt-4o-mini" 或 "llama-3.3-70b"
    timeout   time.Duration
    client    *http.Client
}

type SummaryContext struct {
    FilePath  string
    Action    string // create/update/delete
    Source    string // opencode/claudecode/cursor
    Model     string
    Message   string
    Timestamp string
}

type SessionChange struct {
    FilePath  string
    Action    string
    Diff      string
    Model     string
    Message   string
    Timestamp string
}

type TrendStats struct {
    Period     string
    TotalFiles int
    TotalChanges int
    SourceBreakdown map[string]int
    TopFiles      []FileChangeCount
}

type FileChangeCount struct {
    FilePath string
    Count    int
}
```

### AI Worker

```go
// internal/ai/worker.go
type Worker struct {
    provider Provider
    db       *db.DB
    cfg      *config.AICfg
}

// Run 后台运行，消费待处理的版本记录
func (w *Worker) Run(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.processPending(ctx)
        }
    }
}

// processPending 查询 ai_summaries.status='pending' 的记录，批量生成摘要
func (w *Worker) processPending(ctx context.Context) {
    // 1. 查询 pending 记录（按 created_at 排序，取 batch_size 条）
    // 2. 获取 diff（通过 restore + diff）
    // 3. 调用 provider.Summarize()
    // 4. 写入 ai_summaries.summary
    // 5. 更新 ai_summaries.status = 'completed'
}

// Worker 采用轮询模式（10s ticker），适合个人低频变更场景。
// 团队高频场景可改为 channel 事件驱动：handler 在 snapshot 写入后
// 通过 channel 通知 worker，无需轮询。
```

### API 端点

#### REST API

> 遵循现有约定：使用查询参数而非路径参数

```
GET /api/files/summary?project=xxx&path=xxx&since=xxx&until=xxx
GET /api/files/session?project=xxx&sessionId=xxx
GET /api/files/trends?project=xxx&since=xxx&until=xxx
POST /api/files/summary/refresh?project=xxx&path=xxx&version=xxx  # 手动刷新
```

#### MCP 工具

```
changez_summary     — 文件变更 AI 摘要
changez_session     — 会话级变更报告
changez_trends      — 项目趋势分析
```

### Web UI 集成

```
现有页面: /files/{project} → 文件列表 + 版本时间线
新增功能:
├── 版本时间线: 每个版本显示 AI 摘要（折叠/展开）
├── Dashboard: AI 洞察卡片（最近变更趋势、活跃文件、AI 工具使用分布）
└── 手动刷新: "🔄 重新生成摘要" 按钮
```

### 实施步骤

```
Step 1: 基础架构 (2天)
├── internal/ai/provider.go (接口定义)
├── internal/ai/openai_like.go (OpenAI 兼容实现)
├── internal/config/ai.go (配置解析)
└── DB migration (ai_summaries 表)

Step 2: AI Worker (2天)
├── internal/ai/worker.go (后台异步生成)
├── 集成到 main.go (条件启动)
└── 单元测试

Step 3: API 端点 (2天)
├── REST: /api/files/summary, /api/files/session, /api/files/trends
├── MCP: changez_summary, changez_session, changez_trends
└── 集成测试

Step 4: Web UI 集成 (2-3天)
├── API client 扩展
├── AISummary 组件
├── Dashboard AI 洞察
└── 手动刷新功能
```

### 风险与缓解

| 风险 | 缓解措施 |
|---|---|
| LLM API 故障 | 异步处理 + 重试 + 降级（返回空摘要） |
| 调用频率过高 | 小模型 + 缓存 + 用户可控开关 + 限流 |
| 隐私泄露 | 支持本地 Ollama + 敏感文件跳过 |
| 性能影响 | 异步 worker + 批量处理 + 限流 |

### 升级路径

```
Phase 1: 基础 AI 摘要
├── Provider 接口 + OpenAI 实现
├── AI Worker (异步生成摘要)
├── REST API + MCP 工具
└── Web UI 基础集成

Phase 2: 高级分析
├── 会话级报告
├── 趋势分析
├── Anthropic/Ollama provider
├── 批量摘要 (SummarizeBatch)
└── Web UI Dashboard

Phase 3: 深度集成
├── 代码质量趋势
├── AI 辅助代码审查
├── 语义搜索变更历史
└── 模型效果对比
```

---

## 总结

**内置可选模块**是最优方案：
- ✅ 默认关闭 = 保持零依赖承诺
- ✅ 一处实现，所有消费者共享（agent、Web UI、CLI）
- ✅ AI 结果落库 = 不重复调 LLM，结果复用
- ✅ 统一 API = MCP + REST，协议无关
- ✅ 按需触发 = 用户控制调用
- ✅ 升级路径清晰 = 从基础摘要到高级分析
