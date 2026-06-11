// OpenCode Plugin: changez.server — 自动文件变更追踪 (Server 插件)
//
// 自包含单文件插件，仅依赖 Node.js 内置模块。
// 主 session：监听 V2 EventStream（session.next.tool.called + session.next.tool.success）
// 和 V1 tool.execute.after 兜底，拦截文件修改工具（write/edit/multiedit/apply_patch/bash rm），
// 异步上报文件内容到 changez 服务。
// 子 session：通过 event 钩子监听 session.diff 事件，追踪子 agent 的文件编辑。
//
// 配对文件：changez.tui.tsx（可选 TUI 状态展示插件）
//
// 安装方式：
//   安装位置：~/.opencode/changez/changez.server.ts

import * as fs from "node:fs";
import * as path from "node:path";
import * as http from "node:http";
import * as https from "node:https";

const { loadConfig, shouldSkipFile } = require(path.join(__dirname, "lib", "config.js"));

type ChangezConfig = {
  url: string;
  token?: string;
  source?: string;
  project?: { name?: string };
  logLevel?: "debug" | "info" | "warn" | "error";
};

type LogLevel = keyof typeof LOG_LEVELS;

type FileTarget = {
  path: string;
  action: "create" | "update" | "delete";
};

type SnapshotResultItem = {
  path: string;
  status: "captured" | "unchanged" | "error";
  versionId?: number;
  reason?: string;
};

type SnapshotSummary = {
  captured: number;
  unchanged: number;
  errors: number;
};

type SnapshotResponse = {
  results: SnapshotResultItem[];
  summary: SnapshotSummary;
};

type PluginInput = {
  client: {
    app: {
      log: (params: { body: Record<string, unknown> }) => Promise<void>;
    };
  };
  directory: string;
};

const LOG_LEVELS = { error: 0, warn: 1, info: 2, debug: 3 } as const;

const sessionModels = new Map<string, string>();

const sessionContext = new Map<string, {
    parentID?: string;
    directory?: string;
    model?: string;
}>();

function createLogger(
  client: PluginInput["client"],
  cfg: ChangezConfig,
) {
  return (level: LogLevel, message: string, extra?: object) => {
    if (LOG_LEVELS[level] > LOG_LEVELS[cfg.logLevel ?? "info"]) return;
    void client.app.log({
      body: { service: "changez", level, message, ...extra },
    });
  };
}

type Logger = ReturnType<typeof createLogger>;

function httpRequest(
  cfg: ChangezConfig,
  method: string,
  pathStr: string,
  body?: object,
): Promise<{ status: number; json?: object }> {
  const urlObj = new URL(pathStr, cfg.url);
  const isHttps = urlObj.protocol === "https:";
  const mod = isHttps ? https : http;

  return new Promise((resolve, reject) => {
    const payload = body ? JSON.stringify(body) : undefined;

    const options: http.RequestOptions = {
      hostname: urlObj.hostname,
      port: urlObj.port
        ? parseInt(urlObj.port, 10)
        : isHttps ? 443 : 80,
      path: urlObj.pathname + urlObj.search,
      method,
      headers: {
        "Content-Type": "application/json",
        ...(cfg.token ? { Authorization: `Bearer ${cfg.token}` } : {}),
      },
    };

    const req = mod.request(options, (res) => {
      clearTimeout(timer);
      let data = "";
      res.on("data", (chunk: string) => (data += chunk));
      res.on("end", () => {
        let json: object | undefined;
        try {
          json = JSON.parse(data);
        } catch {
          /* ignore non-JSON responses */
        }
        resolve({ status: res.statusCode ?? 0, json });
      });
    });

    const timer = setTimeout(() => {
      req.destroy(new Error("timeout 5s"));
    }, 5000);

    req.on("error", (e) => {
      clearTimeout(timer);
      reject(e);
    });

    if (payload) req.write(payload);
    req.end();
  });
}

/** 从工具 args 中提取单个文件路径 */
function pickFilePath(args: Record<string, unknown>): string | undefined {
  return (
    (args.filePath as string | undefined) ??
    (args.path as string | undefined)
  );
}

