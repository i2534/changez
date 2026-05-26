# Claude Code Hook 中断重连设计

> 创建日期：2026-05-20
> 状态：讨论中

---

## 问题

当前 `changez-hook.js` 是 fire-and-forget 模式：fetch 失败后记录日志即退出，数据丢失。

**影响场景：**
- changez 服务短暂重启
- 网络抖动
- 服务负载高超时

---

## 约束

Claude Code hook 与 OpenCode 插件的根本差异：

| 维度 | OpenCode Server 插件 | Claude Code Hook 脚本 |
|------|---------------------|----------------------|
| 生命周期 | 长驻进程（插件加载后一直运行） | 短命进程（每次 hook 触发新建进程） |
| 状态保持 | 内存变量（Map, timer） | 无（进程退出后丢失） |
| 重试能力 | setInterval 无限重试 | 需在单次进程内完成 |
| 阻塞限制 | 异步 hook，不阻塞 AI | command hook 阻塞 AI（有 timeout） |

**设计目标：** 在单次进程生命周期内完成重试，不延长 hook 阻塞时间。

---

## 方案对比

### 方案 A：进程内重试（内存重试）

在 `post()` 函数内增加重试逻辑，失败后快速重试 2-3 次。

```javascript
async function post(endpoint, body, maxRetries = 2) {
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const resp = await fetch(url, { body, signal: AbortSignal.timeout(3000) })
      if (resp.ok || resp.status === 409) return resp
      // 5xx 可重试，4xx 不可重试
      if (resp.status >= 500 && attempt < maxRetries) {
        log("warn", `retry ${attempt+1}: ${resp.status}`)
        await sleep(500 * (attempt + 1))
      } else {
        return resp
      }
    } catch (e) {
      if (attempt < maxRetries) {
        log("warn", `retry ${attempt+1}: ${e.message}`)
        await sleep(500 * (attempt + 1))
      } else {
        throw e
      }
    }
  }
}
```

**优点：**
- 实现简单，改动小
- 大多数短暂中断可恢复
- 总阻塞时间可控（3 次 × 3s 超时 + 退避 ≈ 6-8s）

**缺点：**
- 进程退出后未成功的数据丢失
- 重试延长 hook 阻塞时间（可能触发 Claude Code 的 5s timeout）

**适用场景：** 服务短暂不可用（<10s）

---

### 方案 B：本地持久化队列

失败后将 snapshot 请求写入本地队列文件，下次 hook 触发时先尝试发送队列中的请求。

```
~/.changez/queue/
  ├── 20260520-081748-001.json
  ├── 20260520-081802-002.json
  └── ...
```

**流程：**
1. hook 启动 → 读取队列文件
2. 尝试发送队列中的请求（每个最多重试 1 次，超时 2s）
3. 发送当前请求
4. 失败则追加到队列文件

**优点：**
- 跨进程持久化，服务长时间不可用也能恢复
- 当前请求失败不影响后续请求

**缺点：**
- 每次 hook 启动增加队列处理开销
- 队列文件可能积累（需清理策略）
- 阻塞时间可能延长（队列累积时）

**适用场景：** 服务频繁重启或网络不稳定

---

### 方案 C：后台守护进程

独立的 Node.js 守护进程，负责接收 hook 请求并异步发送。

```
changez-hook-daemon (后台常驻)
  ↑ stdin/named pipe
  │
changez-hook.js (hook 入口)
  └─ 写入请求到 daemon → 立即 exit 0
```

**优点：**
- hook 零阻塞（写入 pipe 即退出）
- daemon 可无限重试
- 支持批量合并

**缺点：**
- 需要管理守护进程生命周期
- 架构复杂度增加
- daemon 崩溃时退化为方案 A

**适用场景：** 高可靠性要求

---

## 推荐方案：A + B 组合

**进程内快速重试 + 本地队列兜底。**

### 设计

```javascript
const QUEUE_DIR = path.join(os.homedir(), ".changez", "queue")
const MAX_RETRIES = 2
const RETRY_BASE_DELAY = 500  // ms
const QUEUE_SEND_TIMEOUT = 2000  // ms（队列请求快速失败）
const CURRENT_SEND_TIMEOUT = 5000  // ms（当前请求正常超时）

async function main() {
  // 1. 处理队列中的旧请求
  await drainQueue()

  // 2. 处理当前请求
  const hook = parseStdin()
  const result = await sendWithRetry(hook, CURRENT_SEND_TIMEOUT, MAX_RETRIES)

  // 3. 失败则入队
  if (!result.success) {
    enqueue(hook)
  }
}

async function drainQueue() {
  const files = listQueueFiles()
  const successes = []
  const failures = []

  for (const file of files.slice(0, 5)) {  // 每次最多处理 5 个
    const hook = readQueueFile(file)
    const result = await sendWithRetry(hook, QUEUE_SEND_TIMEOUT, 1)
    if (result.success) {
      successes.push(file)
    } else {
      failures.push(file)
    }
  }

  // 删除成功的队列文件
  for (const file of successes) {
    unlink(file)
  }

  // 失败的文件保留（下次继续尝试）
  // 超过 24h 的队列文件删除（避免无限积累）
  for (const file of failures) {
    if (age(file) > 24 * 3600 * 1000) {
      unlink(file)
      log("warn", `queue expired: ${file}`)
    }
  }
}

async function sendWithRetry(hook, timeout, maxRetries) {
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const resp = await post(hook.endpoint, hook.body, {
        signal: AbortSignal.timeout(timeout)
      })

      if (resp.ok || resp.status === 409) {
        log("debug", `OK after ${attempt} retry(ies)`)
        return { success: true }
      }

      // 4xx 不可重试（除 429）
      if (resp.status >= 400 && resp.status < 500 && resp.status !== 429) {
        log("error", `non-retryable: ${resp.status}`)
        return { success: false }
      }

      // 5xx / 429 可重试
      if (attempt < maxRetries) {
        const delay = RETRY_BASE_DELAY * Math.pow(2, attempt)
        log("warn", `retry ${attempt + 1}/${maxRetries} after ${delay}ms`)
        await sleep(delay)
      }
    } catch (e) {
      if (attempt < maxRetries) {
        const delay = RETRY_BASE_DELAY * Math.pow(2, attempt)
        log("warn", `retry ${attempt + 1}/${maxRetries} after ${delay}ms: ${e.message}`)
        await sleep(delay)
      }
    }
  }

  return { success: false }
}

function enqueue(hook) {
  mkdirp(QUEUE_DIR)
  const ts = Date.now()
  const file = path.join(QUEUE_DIR, `${ts}-${Math.random().toString(36).slice(2, 6)}.json`)
  writeFileSync(file, JSON.stringify({
    ts,
    hook,
  }))
  log("debug", `enqueued: ${file}`)
}
```

