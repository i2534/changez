import { useState, useEffect, useMemo } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { getAISession } from "../api/client";
import { SessionResponse } from "../api/types";
import Skeleton from "../components/Skeleton";
import { ActionIcon, relativeTime } from "../utils";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function SessionAnalysis() {
  const { project, sessionId } = useParams<{ project: string; sessionId: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation();

  const [session, setSession] = useState<SessionResponse | null>(null);
  const [loading, setLoading] = useState(true);

  const projectName = decodeURIComponent(project || "");
  const decodedSessionId = decodeURIComponent(sessionId || "");

  useEffect(() => {
    if (!projectName || !decodedSessionId) return;
    const abort = new AbortController();
    setLoading(true);
    getAISession({ project: projectName, sessionId: decodedSessionId })
      .then(setSession)
      .catch((err) => {
        if (abort.signal.aborted) return;
        toast.error(err instanceof Error ? err.message : t("session.failed_to_load"));
      })
      .finally(() => setLoading(false));
    return () => abort.abort();
  }, [projectName, decodedSessionId, t]);

  const uniqueFiles = useMemo(() => {
    if (!session) return 0;
    return new Set(session.changes.map((c) => c.filePath)).size;
  }, [session]);

  const duration = useMemo(() => {
    if (!session?.changes.length) return "";
    const timestamps = session.changes
      .map((c) => new Date(c.timestamp).getTime())
      .filter((t) => !isNaN(t));
    if (timestamps.length < 2) return "";
    const diffMs = Math.max(...timestamps) - Math.min(...timestamps);
    const hours = Math.floor(diffMs / 3600000);
    const mins = Math.floor((diffMs % 3600000) / 60000);
    const secs = Math.floor((diffMs % 60000) / 1000);
    const parts: string[] = [];
    if (hours > 0) parts.push(`${hours}h`);
    if (mins > 0) parts.push(`${mins}m`);
    if (secs > 0 || parts.length === 0) parts.push(`${secs}s`);
    return parts.join(" ");
  }, [session]);

  const handleCopy = async () => {
    if (!session?.summary) return;
    try {
      await navigator.clipboard.writeText(session.summary);
      toast.success(t("session.copied"));
    } catch {
      toast.error(t("session.copy") + " failed");
    }
  };

  if (loading) {
    return <LoadingSkeleton />;
  }

  if (!session) {
    return (
      <div className="flex flex-col items-center gap-4 py-16 text-gray-400">
        <p>{t("session.failed_to_load")}</p>
        <button
          onClick={() => window.location.reload()}
          className="rounded-lg bg-gray-800 px-4 py-2 text-sm text-gray-200 transition-colors hover:bg-gray-700"
        >
          {t("common.retry")}
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <button
        onClick={() => navigate(`/projects/${encodeURIComponent(projectName)}/files`)}
        className="flex items-center gap-2 text-sm text-gray-400 transition-colors hover:text-gray-200"
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" className="stroke-current">
          <path d="M10 12L6 8l4-4" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span>{t("session.back_to_timeline")}</span>
      </button>

      <div className="flex items-center gap-3">
        <h1 className="text-xl font-bold text-gray-100">
          {t("session.title")}
        </h1>
        <span className="rounded bg-gray-800 px-2 py-0.5 font-mono text-xs text-gray-400">
          {session.sessionId}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <StatCard
          label={t("session.changes_count")}
          value={session.changes.length}
        />
        <StatCard label={t("session.files_count")} value={uniqueFiles} />
        <StatCard
          label={t("session.model")}
          value={session.model ?? "—"}
        />
      </div>

      {duration && (
        <div className="rounded-lg bg-gray-800 p-4 text-sm">
          <span className="text-gray-400">{t("session.duration")}:</span>{" "}
          <span className="font-medium text-gray-200">{duration}</span>
        </div>
      )}

      <div className="relative rounded-lg border border-purple-500/30 bg-gray-800/50 p-5">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-sm font-semibold text-purple-300">
            <span className="text-lg">✦</span>
            {t("session.ai_session_summary")}
          </h2>
          {session.summary && (
            <button
              onClick={handleCopy}
              className="rounded bg-gray-700 px-2.5 py-1 text-xs text-gray-300 transition-colors hover:bg-gray-600"
            >
              {t("session.copy")}
            </button>
          )}
        </div>

        {session.summary ? (
          <>
            <p className="whitespace-pre-wrap text-sm leading-relaxed text-gray-300">
              {session.summary}
            </p>
            {session.model && (
              <div className="mt-3 text-xs text-gray-500">
                [{session.model}]
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center gap-3 py-4 text-sm text-gray-400">
            <svg
              className="h-5 w-5 animate-spin text-purple-400"
              viewBox="0 0 24 24"
              fill="none"
            >
              <circle
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                strokeWidth="3"
                strokeLinecap="round"
                className="opacity-25"
              />
              <path
                d="M12 2a10 10 0 0 1 10 10"
                stroke="currentColor"
                strokeWidth="3"
                strokeLinecap="round"
              />
            </svg>
            {t("session.generating")}
          </div>
        )}
      </div>

      <div>
        <h2 className="mb-3 text-sm font-semibold text-gray-300">
          {t("session.file_changes")}{" "}
          <span className="font-normal text-gray-500">
            ({session.changes.length})
          </span>
        </h2>

        {session.changes.length === 0 ? (
          <p className="py-8 text-center text-sm text-gray-500">
            {t("session.no_changes")}
          </p>
        ) : (
          <div className="space-y-1">
            {session.changes.map((change, idx) => {
              const actionColor =
                change.action === "create"
                  ? "text-green-400"
                  : change.action === "delete"
                    ? "text-red-400"
                    : "text-blue-400";

              const actionLabel =
                change.action === "create"
                  ? "create"
                  : change.action === "delete"
                    ? "delete"
                    : "update";

              return (
                <div
                  key={`${change.filePath}-${change.timestamp}-${idx}`}
                  className="flex items-center gap-3 rounded-lg bg-gray-800 px-4 py-2.5 text-sm transition-colors hover:bg-gray-750 hover:bg-gray-700/50"
                >
                  <span className={actionColor}>
                    <ActionIcon action={change.action} />
                  </span>
                  <span className="font-mono text-xs text-gray-300">
                    [{actionLabel}]
                  </span>
                  <span className="min-w-0 flex-1 truncate font-mono text-gray-200">
                    {change.filePath}
                  </span>
                  <span className="shrink-0 text-xs text-gray-500">
                    {relativeTime(change.timestamp, t)}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
}: {
  label: string;
  value: number | string;
}) {
  return (
    <div className="rounded-lg bg-gray-800 p-4">
      <div className="text-lg font-bold text-gray-100">
        {typeof value === "number" ? value.toLocaleString() : value}
      </div>
      <div className="mt-1 text-xs text-gray-400">{label}</div>
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton variant="row" className="w-32" />
      <Skeleton variant="row" className="w-48" />
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {[1, 2, 3].map((i) => (
          <Skeleton key={i} variant="card" className="h-20" />
        ))}
      </div>
      <Skeleton variant="block" className="h-32" />
      <Skeleton variant="row" className="w-36" />
      {[1, 2, 3, 4, 5].map((i) => (
        <Skeleton key={i} variant="row" />
      ))}
    </div>
  );
}
