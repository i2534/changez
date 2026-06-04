#!/usr/bin/env node
"use strict"

/**
 * changez-hook — Claude Code 统一 hook 脚本
 *
 * 通过 stdin 接收 hook JSON，零外部依赖（仅 Node.js 内置模块）。
 * fire-and-forget，始终 exit 0。
 *
 * 中断重连：进程内快速重试（指数退避），失败后记录日志。
 */

const fs = require("fs")
const path = require("path")

const { loadConfig, shouldSkipFile, DEFAULTS } = require(path.join(__dirname, "lib", "config.js"))

// ── 配置 ───────────────────────────────────────────────────────
const CHANGEZ_LOG_FILE   = process.env.CHANGEZ_LOG_FILE   ?? ""
const MAX_RETRIES        = parseInt(process.env.CHANGEZ_MAX_RETRIES ?? "2", 10)
const RETRY_BASE_DELAY   = parseInt(process.env.CHANGEZ_RETRY_DELAY ?? "500", 10)
const REQUEST_TIMEOUT    = 3000

let unifiedConfig, excludes
try {
  const result = loadConfig(process.cwd(), { source: "claudecode" })
  unifiedConfig = result.config
  excludes = result.excludes
} catch (e) {
  unifiedConfig = { ...DEFAULTS }
  excludes = [...DEFAULTS.excludes]
}

// ── 日志 ───────────────────────────────────────────────────────
function log(level, msg) {
  const line = `[${new Date().toISOString()}] [${level}] ${msg}`
  if (CHANGEZ_LOG_FILE) {
    try { fs.appendFileSync(CHANGEZ_LOG_FILE, line + "\n") } catch {}
  }
  if (level === "error" || level === "warn") {
    console.error(line)
  }
}

// ── 工具函数 ───────────────────────────────────────────────────
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// ── HTTP 请求工具（带重试）─────────────────────────────────────
async function post(endpoint, body, maxRetries) {
  const headers = { "Content-Type": "application/json" }
  if (unifiedConfig.token) headers["Authorization"] = `Bearer ${unifiedConfig.token}`

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const resp = await fetch(`${unifiedConfig.url}${endpoint}`, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(REQUEST_TIMEOUT),
      })

      if (resp.ok || resp.status === 409) {
        if (attempt > 0) {
          log("debug", `retry success after ${attempt} attempt(s): ${endpoint}`)
        }
        return resp
      }

      if (resp.status >= 400 && resp.status < 500 && resp.status !== 429) {
        return resp
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

  return null
}

// ── SessionStart → 项目注册 ────────────────────────────────────
async function handleSessionStart(hook) {
  const rootPath = hook.cwd
  if (!rootPath) {
    log("debug", "SessionStart: no cwd")
    return
  }

  try {
    const resp = await post("/api/projects", {
      rootPath,
      name: path.basename(rootPath),
    }, MAX_RETRIES)

    if (resp && resp.status === 201) {
      log("debug", `project registered: ${rootPath}`)
    } else if (resp && resp.status !== 409) {
      log("warn", `project registration: ${resp ? resp.status : 'timeout'}`)
    }
  } catch (e) {
    log("warn", `project registration error: ${e.message}`)
  }
}

// ── PostToolUse → 文件变更上报 ─────────────────────────────────
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

  const action    = hook.tool_response?.type ?? "update"
  const sessionId = hook.session_id ?? ""

  let content = hook.tool_input?.content
    ?? hook.tool_response?.originalFile

  if (!content && fs.existsSync(filePath)) {
    try {
      const stat = fs.statSync(filePath)
      if (stat.size > unifiedConfig.max_file_size) {
        log("debug", `skip large file (${stat.size} > ${unifiedConfig.max_file_size}): ${filePath}`)
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
    if (size > unifiedConfig.max_file_size) {
      log("debug", `skip large file (${size} > ${unifiedConfig.max_file_size}): ${filePath}`)
      return
    }
  }

  if (shouldSkipFile(filePath, excludes, content)) {
    log("debug", `skip excluded file: ${filePath}`)
    return
  }

  log("debug", `reporting: ${filePath} (${action}, ${content ? Buffer.byteLength(content, "utf8") : 0} bytes)`)

  try {
    const resp = await post("/api/snapshot", {
      source: unifiedConfig.source,
      sessionId,
      files: [{ path: filePath, content: content ?? "", action }],
    }, MAX_RETRIES)

    if (resp && resp.ok) {
      log("debug", `snapshot OK: ${filePath}`)
    } else if (resp && resp.status >= 400 && resp.status < 500) {
      log("error", `snapshot failed: ${resp.status} ${resp.statusText}`)
    } else if (resp) {
      log("warn", `snapshot failed: ${resp.status} ${resp.statusText}`)
    } else {
      log("warn", `snapshot failed: all retries exhausted for ${filePath}`)
    }
  } catch (e) {
    log("warn", `snapshot error: ${e.message}`)
  }
}

// ── 主入口 ─────────────────────────────────────────────────────
async function main() {
  let raw = ""
  for await (const chunk of process.stdin) {
    raw += chunk
  }
  if (!raw) {
    log("debug", "empty stdin")
    return
  }

  let hook
  try {
    hook = JSON.parse(raw)
  } catch (e) {
    log("error", `JSON parse failed: ${e.message}`)
    return
  }

  if (hook.hook_event_name === "SessionStart") {
    await handleSessionStart(hook)
  } else {
    await handlePostToolUse(hook)
  }
}

main().then(() => process.exit(0)).catch((e) => {
  log("error", `unhandled: ${e.message}`)
  process.exit(0)
})