/** 从 apply_patch 的 patchText 中解析目标文件路径和 action */
function parsePatchTargets(
  patchText: string,
  log: Logger,
): FileTarget[] {
  if (!patchText) return [];

  const results: FileTarget[] = [];

  const re = /^\*\*\s+(Add|Update|Delete)\s+File:\s+(.+)$/gm;
  let match: RegExpExecArray | null;
  while ((match = re.exec(patchText)) !== null) {
    const actionLabel = match[1].toLowerCase();
    const filePath = match[2].trim();

    let action: FileTarget["action"];
    switch (actionLabel) {
      case "add":
        action = "create";
        break;
      case "delete":
        action = "delete";
        break;
      default:
        action = "update";
        break;
    }

    results.push({ path: filePath, action });
  }

  if (results.length === 0 && patchText.includes("***")) {
    log("debug", "parsePatchTargets: patch contains '***' but no targets matched");
  }

  return results;
}

/** 从 bash 命令中提取 rm 的文件路径 */
function extractRmPaths(command: string): string[] {
  if (!command) return [];

  const results: string[] = [];

  // Split on && first to handle chained commands
  const parts = command.split(/&&/).map(s => s.trim());

  for (const part of parts) {
    const rmCmdRe = /\brm\s+(?:-[rfv]+\s+|--\w+\s+)*(.+?)(?=$|[;&|])/g;
    let cmdMatch: RegExpExecArray | null;
    while ((cmdMatch = rmCmdRe.exec(part)) !== null) {
      const allArgs = cmdMatch[1].trim();
      if (!allArgs) continue;
      for (const filePath of allArgs.split(/\s+/)) {
        if (filePath.includes("*") || filePath.includes("?")) continue;
        if (filePath.startsWith("-")) continue;
        results.push(filePath);
      }
    }
  }

  return results;
}

/** 从 multiedit 的 args 中收集所有文件路径 */
function collectMultieditPaths(
  args: Record<string, unknown>,
): string[] {
  const paths = new Set<string>();

  const edits = args.edits;
  if (Array.isArray(edits)) {
    for (const edit of edits) {
      if (edit && typeof edit === "object") {
        const p = (edit as Record<string, unknown>).path;
        if (typeof p === "string") paths.add(p);
      }
    }
  }

  const changes = args.changes;
  if (Array.isArray(changes)) {
    for (const change of changes) {
      if (change && typeof change === "object") {
        const p = (change as Record<string, unknown>).path;
        if (typeof p === "string") paths.add(p);
      }
    }
  }

  const files = args.files;
  if (Array.isArray(files)) {
    for (const f of files) {
      if (f && typeof f === "object") {
        const p = (f as Record<string, unknown>).path;
        if (typeof p === "string") paths.add(p);
      }
    }
  }

  return [...paths];
}

