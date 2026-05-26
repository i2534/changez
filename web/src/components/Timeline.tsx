import { useState, useMemo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { relativeTime, sourceColor, actionIcon } from "../utils";
import { apiJSON } from "../api/client";
import { VersionEntry } from "../api/types";

const ALL_ACTIONS = ["create", "update", "delete"];

interface SourceInfo {
  name: string;
  version_count: number;
}

interface SourcesResponse {
  sources: SourceInfo[];
}

export default function Timeline({
  entries,
  selectedIds,
  onVersionClick,
  onDiff,
}: {
  entries: VersionEntry[];
  selectedIds: number[];
  onVersionClick: (id: number) => void;
  onDiff: (from: number, to: number) => void;
}) {
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [sourceFilter, setSourceFilter] = useState<string>("");
  const [actionFilter, setActionFilter] = useState<string>("");
  const [sources, setSources] = useState<string[]>([]);
  const { t } = useTranslation();

  useEffect(() => {
    apiJSON<SourcesResponse>("/api/sources").then(
      (res) => setSources(res.sources.map((s) => s.name)),
      () => setSources([])
    );
  }, []);

  const filtered = useMemo(() => {
    if (!sourceFilter && !actionFilter) return entries;
    return entries.filter((e) => {
      if (sourceFilter && e.source !== sourceFilter) return false;
      if (actionFilter && e.action !== actionFilter) return false;
      return true;
    });
  }, [entries, sourceFilter, actionFilter]);

  const hasFilters = sourceFilter || actionFilter;

  return (
    <div>
      <div className="mb-3 flex gap-3">
        <select
          value={sourceFilter}
          onChange={(e) => setSourceFilter(e.target.value)}
          className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
        >
          <option value="">{t("timeline.all_sources")}</option>
          {sources.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <select
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
        >
          <option value="">{t("timeline.all_actions")}</option>
          {ALL_ACTIONS.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        {hasFilters && (
          <button
            onClick={() => {
              setSourceFilter("");
              setActionFilter("");
            }}
            className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-400 hover:text-gray-200"
          >
            {t("common.clear")}
          </button>
        )}
        {hasFilters && (
          <span className="self-center text-xs text-gray-500">
            {filtered.length} / {entries.length}
          </span>
        )}
      </div>

      {filtered.length === 0 ? (
        <p className="py-8 text-center text-sm text-gray-500">
          {t("timeline.no_versions_match")}
        </p>
      ) : (
        <div className="relative space-y-0">
          {filtered.map((entry, i) => {
            const isSelected = selectedIds.includes(entry.versionId);
            const isExpanded = expandedId === entry.versionId;

            return (
              <div key={entry.versionId} className="relative flex">
                <div className="flex flex-shrink-0 flex-col items-center">
                  <div
                    className={`z-10 flex h-5 w-5 items-center justify-center rounded-full text-xs ${
                      isSelected ? "bg-blue-500 ring-2 ring-blue-300" : `${sourceColor(entry.source)} ring-2 ring-gray-800`
                    } text-white`}
                  >
                    {actionIcon(entry.action)}
                  </div>
                  {i < filtered.length - 1 && (
                    <div className="h-full w-px bg-gray-700" />
                  )}
                </div>
                <div className="flex-1 pb-6 pl-4">
                  <div
                    className={`rounded-lg p-3 transition-colors ${
                      isSelected ? "border-l-2 border-blue-500 bg-blue-900/40" : "bg-gray-800 hover:bg-gray-700"
                    }`}
                  >
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => onVersionClick(entry.versionId)}
                        className="font-mono text-sm font-bold text-gray-100 hover:underline"
                      >
                        v{entry.versionId}
                      </button>
                      <span className={`rounded px-1.5 py-0.5 text-xs font-medium ${sourceColor(entry.source)} text-white`}>
                        {entry.source}
                      </span>
                      <span className="text-xs text-gray-400">
                        {actionIcon(entry.action)} {entry.action}
                      </span>
                      <span className="text-xs text-gray-500">
                        {relativeTime(entry.timestamp, t)}
                      </span>
                      <span className="text-xs text-gray-500">
                        ({new Date(entry.timestamp).toLocaleString()})
                      </span>
                    </div>

                    {(entry.sessionId || entry.model || entry.message) && (
                      <button
                        onClick={() =>
                          setExpandedId(isExpanded ? null : entry.versionId)
                        }
                        className="mt-1 text-xs text-blue-400 hover:text-blue-300"
                      >
                        {isExpanded ? t("timeline.hide_details") : t("timeline.show_details")}
                      </button>
                    )}

                    {isExpanded && (
                      <div className="mt-2 rounded bg-gray-900 p-2 text-xs text-gray-400">
                        {entry.sessionId && (
                          <div>
                            <span className="text-gray-500">{t("timeline.session")}:</span>{" "}
                            {entry.sessionId}
                          </div>
                        )}
                        {entry.model && (
                          <div>
                            <span className="text-gray-500">{t("timeline.model")}:</span> {entry.model}
                          </div>
                        )}
                        {entry.message && (
                          <div>
                            <span className="text-gray-500">{t("timeline.message")}:</span>{" "}
                            {entry.message}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {selectedIds.length >= 2 && (
        <div className="sticky bottom-0 z-10 flex items-center justify-center gap-3 rounded-t-lg border-t border-gray-700 bg-gray-800/95 p-3 backdrop-blur">
          <span className="text-sm text-gray-400">
            v{selectedIds[0]} → v{selectedIds[1]}
          </span>
          <button
            onClick={() => {
              const [from, to] =
                selectedIds[0] < selectedIds[1]
                  ? selectedIds
                  : [selectedIds[1], selectedIds[0]];
              onDiff(from, to);
            }}
            className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-500"
          >
            {t("timeline.view_diff")}
          </button>
        </div>
      )}
    </div>
  );
}
