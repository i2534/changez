// session-init.js — sessionStart hook: register project + inject env vars
//
// Runtime: Node.js 18+ (built-in fetch, AbortSignal, process.stdin)
// No external dependencies.

const CHANGEZ_URL = process.env.CHANGEZ_URL || "http://127.0.0.1:8760";
const CHANGEZ_TOKEN = process.env.CHANGEZ_TOKEN || "";
const CHANGEZ_SOURCE = process.env.CHANGEZ_SOURCE || "cursor";

async function registerProject(workspaceRoot) {
  const projectName = workspaceRoot.split("/").pop() || "unknown";
  const headers = { "Content-Type": "application/json" };
  if (CHANGEZ_TOKEN) {
    headers["Authorization"] = `Bearer ${CHANGEZ_TOKEN}`;
  }
  for (let i = 0; i <= 2; i++) {
    try {
      const resp = await fetch(`${CHANGEZ_URL}/api/projects`, {
        method: "POST",
        headers,
        body: JSON.stringify({ rootPath: workspaceRoot, name: projectName }),
        signal: AbortSignal.timeout(5000),
      });
      if (resp.ok || resp.status === 409) return;
    } catch (_) {}
    if (i < 2) await new Promise((r) => setTimeout(r, 500 * (i + 1)));
  }
  console.error(`[changez] project registration exhausted retries for ${workspaceRoot}`);
}

async function main() {
  let inputText = "";
  for await (const chunk of process.stdin) {
    inputText += chunk;
  }
  const input = JSON.parse(inputText);

  const workspaceRoot = input.workspace_roots[0];
  const sessionId = input.session_id;
  const model = input.model;

  if (workspaceRoot) {
    await registerProject(workspaceRoot);
  }

  const output = {
    env: {
      CHANGEZ_URL,
      CHANGEZ_TOKEN,
      CHANGEZ_SOURCE,
      CHANGEZ_SESSION_ID: sessionId,
      CHANGEZ_MODEL: model,
    },
  };
  process.stdout.write(JSON.stringify(output) + "\n");
}

main().catch((err) => {
  console.error("[changez] session-init failed", err.message);
  process.stdout.write('{"env": {}}\n');
});