### 时序

```
正常情况（无重试）:
  hook 启动 → drainQueue(空) → 发送当前 → OK → exit 0
  耗时: ~50ms

服务短暂不可用（<3s）:
  hook 启动 → drainQueue(空) → 发送失败 → retry(500ms) → retry(1000ms) → OK → exit 0
  耗时: ~2-3s

服务不可用（>8s）:
  hook 启动 → drainQueue(空) → 发送失败 → retry × 2 → 失败 → 入队 → exit 0
  耗时: ~2s（不会触发 Claude Code timeout）

下次 hook 触发（服务已恢复）:
  hook 启动 → drainQueue(1个旧请求) → 发送成功 → 删除队列文件 → 发送当前 → OK → exit 0
  耗时: ~2s（队列请求）+ ~50ms（当前请求）
```

### 配置项

```javascript
const CHANGEZ_MAX_RETRIES = parseInt(process.env.CHANGEZ_MAX_RETRIES ?? "2", 10)
const CHANGEZ_RETRY_DELAY = parseInt(process.env.CHANGEZ_RETRY_DELAY ?? "500", 10)
const CHANGEZ_QUEUE_ENABLED = process.env.CHANGEZ_QUEUE_ENABLED !== "false"
const CHANGEZ_QUEUE_DIR = process.env.CHANGEZ_QUEUE_DIR ?? path.join(os.homedir(), ".changez", "queue")
const CHANGEZ_QUEUE_TTL = parseInt(process.env.CHANGEZ_QUEUE_TTL ?? "86400", 10)  // 秒
```

### 队列管理

| 操作 | 规则 |
|------|------|
| 入队 | 当前请求重试耗尽后写入队列文件 |
| 出队 | 下次 hook 启动时处理（最多 5 个） |
| 清理 | 超过 TTL（默认 24h）的队列文件删除 |
| 并发 | 多个 hook 进程同时运行，队列文件用时间戳+随机后缀避免冲突 |

### 与 OpenCode 对比

| 维度 | OpenCode Server 插件 | Claude Code Hook (A+B) |
|------|---------------------|----------------------|
| Project 注册重试 | setInterval 30s 无限重试 | 进程内重试 2 次 + 入队 |
| Snapshot 重试 | 无（fire-and-forget） | 进程内重试 2 次 + 入队 |
| 持久化 | 无（长驻进程） | 本地队列文件 |
| 阻塞 AI | 否（异步 hook） | 是（重试延长阻塞，但有限制） |

---

## 实现范围

### Phase 1 ✅ 已完成（2026-05-20）
- [x] `post()` — 进程内重试（指数退避 500ms/1000ms）
- [x] 可重试/不可重试状态码区分（5xx/429 可重试，4xx 不可重试）
- [x] 超时时间调整（单次 3s，留出重试空间）
- [x] 环境变量：`CHANGEZ_MAX_RETRIES`、`CHANGEZ_RETRY_DELAY`
- [x] 自动化测试脚本 `test-hook.sh`（10/10 通过）

### Phase 2（可选，未启动）
- [ ] 本地队列文件（入队/出队/清理）
- [ ] `drainQueue` — hook 启动时处理旧请求
- [ ] 队列 TTL 配置

### HTTP Daemon 方案（已搁置）
- `changez-daemon.js` 已保留备用，详见 `docs/claude_hook_http_design.md`
- DSV4 review 结论：command hook 方案已足够，暂不需要 daemon

---

## 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| 重试延长 hook 阻塞 | 可能触发 Claude Code 5s timeout | 总重试时间控制在 2s 内（500ms + 1000ms） |
| 队列文件积累 | 磁盘空间 | TTL 24h 自动清理 |
| 多进程队列竞争 | 文件冲突 | 时间戳+随机后缀，处理时加文件锁 |
| 队列请求过时 | 发送旧版本内容 | TTL 限制 + 服务端 SHA256 去重 |

---

## 设计决策

| # | 问题 | 结论 |
|---|------|------|
| 1 | 重试次数 | 2 次（共 3 次尝试），总阻塞 <2s |
| 2 | 退避策略 | 指数退避（500ms, 1000ms） |
| 3 | 可重试状态码 | 5xx, 429, 网络错误；4xx 不可重试 |
| 4 | 队列 TTL | 24h（避免无限积累） |
| 5 | 队列处理数量 | 每次 hook 最多处理 5 个旧请求 |
| 6 | Phase 1 是否包含队列 | 不包含，先实现进程内重试 |