const createServerPlugin = async (
  { client, directory }: PluginInput,
  options?: Record<string, unknown>,
) => {
  const cfg: ChangezConfig = {
    url: (options?.url as string) ?? "http://127.0.0.1:8760",
    token: (options?.token as string) ?? "",
    source: (options?.source as string) ?? "opencode",
    project: options?.project as ChangezConfig["project"],
    logLevel: (options?.logLevel as ChangezConfig["logLevel"]) ?? "info",
  };

  const projectRoot = path.resolve(directory);
  const projectName = cfg.project?.name ?? path.basename(projectRoot);
  const log = createLogger(client, cfg);

  // 只传非空值，避免空字符串覆盖全局配置 (~/.changez/config.json)
  const pluginConfig = {};
  if (cfg.url) pluginConfig.url = cfg.url;
  if (cfg.token) pluginConfig.token = cfg.token;
  if (cfg.source) pluginConfig.source = cfg.source;
  if (cfg.logLevel) pluginConfig.log_level = cfg.logLevel;
  const { config: unifiedConfig, excludes, projectRoot: detectedRoot } = loadConfig(
    projectRoot,
    pluginConfig,
  );

  log("info", `loaded (${unifiedConfig.url}, project: ${projectName}, excludes: ${excludes.length} patterns)`);

  // Project 注册：成功后停止，失败则每 30 秒无限重试
  let registered = false;
  let registerTimer: ReturnType<typeof setInterval> | null = null;

  const registerProject = async () => {
    try {
      const res = await httpRequest(unifiedConfig, "POST", "/api/projects", {
        rootPath: projectRoot,
        name: projectName,
      });
      if (res.status === 201) {
        log("info", `project registered: ${projectName}`);
        registered = true;
        if (registerTimer) {
          clearInterval(registerTimer);
          registerTimer = null;
        }
      } else if (res.status === 409) {
        log("info", `project already registered: ${projectName}`);
        registered = true;
        if (registerTimer) {
          clearInterval(registerTimer);
          registerTimer = null;
        }
      } else {
        log("warn", `project registration failed: ${res.status}`);
      }
    } catch (e) {
      log("warn", `project registration error: ${String(e)}`);
    }
  };

  await registerProject();
  if (!registered) {
    registerTimer = setInterval(registerProject, 30_000);
    log("debug", `project registration retry started (every 30s)`);
  }

  // === V2 EventStream 追踪：Tool.Called → 缓存参数，Tool.Success → 上报快照 ===
  const pendingToolCalls = new Map<string, {
    tool: string;
    input: Record<string, unknown>;
    sessionID: string;
    timestamp: number;
  }>();
  const processedCalls = new Set<string>();

  const cleanupTimer = setInterval(() => {
    const cutoff = Date.now() - 300_000;
    for (const [key, val] of pendingToolCalls) {
      if (val.timestamp < cutoff) pendingToolCalls.delete(key);
    }
    if (processedCalls.size > 2000) processedCalls.clear();
  }, 60_000);

  /** 从工具 args 中提取文件路径并上报快照（V1 tool.execute.after 和 V2 Tool.Success 共用） */
  async function handleToolExecution(
    rawTool: string,
    args: Record<string, unknown> | undefined,
    sessionID: string,
    source: string,
  ): Promise<void> {
    if (!args) {
      log("debug", `${source}: args is empty for tool=${rawTool}`);
      return;
    }
    try {
      const tool = rawTool.toLowerCase();
      const model = sessionModels.get(sessionID);
      if (!model) {
        log("debug", `no model info for session ${sessionID}`);
      }

      log("debug", `${source} triggered: tool=${rawTool}`, {
        tool: rawTool,
        sessionID,
        source,
      });

      let filePaths: FileTarget[] = [];

      if (tool === "write" || tool === "edit") {
        const fp = pickFilePath(args);
        if (fp) filePaths.push({ path: fp, action: "update" });
      } else if (tool === "multiedit") {
        const paths = collectMultieditPaths(args);
        filePaths = paths.map((p) => ({ path: p, action: "update" }));
      } else if (tool === "apply_patch") {
        const patchText =
          (typeof args?.patchText === "string" ? args.patchText : "") ||
          (typeof args?.patch === "string" ? args.patch : "");
        filePaths = parsePatchTargets(patchText, log);
      } else if (tool === "bash") {
        const cmd =
          (typeof args?.command === "string" ? args.command : "") ||
          (typeof args?.bash_command === "string" ? args.bash_command : "");
        const rmPaths = extractRmPaths(cmd);
        filePaths = rmPaths.map((p) => ({ path: p, action: "delete" }));
      }

      if (filePaths.length === 0) {
        log("debug", `${source}: no file targets for tool=${rawTool}`);
        return;
      }

      log("info", `${source}: ${filePaths.length} file target(s) for tool=${rawTool}`, {
        filePaths: filePaths.map(f => f.path),
      });

      const files: Array<{
        path: string;
        content: string;
        action: string;
      }> = [];

      for (const { path: fp, action } of filePaths) {
        try {
          const absPath = path.isAbsolute(fp)
            ? path.normalize(fp)
            : path.normalize(path.join(projectRoot, fp));

          if (action === "delete") {
            files.push({ path: absPath, content: "", action });
            continue;
          }

          const stat = fs.statSync(absPath);
          if (stat.size > unifiedConfig.max_file_size) {
            log("debug", `skip large file (>${unifiedConfig.max_file_size} bytes): ${absPath}`);
            continue;
          }
          const content = fs.readFileSync(absPath, "utf8");
          if (shouldSkipFile(absPath, excludes, content)) {
            log("debug", `skip excluded file: ${absPath}`);
            continue;
          }
          files.push({ path: absPath, content, action });
        } catch (e) {
          log("debug", `read failed for ${fp}: ${String(e)}`);
        }
      }

      if (files.length === 0) return;

      // Fire-and-forget: 不阻塞工具执行
      void (async () => {
        try {
          const res = await httpRequest(unifiedConfig, "POST", "/api/snapshot", {
            source: unifiedConfig.source,
            sessionId: sessionID,
            model: model ?? "",
            files,
          });

          log("info", `snapshot sent: ${files.length} file(s)`, {
            tool,
            sessionID,
            source,
            httpStatus: res.status,
          });

          if (res.status >= 400) {
            const body = res.json as { error?: { code?: string; message?: string } } | undefined;
            const errorMsg = body?.error?.message ?? JSON.stringify(body);
            if (res.status >= 500) {
              log("warn", `snapshot server error: ${res.status} — ${errorMsg}`);
            } else {
              log("error", `snapshot client error: ${res.status} — ${errorMsg}`);
            }
            return;
          }

          const body = res.json as SnapshotResponse | undefined;
          if (!body || !body.results) return;

          const { results, summary } = body;

          for (const item of results) {
            switch (item.status) {
              case "error":
                log("warn", `snapshot error: ${item.path} — ${item.reason ?? "unknown"}`);
                break;
              case "unchanged":
                log("debug", `unchanged: ${item.path}`);
                break;
              case "captured":
                log("debug", `captured v${item.versionId}: ${item.path}`);
                break;
            }
          }

          log(
            "info",
            `snapshot summary: ${summary.captured} captured, ${summary.unchanged} unchanged, ${summary.errors} errors`,
          );
        } catch (e) {
          log("warn", `snapshot request failed: ${String(e)}`);
        }
      })();
    } catch (e) {
      log("error", `${source} unhandled error: ${String(e)}`, {
        tool: rawTool,
        sessionID,
      });
    }
  }

  return {
    "chat.message": async (
      input: { sessionID: string; model?: { providerID: string; modelID: string } },
      _output: unknown,
    ): Promise<void> => {
      if (input.model) {
        const modelKey = `${input.model.providerID}/${input.model.modelID}`;
        sessionModels.set(input.sessionID, modelKey);
        const ctx = sessionContext.get(input.sessionID);
        if (ctx) ctx.model = modelKey;
      }
    },

    "event": async (input: { event: { type: string; properties: Record<string, unknown> } }): Promise<void> => {
      const { event } = input;

      // V2 EventStream: 工具被调用 → 缓存参数（后续 Tool.Success 配对使用）
      if (event.type === "session.next.tool.called") {
        log("debug", `tool.called: ${event.properties.tool} (callID=${event.properties.callID})`, {
          tool: event.properties.tool,
          callID: event.properties.callID,
          sessionID: event.properties.sessionID,
        });
        pendingToolCalls.set(event.properties.callID as string, {
          tool: event.properties.tool as string,
          input: event.properties.input as Record<string, unknown>,
          sessionID: event.properties.sessionID as string,
          timestamp: Date.now(),
        });
        return;
      }

      // V2 EventStream: 工具执行成功 → 关联缓存，上报快照
      if (event.type === "session.next.tool.success") {
        const callID = event.properties.callID as string;
        if (processedCalls.has(callID)) {
          log("debug", `tool.success: ${callID} already processed (dedup)`);
          return;
        }

        const cached = pendingToolCalls.get(callID);
        if (cached) {
          log("debug", `tool.success: matching called found for callID=${callID}, processing...`);
          processedCalls.add(callID);
          handleToolExecution(cached.tool, cached.input, cached.sessionID, "session.next.tool.success");
          pendingToolCalls.delete(callID);
        } else {
          log("warn", `tool.success: no matching called for callID=${callID} — session.next.tool.called may have been missed`, {
            sessionID: event.properties.sessionID,
            callID,
          });
        }
        return;
      }

      if (event.type === "session.created") {
        const info = event.properties.info as { id?: string; parentID?: string; directory?: string } | undefined;
        if (info?.id) {
          sessionContext.set(info.id, {
            parentID: info.parentID,
            directory: info.directory,
          });
          log("debug", `session.created: ${info.id}${info.parentID ? ` (child of ${info.parentID})` : " (main)"}`, {
            sessionID: info.id,
            parentID: info.parentID,
            directory: info.directory,
          });
        }
      }

      if (event.type === "session.diff") {
        const sessionID = (event.properties.sessionID as string) ?? "";
        const diff = (event.properties.diff as Array<{ file: string; before: string; after: string }> | undefined) ?? [];
        const ctx = sessionContext.get(sessionID);

        if (!ctx) {
          log("warn", `session.diff: no context for session ${sessionID} — session.created may have been missed`, {
            sessionID,
            diffLength: diff.length,
          });
          return;
        }
        if (!ctx.parentID) {
          log("debug", `session.diff: skipping main session ${sessionID} (no parentID)`, {
            sessionID,
          });
          return;
        }

        // 按 file 去重：保留每个文件的最后一次编辑
        const seen = new Map<string, { file: string; before: string; after: string }>();
        for (const fileDiff of diff) {
          seen.set(fileDiff.file, fileDiff);
        }

        log("info", `session.diff: ${seen.size} file(s) from child session ${sessionID}`, {
          sessionID,
          parentID: ctx.parentID,
        });

        const files: Array<{ path: string; content: string; action: string }> = [];

        for (const fileDiff of seen.values()) {
          if (!fileDiff.after) continue;

          try {
            const absPath = path.isAbsolute(fileDiff.file)
              ? path.normalize(fileDiff.file)
              : path.normalize(path.join(ctx.directory ?? detectedRoot, fileDiff.file));

            const stat = fs.statSync(absPath);
            if (stat.size > unifiedConfig.max_file_size) {
              log("debug", `skip large file (>${unifiedConfig.max_file_size} bytes): ${absPath}`);
              continue;
            }
            const content = fs.readFileSync(absPath, "utf8");
            if (shouldSkipFile(absPath, excludes, content)) {
              log("debug", `skip excluded file: ${absPath}`);
              continue;
            }
            files.push({ path: absPath, content, action: fileDiff.before ? "update" : "create" });
          } catch (e) {
            log("debug", `session.diff read failed for ${fileDiff.file}: ${String(e)}`);
          }
        }

        if (files.length === 0) return;

        void (async () => {
          try {
            const res = await httpRequest(unifiedConfig, "POST", "/api/snapshot", {
              source: unifiedConfig.source,
              sessionId: sessionID,
              model: ctx.model ?? "",
              files,
            });

            log("info", `session.diff snapshot sent: ${files.length} file(s) — ${res.status}`, {
              sessionID,
              parentID: ctx.parentID,
              httpStatus: res.status,
            });

            if (res.status >= 400) {
              const body = res.json as { error?: { code?: string; message?: string } } | undefined;
              const errorMsg = body?.error?.message ?? JSON.stringify(body);
              if (res.status >= 500) {
                log("warn", `session.diff snapshot server error: ${res.status} — ${errorMsg}`);
              } else {
                log("error", `session.diff snapshot client error: ${res.status} — ${errorMsg}`);
              }
            }
          } catch (e) {
            log("warn", `session.diff snapshot request failed: ${String(e)}`);
          }
        })();
      }
    },

    "tool.execute.after": async (
      input: {
        tool: string;
        sessionID: string;
        callID: string;
        args: Record<string, unknown>;
      },
      _output: { title: string; output: string; metadata: unknown },
    ): Promise<void> => {
      // V1 路径兜底：V2 EventStream 事件可能已先处理，防重复
      if (processedCalls.has(input.callID)) {
        log("debug", `tool.execute.after: ${input.callID} already processed by V2 (dedup)`);
        return;
      }
      processedCalls.add(input.callID);

      log("info", `tool.execute.after: V1 fallback for tool=${input.tool} (callID=${input.callID})`);
      await handleToolExecution(input.tool, input.args, input.sessionID, "tool.execute.after");
    },
  };
};

export default {
  id: "changez",
  server: createServerPlugin,
};
