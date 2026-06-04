"use strict";

// report.js — afterFileEdit hook: read file from filesystem → POST /api/snapshot
//
// Runtime: Node.js 18+ (built-in fetch, AbortSignal, process.stdin)
// No external dependencies.

const { readFileSync } = require("node:fs");
const path = require("node:path");

const { loadConfig, shouldSkipFile, DEFAULTS } = require(path.join(__dirname, "lib", "config.js"));

const MAX_RETRIES = 1;
const RETRY_DELAY_MS = 500;

let unifiedConfig, excludes;
try {
  const result = loadConfig(process.cwd(), { source: "cursor" });
  unifiedConfig = result.config;
  excludes = result.excludes;
} catch (e) {
  unifiedConfig = { ...DEFAULTS };
  excludes = [...DEFAULTS.excludes];
}

async function fetchWithRetry(url, options) {
  for (let i = 0; i <= MAX_RETRIES; i++) {
    try {
      return await fetch(url, options);
    } catch (_) {
      if (i < MAX_RETRIES) {
        await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
      }
    }
  }
  return null;
}

async function main() {
  let inputText = "";
  for await (const chunk of process.stdin) {
    inputText += chunk;
  }
  const input = JSON.parse(inputText);

  const filePath = input.file_path;
  const sessionId = input.conversation_id;
  const model = input.model;

  if (!filePath) {
    process.stdout.write(JSON.stringify({ status: "skipped", reason: "no file_path" }) + "\n");
    process.exit(0);
  }

  let content;
  try {
    content = readFileSync(filePath, "utf8");
  } catch (e) {
    process.stdout.write(JSON.stringify({ status: "error", reason: "read_failed", file: filePath, error: e.message }) + "\n");
    process.exit(0);
  }

  if (Buffer.byteLength(content, "utf8") > unifiedConfig.max_file_size) {
    process.stdout.write(JSON.stringify({ status: "skipped", reason: "file_too_large", file: filePath }) + "\n");
    process.exit(0);
  }

  if (shouldSkipFile(filePath, excludes, content)) {
    process.stdout.write(JSON.stringify({ status: "skipped", reason: "excluded", file: filePath }) + "\n");
    process.exit(0);
  }

  const body = {
    source: unifiedConfig.source,
    sessionId,
    model,
    files: [{ path: filePath, content, action: "update" }],
  };

  const headers = { "Content-Type": "application/json" };
  if (unifiedConfig.token) {
    headers["Authorization"] = `Bearer ${unifiedConfig.token}`;
  }

  const resp = await fetchWithRetry(`${unifiedConfig.url}/api/snapshot`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(5000),
  });

  if (resp && resp.ok) {
    let detail = {};
    try { detail = await resp.json(); } catch (_) {}
    process.stdout.write(JSON.stringify({
      status: "ok",
      file: filePath,
      httpStatus: resp.status,
      ...detail,
    }) + "\n");
  } else {
    const errorMsg = resp ? `HTTP ${resp.status}` : "no response";
    console.error(`[changez] snapshot failed: ${errorMsg}`);
    process.stdout.write(JSON.stringify({
      status: "error",
      file: filePath,
      reason: errorMsg,
    }) + "\n");
  }
}

main().catch((e) => {
  console.error(`[changez] report.js error: ${e.message}`);
  process.stdout.write(JSON.stringify({ status: "error", error: e.message }) + "\n");
});
