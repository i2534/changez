// @ts-nocheck
/** @jsxImportSource @opentui/solid */
// OpenCode TUI Plugin: changez — 状态通知 + 侧边栏常驻状态
//
// P0: 通过 toast 通知用户加载结果
// P1: 通过 sidebar_content slot 常驻显示 snapshot 状态
//
// 配对文件：changez.server.ts（核心 Server 插件）
//
// 安装方式：
//   全局：~/.config/opencode/tui-plugins/changez.tui.tsx
//   项目级：{project}/.opencode/tui-plugins/changez.tui.tsx
//
// 配置（tui.json）：
//   {
//     "plugin": [
//       ["file:///path/to/changez.tui.tsx", {
//         "url": "http://127.0.0.1:8760",
//         "token": "xxx"
//       }]
//     ]
//   }

import * as http from "node:http";
import * as https from "node:https";
import * as path from "node:path";
import { createSignal, For, Show } from "solid-js";

import type { TuiPlugin, TuiPluginApi, TuiPluginMeta, TuiPluginModule, TuiSlotPlugin, TuiSlotContext } from "@opencode-ai/plugin/tui";

type TuiConfig = {
  url: string;
  token?: string;
};

type SnapshotData = {
  updatedAt: string;
  captured: number;
  unchanged: number;
  files: Array<{ path: string; versionId: number }>;
};

type TrackedFile = {
  project: string;
  path: string;
  latestVersionId: number | null;
  createdAt: string;
};

type PluginOptions = Record<string, unknown> | undefined;

function httpGet(urlStr: string, token?: string, timeoutMs = 5000): Promise<{ status: number; body?: string }> {
  const urlObj = new URL(urlStr);
  const isHttps = urlObj.protocol === "https:";
  const mod = isHttps ? https : http;

  return new Promise((resolve, reject) => {
    const options: http.RequestOptions = {
      hostname: urlObj.hostname,
      port: urlObj.port ? parseInt(urlObj.port, 10) : isHttps ? 443 : 80,
      path: urlObj.pathname + urlObj.search,
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
    };

    const req = mod.request(options, (res) => {
      clearTimeout(timer);
      let data = "";
      res.on("data", (chunk: string) => (data += chunk));
      res.on("end", () => {
        resolve({ status: res.statusCode ?? 0, body: data || undefined });
      });
    });

    const timer = setTimeout(() => {
      req.destroy(new Error(`timeout ${timeoutMs}ms`));
    }, timeoutMs);

    req.on("error", (e) => {
      clearTimeout(timer);
      reject(e);
    });

    req.end();
  });
}

const timeAgo = (isoStr: string): string => {
  const diff = (Date.now() - new Date(isoStr).getTime()) / 1000;
  if (diff < 60) return `${Math.round(diff)}s ago`;
  if (diff < 3600) return `${Math.round(diff / 60)}m ago`;
  return `${Math.round(diff / 3600)}h ago`;
};

const createSidebarSlot = (
  data: () => SnapshotData | null,
  allFiles: () => TrackedFile[],
  error: () => string | null,
): TuiSlotPlugin => {
  const [sidebarOpen, setSidebarOpen] = createSignal(false);

  const slotPlugin: TuiSlotPlugin = {
    order: 100,
    slots: {
      sidebar_content(ctx: TuiSlotContext, _value: { session_id: string }) {
        const theme = ctx.theme.current;
        const snapshot = data();
        const err = error();

        let statusLabel = "";
        let statusColor = theme.textMuted;
        let dotChar = "○";

        const hasFiles = allFiles().length > 0;
        if (err) {
          statusLabel = err;
          statusColor = theme.error;
          dotChar = "●";
        } else if (snapshot || hasFiles) {
          statusLabel = "Connected";
          statusColor = theme.success;
          dotChar = "●";
        } else {
          statusLabel = "Waiting...";
          statusColor = theme.textMuted;
          dotChar = "○";
        }

        let summary = "";
        if (snapshot) {
          const ago = timeAgo(snapshot.updatedAt);
          summary = `Last captured ${ago}`;
        } else if (hasFiles) {
          summary = "No snapshots yet";
        }

        const files = allFiles()
          .filter((f) => f.latestVersionId !== null)
          .map((f) => ({
            path: f.path,
            versionId: f.latestVersionId,
          }));
        const fileCount = files.length;
        const showArrow = fileCount > 0;

        return (
          <box flexDirection="column" gap={1}>
            <box
              flexDirection="row"
              gap={1}
              justifyContent="space-between"
              onMouseDown={() => setSidebarOpen((x) => !x)}
            >
              <text fg={theme.text}>
                <Show when={showArrow}>
                  <span>{sidebarOpen() ? "▼" : "▶"}</span>
                  <span>{" "}</span>
                </Show>
                <b>changez</b>
              </text>
              <text fg={theme.text}>
                <span style={{ fg: statusColor }}>{dotChar} </span>
                <span style={{ fg: theme.textMuted }}>{statusLabel}</span>
              </text>
            </box>

            <Show when={sidebarOpen()}>
              <box flexDirection="column" gap={0}>
                <Show when={summary}>
                  <text fg={theme.textMuted}>{summary}</text>
                </Show>

                <Show when={fileCount > 0}>
                  <For each={files}>
                    {(file) => (
                      <box flexDirection="row" gap={1} justifyContent="space-between">
                        <text fg={theme.textMuted} wrapMode="none" flexShrink={0}>
                          {"  • "}{path.basename(file.path)}
                        </text>
                        <text fg={theme.text} flexShrink={0}>
                          @v{file.versionId}
                        </text>
                      </box>
                    )}
                  </For>
                </Show>
              </box>
            </Show>
          </box>
        );
      },
    },
  };
  return slotPlugin;
};

