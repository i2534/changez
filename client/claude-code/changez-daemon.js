#!/usr/bin/env node
"use strict"

/**
 * changez-daemon — Claude Code Hook 持久化服务
 *
 * 接收 Claude Code 的 HTTP hook 请求，异步处理文件变更上报。
 * 零外部依赖（仅 Node.js 内置模块）。
 *
 * 启动方式：
 *   node changez-daemon.js
 *   CHANGEZ_TOKEN=xxx node changez-daemon.js
 *   CHANGEZ_PORT=8761 node changez-daemon.js
 */

const http = require("http")
const fs = require("fs")
const path = require("path")

// ── 配置 ───────────────────────────────────────────────────────
const CHANGEZ_URL        = process.env.CHANGEZ_URL        ?? "http://127.0.0.1:8760"
const CHANGEZ_TOKEN      = process.env.CHANGEZ_TOKEN      ?? ""
const CHANGEZ_SOURCE     = process.env.CHANGEZ_SOURCE     ?? "claude-code"
const CHANGEZ_LOG_FILE   = process.env.CHANGEZ_LOG_FILE   ?? ""
const DAEMON_PORT        = parseInt(process.env.CHANGEZ_PORT ?? "8761", 10)
const MAX_FILE_SIZE      = parseInt(process.env.CHANGEZ_MAX_FILE_SIZE ?? "10485760", 10)
const MAX_RETRIES        = parseInt(process.env.CHANGEZ_MAX_RETRIES ?? "2", 10)
const RETRY_BASE_DELAY   = parseInt(process.env.CHANGEZ_RETRY_DELAY ?? "500", 10)
const REQUEST_TIMEOUT    = 3000

// ── 统计 ───────────────────────────────────────────────────────
const stats = {
  hooksReceived: 0,
  hooksProcessed: 0,
  hooksFailed: 0,
  lastHookAt: null,
}

// ── 日志 ───────────────────────────────────────────────────────
function log(level, msg) {
  const line = `[${new Date().toISOString()}] [${level}] ${msg}`
  if (CHANGEZ_LOG_FILE) {
    try { fs.appendFileSync(CHANGEZ_LOG_FILE, line + "\n") } catch {}
  }
  if (level === "error" || level === "warn") {
    console.error(line)
  } else {
    console.log(line)
  }
}

