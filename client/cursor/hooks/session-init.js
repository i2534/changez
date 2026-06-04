"use strict";

// session-init.js — sessionStart hook: register project + inject env vars
//
// Runtime: Node.js 18+ (built-in fetch, AbortSignal, process.stdin)
// No external dependencies.

const path = require("node:path");
const { loadConfig, DEFAULTS } = require(path.join(__dirname, "lib", "config.js"));

let unifiedConfig;
try {
  const result = loadConfig(process.cwd(), { source: "cursor" });
  unifiedConfig = result.config;
} catch (e) {
  unifiedConfig = { ...DEFAULTS };
}

async function registerProject(workspaceRoot, cfg) {
  const projectName = workspaceRoot.split("/").pop() || "unknown";
  const headers = { "Content-Type": "application/json" };
  if (cfg.token) {
    headers["Authorization"] = `Bearer ${cfg.token}`;
  }
  for (let i = 0; i <= 2; i++) {
    try {
      const resp = await fetch(`${cfg.url}/api/projects`, {
        method: "POST",
        headers,
        body: JSON.stringify({ rootPath: workspaceRoot, name: projectName }),
        signal: AbortSignal.timeout(5000),
      });
      if (resp.ok || resp.status === 409) return { status: "registered", code: resp.status };
      if (i < 2) await new Promise((r) => setTimeout(r, 500 * (i + 1)));
    } catch (_) {
      if (i < 2) await new Promise((r) => setTimeout(r, 500 * (i + 1)));
    }
  }
  return { status: "failed", attempts: 3 };
}

async function main() {
  let inputText = "";
  for await (const chunk of process.stdin) {
    inputText += chunk;
  }
  const input = JSON.parse(inputText);

  const workspaceRoot = input.workspace_roots?.[0];
  const sessionId = input.session_id;
  const model = input.model;

  let projectResult = { status: "skipped" };
  if (workspaceRoot) {
    projectResult = await registerProject(workspaceRoot, unifiedConfig);
  }

const maskedToken = unifiedConfig.token ? "***" : "";

const output = {
    env: {
      CHANGEZ_URL: unifiedConfig.url,
      CHANGEZ_TOKEN: maskedToken,
      CHANGEZ_SOURCE: unifiedConfig.source,
      CHANGEZ_SESSION_ID: sessionId,
      CHANGEZ_MODEL: model,
    },
    changez: {
      project: projectResult,
      workspaceRoot,
      sessionId,
    },
  };
  process.stdout.write(JSON.stringify(output) + "\n");
}

main().catch((err) => {
  console.error(`[changez] session-init failed: ${err.message}`);
  process.stdout.write(JSON.stringify({
    env: {},
    changez: { error: err.message },
  }) + "\n");
});