const tui: TuiPlugin = async (
  api: TuiPluginApi,
  options: PluginOptions,
  _meta: TuiPluginMeta,
): Promise<void> => {
  const cfg: TuiConfig = {
    url: (options?.url as string) ?? "http://127.0.0.1:8760",
    token: (options?.token as string) ?? undefined,
  };

  const [data, setData] = createSignal<SnapshotData | null>(null);
  const [allFiles, setAllFiles] = createSignal<TrackedFile[]>([]);
  const [error, setError] = createSignal<string | null>(null);

  const slotId = api.slots.register(createSidebarSlot(data, allFiles, error));

  api.lifecycle.onDispose(() => {
    api.slots.unregister(slotId);
    if (pollInterval) clearInterval(pollInterval);
  });

  let healthy = false;
  try {
    const res = await httpGet(`${cfg.url}/health`, cfg.token, 5000);
    if (res.status >= 200 && res.status < 300) {
      healthy = true;
      api.ui.toast({
        variant: "success",
        title: "Changez",
        message: `Connected to ${cfg.url}`,
        duration: 2000,
      });
    } else {
      api.ui.toast({
        variant: "warning",
        title: "Changez",
        message: `Service responded with ${res.status}`,
        duration: 3000,
      });
    }
  } catch {
    api.ui.toast({
      variant: "error",
      title: "Changez",
      message: `Cannot reach service at ${cfg.url}`,
      duration: 4000,
    });
    setError("Probing...");
  }

  const projectRoot = path.resolve(process.cwd());
  const projectName = path.basename(projectRoot);
  let consecutiveFailures = 0;
  let probeFailures = 0;
  let isProbing = false;
  let pollInterval: ReturnType<typeof setInterval> | null = null;

  const startPolling = (interval: number, probing: boolean) => {
    if (pollInterval) clearInterval(pollInterval);
    isProbing = probing;
    pollInterval = setInterval(poll, interval);
  };

  const handlePollFailure = () => {
    if (isProbing) {
      probeFailures++;
      if (probeFailures >= 10) {
        setError("Service offline");
        if (pollInterval) clearInterval(pollInterval);
        pollInterval = null;
      }
    } else {
      consecutiveFailures++;
      if (consecutiveFailures >= 3) {
   setError("Probing...");
        consecutiveFailures = 0;
        probeFailures = 0;
        startPolling(60_000, true);
      }
    }
  };

  const poll = async () => {
    const snapshotUrl = `${cfg.url}/api/snapshots/latest?projectRoot=${encodeURIComponent(projectRoot)}`;
    const filesUrl = `${cfg.url}/api/files?project=${encodeURIComponent(projectName)}&limit=1000`;
    try {
      const [snapshotRes, filesRes] = await Promise.all([
        httpGet(snapshotUrl, cfg.token, 5000),
        httpGet(filesUrl, cfg.token, 5000),
      ]);

      if (snapshotRes.status !== 200 || !snapshotRes.body) {
        handlePollFailure();
        return;
      }
      consecutiveFailures = 0;
      probeFailures = 0;
      if (isProbing) {
        setError(null);
        startPolling(8000, false);
      }
      setData(JSON.parse(snapshotRes.body));

      if (filesRes.status === 200 && filesRes.body) {
        const parsed = JSON.parse(filesRes.body);
        setAllFiles(parsed.files ?? []);
        setError(null);
      } else if (filesRes.status >= 400) {
        setError(`Files error ${filesRes.status}`);
      }
    } catch {
      handlePollFailure();
    }
  };

  if (healthy) {
    await poll();
    startPolling(8000, false);
  } else {
    startPolling(60_000, true);
  }
};

export default {
  id: "changez-tui",
  tui,
} satisfies TuiPluginModule & { id: string };