// ── 工具函数 ───────────────────────────────────────────────────
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// ── HTTP 请求工具（带重试）─────────────────────────────────────
async function postToChangez(endpoint, body, maxRetries) {
  const url = new URL(endpoint, CHANGEZ_URL)
  const isHttps = url.protocol === "https:"

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const req = await makeRequest(url, body, isHttps)
      const resp = await req

      if (resp.status >= 200 && resp.status < 300 || resp.status === 409) {
        if (attempt > 0) {
          log("debug", `retry success after ${attempt} attempt(s): ${endpoint}`)
        }
        return { ok: true, status: resp.status, body: resp.body }
      }

      if (resp.status >= 400 && resp.status < 500 && resp.status !== 429) {
        return { ok: false, status: resp.status, body: resp.body }
      }

      if (attempt < maxRetries) {
        const delay = RETRY_BASE_DELAY * Math.pow(2, attempt)
        log("warn", `retry ${attempt + 1}/${maxRetries} after ${delay}ms: ${resp.status} ${endpoint}`)
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

  return { ok: false, status: null, body: null }
}

function makeRequest(url, body, isHttps) {
  return new Promise((resolve, reject) => {
    const lib = isHttps ? require("https") : require("http")
    const payload = JSON.stringify(body)

    const headers = { "Content-Type": "application/json" }
    if (CHANGEZ_TOKEN) headers["Authorization"] = `Bearer ${CHANGEZ_TOKEN}`

    const req = lib.request({
      hostname: url.hostname,
      port: url.port || (isHttps ? 443 : 80),
      path: url.pathname + url.search,
      method: "POST",
      headers,
      timeout: REQUEST_TIMEOUT,
    }, (res) => {
      let data = ""
      res.on("data", (chunk) => (data += chunk))
      res.on("end", () => resolve({ status: res.statusCode, body: data }))
    })

    req.on("timeout", () => {
      req.destroy(new Error("timeout"))
    })

    req.on("error", (e) => reject(e))

    req.write(payload)
    req.end()
  })
}

// ── Hook 处理 ──────────────────────────────────────────────────
async function handleSessionStart(hook) {
  const rootPath = hook.cwd
  if (!rootPath) {
    log("debug", "SessionStart: no cwd")
    return
  }

  log("debug", `SessionStart: registering project ${rootPath}`)
  const result = await postToChangez("/api/projects", {
    rootPath,
    name: path.basename(rootPath),
  }, MAX_RETRIES)

  if (result.ok && result.status === 201) {
    log("debug", `project registered: ${rootPath}`)
  } else if (result.ok && result.status === 409) {
    log("debug", `project already registered: ${rootPath}`)
  } else {
    log("warn", `project registration failed: ${result.status}`)
  }
}

async function handlePostToolUse(hook) {
  const toolName = hook.tool_name
  if (toolName !== "Write" && toolName !== "Edit") {
    log("debug", `skip tool: ${toolName}`)
    return
  }

  const filePath = hook.tool_input?.file_path
  if (!filePath) {
    log("debug", "no file_path in tool_input")
    return
  }

  const action = hook.tool_response?.type ?? "update"
  const sessionId = hook.session_id ?? ""

  // 获取文件内容
  let content = hook.tool_input?.content ?? hook.tool_response?.originalFile

  // 兜底：从文件系统读取
  if (!content && fs.existsSync(filePath)) {
    try {
      const stat = fs.statSync(filePath)
      if (stat.size > MAX_FILE_SIZE) {
        log("debug", `skip large file (${stat.size} > ${MAX_FILE_SIZE}): ${filePath}`)
        return
      }
      content = fs.readFileSync(filePath, "utf8")
    } catch (e) {
      log("error", `read file failed: ${filePath}: ${e.message}`)
      return
    }
  }

  if (toolName === "Write" && content) {
    const size = Buffer.byteLength(content, "utf8")
    if (size > MAX_FILE_SIZE) {
      log("debug", `skip large file (${size} > ${MAX_FILE_SIZE}): ${filePath}`)
      return
    }
  }

  log("debug", `reporting: ${filePath} (${action}, ${content ? Buffer.byteLength(content, "utf8") : 0} bytes)`)

  const result = await postToChangez("/api/snapshot", {
    source: CHANGEZ_SOURCE,
    sessionId,
    files: [{ path: filePath, content: content ?? "", action }],
  }, MAX_RETRIES)

  if (result.ok) {
    log("debug", `snapshot OK: ${filePath}`)
    stats.hooksProcessed++
  } else {
    log("warn", `snapshot failed: ${result.status} for ${filePath}`)
    stats.hooksFailed++
  }
}

async function processHook(hook) {
  try {
    if (hook.hook_event_name === "SessionStart") {
      await handleSessionStart(hook)
    } else {
      await handlePostToolUse(hook)
    }
  } catch (e) {
    log("error", `hook processing error: ${e.message}`)
    stats.hooksFailed++
  }
}

// ── HTTP 服务器 ────────────────────────────────────────────────
const server = http.createServer(async (req, res) => {
  // CORS preflight
  if (req.method === "OPTIONS") {
    res.writeHead(204, { "Access-Control-Allow-Origin": "*" })
    res.end()
    return
  }

  // POST /hook
  if (req.method === "POST" && req.url === "/hook") {
    let body = ""
    req.on("data", (chunk) => (body += chunk))
    req.on("end", async () => {
      stats.hooksReceived++
      stats.lastHookAt = new Date().toISOString()

      // 立即响应 202
      res.writeHead(202, { "Content-Type": "application/json" })
      res.end(JSON.stringify({ accepted: true }))

      // 异步处理
      try {
        const hook = JSON.parse(body)
        await processHook(hook)
      } catch (e) {
        log("error", `hook parse error: ${e.message}`)
      }
    })
    return
  }

  // GET /health
  if (req.method === "GET" && req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" })
    res.end(JSON.stringify({
      status: "ok",
      port: DAEMON_PORT,
      changezUrl: CHANGEZ_URL,
    }))
    return
  }

  // GET /stats
  if (req.method === "GET" && req.url === "/stats") {
    res.writeHead(200, { "Content-Type": "application/json" })
    res.end(JSON.stringify(stats))
    return
  }

  // 404
  res.writeHead(404, { "Content-Type": "application/json" })
  res.end(JSON.stringify({ error: "not found" }))
})

// ── 启动 ───────────────────────────────────────────────────────
server.listen(DAEMON_PORT, "127.0.0.1", () => {
  log("info", `changez-daemon started on port ${DAEMON_PORT}`)
  log("info", `changez URL: ${CHANGEZ_URL}`)
  log("info", `token: ${CHANGEZ_TOKEN ? "configured" : "not set"}`)
})

// ── 优雅关闭 ───────────────────────────────────────────────────
process.on("SIGINT", () => {
  log("info", "shutting down...")
  server.close(() => {
    log("info", "daemon stopped")
    process.exit(0)
  })
})

process.on("SIGTERM", () => {
  log("info", "shutting down...")
  server.close(() => {
    log("info", "daemon stopped")
    process.exit(0)
  })
})

process.on("error", (e) => {
  log("error", `unhandled error: ${e.message}`)
})
