// report.js — afterFileEdit hook: read file from filesystem → POST /api/snapshot
//
// Runtime: Node.js 18+ (built-in fetch, AbortSignal, process.stdin)
// No external dependencies.

const { readFileSync } = require("node:fs");

const CHANGEZ_URL = process.env.CHANGEZ_URL || "http://127.0.0.1:8760";
const CHANGEZ_TOKEN = process.env.CHANGEZ_TOKEN || "";
const CHANGEZ_SOURCE = process.env.CHANGEZ_SOURCE || "cursor";
const MAX_FILE_SIZE = 10 * 1024 * 1024;
const MAX_RETRIES = 1;
const RETRY_DELAY_MS = 500;

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

  let content;
  try {
    content = readFileSync(filePath, "utf8");
  } catch (e) {
    console.error(`[changez] failed to read ${filePath}: ${e.message}`);
    process.exit(0);
  }

  if (Buffer.byteLength(content, "utf8") > MAX_FILE_SIZE) {
    process.exit(0);
  }

  const body = {
    source: CHANGEZ_SOURCE,
    sessionId,
    model,
    files: [{ path: filePath, content, action: "update" }],
  };

  const headers = { "Content-Type": "application/json" };
  if (CHANGEZ_TOKEN) {
    headers["Authorization"] = `Bearer ${CHANGEZ_TOKEN}`;
  }

  const resp = await fetchWithRetry(`${CHANGEZ_URL}/api/snapshot`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(5000),
  });
  if (!resp || !resp.ok) {
    console.error(`[changez] snapshot failed: ${resp ? resp.status : "no response"}`);
  }
}

main().catch((e) => {
  console.error(`[changez] report.js error: ${e.message}`);
});
