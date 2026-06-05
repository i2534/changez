import { useState, useEffect, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { getAITrends } from "../api/client";
import { TrendsResponse } from "../api/types";
import Skeleton from "../components/Skeleton";
import { sourceColor } from "../utils";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

function getDate(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() - days);
  return d.toISOString().slice(0, 10);
}

export default function TrendsAnalysis() {
  const { project } = useParams<{ project: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");

  const [since, setSince] = useState(getDate(7));
  const [until] = useState(getDate(0));
  const [data, setData] = useState<TrendsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [quickRange, setQuickRange] = useState<"7" | "30" | "custom">("7");

  const fetchData = useCallback(() => {
    if (!projectName) return;
    const abort = new AbortController();
    setLoading(true);
    setError(null);
    getAITrends({ project: projectName, since, until, topFiles: 10 })
      .then((res) => {
        setData(res);
      })
      .catch((err) => {
        if (abort.signal.aborted) return;
        setError(err instanceof Error ? err.message : t("trends.failed_to_load"));
      })
      .finally(() => setLoading(false));
    return () => abort.abort();
  }, [projectName, since, until, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleQuickRange = (range: "7" | "30" | "custom") => {
    setQuickRange(range);
    if (range === "7") {
      setSince(getDate(7));
    } else if (range === "30") {
      setSince(getDate(30));
    }
  };

  const handleCopySummary = async () => {
    if (!data?.summary) return;
    try {
      await navigator.clipboard.writeText(data.summary);
      toast.success(t("trends.copied"));
    } catch {
      toast.error(t("common.error"));
    }
  };

  const totalSourceCount = data
    ? Object.values(data.sourceBreakdown).reduce((a, b) => a + b, 0)
    : 0;

  if (loading) {
    return <LoadingSkeleton />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <div className="text-lg text-gray-300">{error}</div>
        <button
          onClick={fetchData}
          className="mt-4 rounded-lg bg-gray-700 px-4 py-2 text-sm text-gray-200 transition-colors hover:bg-gray-600"
        >
          {t("common.retry")}
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(`/projects/${encodeURIComponent(projectName)}`)}
            className="rounded-lg bg-gray-800 px-3 py-1.5 text-sm text-gray-300 transition-colors hover:bg-gray-700"
          >
            {t("trends.back")}
          </button>
          <div>
            <h1 className="text-lg font-bold text-gray-100">{t("trends.title")}</h1>
            <p className="text-sm text-gray-400">{projectName}</p>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 rounded-lg border border-gray-700 bg-gray-800 p-3">
        <span className="text-xs font-medium text-gray-400">{t("trends.date_range")}:</span>
        <button
          onClick={() => handleQuickRange("7")}
          className={`rounded px-3 py-1 text-xs font-medium transition-colors ${
            quickRange === "7"
              ? "bg-blue-500/20 text-blue-400"
              : "text-gray-400 hover:bg-gray-700 hover:text-gray-300"
          }`}
        >
          {t("trends.last_7_days")}
        </button>
        <button
          onClick={() => handleQuickRange("30")}
          className={`rounded px-3 py-1 text-xs font-medium transition-colors ${
            quickRange === "30"
              ? "bg-blue-500/20 text-blue-400"
              : "text-gray-400 hover:bg-gray-700 hover:text-gray-300"
          }`}
        >
          {t("trends.last_30_days")}
        </button>
        <button
          onClick={() => handleQuickRange("custom")}
          className={`rounded px-3 py-1 text-xs font-medium transition-colors ${
            quickRange === "custom"
              ? "bg-blue-500/20 text-blue-400"
              : "text-gray-400 hover:bg-gray-700 hover:text-gray-300"
          }`}
        >
          {t("trends.custom")}
        </button>
        <div className="ml-auto flex items-center gap-2">
          <label className="flex items-center gap-2 text-xs text-gray-400">
            {t("trends.from")}:
            <input
              type="date"
              value={since}
              onChange={(e) => {
                setSince(e.target.value);
                setQuickRange("custom");
              }}
              className="rounded border border-gray-700 bg-gray-900 px-2 py-1 text-xs text-gray-300 focus:border-blue-500 focus:outline-none"
            />
          </label>
          <span className="text-xs text-gray-500">{t("trends.to")}:</span>
          <label className="flex items-center gap-2 text-xs text-gray-400 cursor-default">
            <input
              type="date"
              value={until}
              readOnly
              className="rounded border border-gray-700 bg-gray-800/50 px-2 py-1 text-xs text-gray-500 cursor-default select-none"
              title={t("trends.today_only")}
            />
          </label>
          <button
            onClick={fetchData}
            disabled={loading}
            className="ml-1 rounded bg-blue-500 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-blue-600 disabled:opacity-50"
          >
            {t("trends.query")}
          </button>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <div className="rounded-lg bg-gray-800 p-5">
          <div className="text-2xl font-bold text-gray-100">
            {data?.totalChanges?.toLocaleString() ?? 0}
          </div>
          <div className="mt-1 text-sm text-gray-400">{t("trends.total_changes")}</div>
        </div>
        <div className="rounded-lg bg-gray-800 p-5">
          <div className="text-2xl font-bold text-gray-100">
            {data?.totalFiles?.toLocaleString() ?? 0}
          </div>
          <div className="mt-1 text-sm text-gray-400">{t("trends.files_affected")}</div>
        </div>
        <div className="rounded-lg bg-gray-800 p-5">
          <div className="text-sm font-bold text-gray-100">{data?.period ?? ""}</div>
          <div className="mt-1 text-sm text-gray-400">{t("trends.period")}</div>
        </div>
      </div>

        {(data?.summary || data?.status === "pending") && (
          <div className="rounded-lg border border-purple-900/30 bg-purple-900/10 p-5">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" className="text-purple-400">
                <path
                  d="M8 1l1.5 4.5L14 7l-4.5 1.5L8 13l-1.5-4.5L2 7l4.5-1.5L8 1z"
                  fill="currentColor"
                />
              </svg>
              <h3 className="text-sm font-semibold text-purple-400">
                {t("trends.ai_trends_insights")}
              </h3>
            </div>
            {data?.summary && (
              <button
                onClick={handleCopySummary}
                className="text-xs text-gray-500 transition-colors hover:text-gray-400"
              >
                {t("trends.copy")}
              </button>
            )}
          </div>

          {data?.status === "pending" ? (
            <div className="flex items-center gap-2">
              <svg
                className="animate-spin h-4 w-4 text-purple-400"
                viewBox="0 0 24 24"
                fill="none"
              >
                <circle
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  strokeWidth="3"
                  strokeDasharray="31.4"
                  strokeDashoffset="10"
                />
              </svg>
              <span className="text-sm text-purple-400/80">{t("trends.generating")}</span>
              <div className="ml-auto flex gap-0.5">
                <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-purple-400" />
                <span
                  className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-purple-400"
                  style={{ animationDelay: "0.2s" }}
                />
                <span
                  className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-purple-400"
                  style={{ animationDelay: "0.4s" }}
                />
              </div>
            </div>
          ) : (
            <div className="space-y-2 text-sm leading-relaxed text-gray-300">
              {data?.summary?.split("\n").map((line, i) => (
                <p key={i}>{line}</p>
              ))}
              {data?.model && (
                <div className="mt-3 border-t border-purple-900/20 pt-2 text-xs text-gray-500">
                  Model: {data.model}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      <div className="grid gap-6 sm:grid-cols-2">
        <div className="rounded-lg bg-gray-800 p-5">
          <h3 className="mb-3 text-sm font-semibold text-gray-300">
            {t("trends.source_distribution")}
          </h3>
          {data?.sourceBreakdown && totalSourceCount > 0 ? (
            <div className="space-y-2">
              {Object.entries(data.sourceBreakdown)
                .sort((a, b) => b[1] - a[1])
                .map(([name, count]) => {
                  const pct = Math.round((count / totalSourceCount) * 100);
                  return (
                    <div key={name} className="flex items-center gap-3">
                      <span className="w-20 truncate text-right text-sm text-gray-400">
                        {name}
                      </span>
                      <div className="flex h-5 flex-1 overflow-hidden rounded bg-gray-700">
                        <div
                          className={`h-full ${sourceColor(name)} transition-all`}
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                      <span className="w-16 text-sm text-gray-300">
                        {count} ({pct}%)
                      </span>
                    </div>
                  );
                })}
            </div>
          ) : (
            <p className="text-sm text-gray-500">{t("trends.no_data")}</p>
          )}
        </div>

          <div className="rounded-lg bg-gray-800 p-5">
          <h3 className="mb-3 text-sm font-semibold text-gray-300">{t("trends.top_files")}</h3>
          {data?.topFiles && data.topFiles.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-700 text-gray-400">
                    <th className="pb-2 pr-4 text-left text-xs font-medium">#</th>
                    <th className="pb-2 pr-4 text-left text-xs font-medium">File</th>
                    <th className="pb-2 text-right text-xs font-medium">Changes</th>
                  </tr>
                </thead>
                <tbody>
                  {data.topFiles.map((file, i) => (
                    <tr
                      key={file.filePath}
                      className="border-b border-gray-800 last:border-0"
                    >
                      <td className="py-2 pr-4 text-gray-500">{i + 1}</td>
                      <td className="py-2 pr-4 font-mono text-gray-300">
                        <button
                          onClick={() =>
                            navigate(
                              `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(file.filePath)}`
                            )
                          }
                          className="truncate text-left transition-colors hover:text-blue-400"
                          title={file.filePath}
                        >
                          {file.filePath}
                        </button>
                      </td>
                      <td className="py-2 text-right font-medium text-gray-200">
                        {file.count}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="text-sm text-gray-500">{t("trends.no_data")}</p>
          )}
        </div>
      </div>
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton variant="row" className="h-8 w-48" />
      <Skeleton variant="row" className="h-12" />
      <div className="grid gap-4 sm:grid-cols-3">
        {[1, 2, 3].map((i) => (
          <Skeleton key={i} variant="card" />
        ))}
      </div>
      <Skeleton variant="block" className="h-32" />
      <div className="grid gap-6 sm:grid-cols-2">
        <Skeleton variant="block" className="h-48" />
        <Skeleton variant="block" className="h-48" />
      </div>
    </div>
  );
}
