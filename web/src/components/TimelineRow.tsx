import type { RowComponentProps } from "react-window";
import { relativeTime, sourceColor, actionIcon } from "../utils";
import { useTimelineContext } from "./TimelineContext";
import CodeView from "./CodeView";
import "prismjs/components/prism-json.min.js";
import "prismjs/components/prism-css.min.js";
import "prismjs/components/prism-ini.min.js";

interface TimelineRowData {
  filtered: import("../api/types").VersionEntry[];
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onRowClick: (index: number) => void;
  onDiff: (from: number, to: number) => void;
  fetchInlineContent: (versionId: number) => Promise<void>;
  onCopyContent: (content: string) => void;
}

export function TimelineRow({
  index,
  style,
  filtered,
  onVersionClick,
  onRowClick,
  onDiff,
  fetchInlineContent,
  onCopyContent,
}: RowComponentProps<TimelineRowData>) {
  const {
    expandedId,
    setExpandedId,
    contentVersionId,
    setContentVersionId,
    filePath,
    selectedIds,
    contentMap,
    loadingContent,
    t,
  } = useTimelineContext();

  const entry = filtered[index];
  const isSelected = selectedIds.includes(entry.versionId);
  const isExpanded = expandedId === entry.versionId;
  const hasMeta = entry.sessionId || entry.model || entry.message;
  const showDiffBtn = selectedIds.length === 2 && isSelected;
  const inlineContent = contentMap.get(entry.versionId) ?? null;
  const isLoading = loadingContent === entry.versionId;

  return (
    <div style={style} className="relative flex">
      <div className="flex flex-shrink-0 flex-col items-center">
        <div
          onClick={(e) => {
            e.stopPropagation();
            onVersionClick(entry.versionId, e.shiftKey);
          }}
          className={`z-10 flex h-5 w-5 cursor-pointer items-center justify-center rounded-full text-xs ${isSelected ? "bg-blue-500 ring-2 ring-blue-300" : `${sourceColor(entry.source)} ring-2 ring-gray-800`} text-white`}
        >
          {actionIcon(entry.action)}
        </div>
        {index < filtered.length - 1 && (
          <div className="h-full w-px bg-gray-700" />
        )}
      </div>
      <div className="flex-1 overflow-x-hidden pb-6 pl-4">
        <div
          className={`rounded-lg p-3 transition-colors ${isSelected ? "border-l-2 border-blue-500 bg-blue-900/40" : "bg-gray-800 hover:bg-gray-700"}`}
          onClick={() => onRowClick(index)}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onVersionClick(entry.versionId, e.shiftKey);
                }}
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
            <div className="flex items-center gap-2">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setContentVersionId(contentVersionId === entry.versionId ? null : entry.versionId);
                  if (contentVersionId !== entry.versionId) {
                    fetchInlineContent(entry.versionId);
                  }
                }}
                className="rounded bg-gray-700 px-2 py-0.5 text-xs text-gray-300 hover:bg-gray-600 hover:text-gray-200"
              >
                {contentVersionId === entry.versionId
                  ? t("timeline.hide_content", { version: entry.versionId })
                  : t("timeline.view_content", { version: entry.versionId })}
              </button>
              {showDiffBtn && (
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    const [from, to] = selectedIds[0] < selectedIds[1] ? selectedIds : [selectedIds[1], selectedIds[0]];
                    onDiff(from, to);
                  }}
                  className="rounded bg-blue-600/80 px-2 py-0.5 text-xs text-white hover:bg-blue-500"
                >
                  {t("timeline.diff_between", { from: Math.min(...selectedIds), to: Math.max(...selectedIds) })}
                </button>
              )}
            </div>
          </div>

          {hasMeta && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                setExpandedId(isExpanded ? null : entry.versionId);
              }}
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
                  <span className="text-gray-500">{t("timeline.model")}:</span>{" "}
                  {entry.model}
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

          {contentVersionId === entry.versionId && (inlineContent !== null || isLoading) && (
            <div
              className="mt-3 w-full overflow-hidden rounded-lg border border-gray-700 bg-gray-900"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-center justify-between border-b border-gray-700 px-3 py-2">
                <span className="text-xs text-gray-400">
                  {t("timeline.content_at_v", { version: entry.versionId })}
                </span>
                {inlineContent !== null && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onCopyContent(inlineContent);
                    }}
                    className="rounded bg-gray-700 px-2 py-0.5 text-xs text-gray-300 hover:bg-gray-600 hover:text-gray-200"
                  >
                    {t("timeline.copy_clipboard", { version: entry.versionId })}
                  </button>
                )}
              </div>
              {isLoading ? (
                <div className="flex items-center justify-center py-8 text-sm text-gray-500">
                  {t("timeline.loading_content")}
                </div>
              ) : inlineContent !== null ? (
                <CodeView content={inlineContent} filePath={filePath} height={250} />
              ) : null}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
