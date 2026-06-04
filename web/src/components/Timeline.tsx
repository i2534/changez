import { useState, useMemo, useCallback, useEffect, useRef } from "react";
import type { ListImperativeAPI, RowComponentProps } from "react-window";
import { List, useDynamicRowHeight } from "react-window";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { relativeTime, sourceColor, actionIcon } from "../utils";
import { apiJSON } from "../api/client";
import { VersionEntry, RestoreResponse } from "../api/types";
import CodeView from "./CodeView";
import "prismjs/components/prism-json.min.js";
import "prismjs/components/prism-css.min.js";
import "prismjs/components/prism-ini.min.js";


const ALL_ACTIONS = ["create", "update", "delete"];

interface SourceInfo {
  name: string;
  version_count: number;
}

interface SourcesResponse {
  sources: SourceInfo[];
}

const ROW_BASE = 70;
const ROW_METADATA = 90;
const ROW_EXPANDED = 130;
const ROW_CONTENT_LOADING = 130;
const ROW_CONTENT_LOADED = 310; // header~30 + height-250 + padding~30
const DEFAULT_ROW_HEIGHT = 80;


interface TimelineFilterProps {
  sources: string[];
  sourceFilter: string;
  actionFilter: string;
  hasFilters: boolean;
  filteredCount: number;
  totalCount: number;
  versionJumpRef: React.RefObject<HTMLInputElement>;
  versionJump: string;
  onVersionJumpChange: (value: string) => void;
  onVersionJumpSubmit: () => void;
  onSourceChange: (value: string) => void;
  onActionChange: (value: string) => void;
  onClear: () => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

function TimelineFilter({
  sources,
  sourceFilter,
  actionFilter,
  hasFilters,
  filteredCount,
  totalCount,
  versionJumpRef,
  versionJump,
  onVersionJumpChange,
  onVersionJumpSubmit,
  onSourceChange,
  onActionChange,
  onClear,
  t,
}: TimelineFilterProps) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-3">
      <input
        ref={versionJumpRef}
        type="number"
        value={versionJump}
        onChange={(e) => onVersionJumpChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onVersionJumpSubmit();
          }
        }}
        placeholder={t("timeline.version_jump_placeholder")}
        className="w-28 rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none"
      />
      <select
        value={sourceFilter}
        onChange={(e) => onSourceChange(e.target.value)}
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
        onChange={(e) => onActionChange(e.target.value)}
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
          onClick={onClear}
          className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-400 hover:text-gray-200"
        >
          {t("common.clear")}
        </button>
      )}
      {hasFilters && (
        <span className="self-center text-xs text-gray-500">
          {filteredCount} / {totalCount}
        </span>
      )}
    </div>
  );
}

interface TimelineRowData {
  filtered: VersionEntry[];
  expandedId: number | null;
  setExpandedId: (id: number | null) => void;
  contentVersionId: number | null;
  setContentVersionId: (id: number | null) => void;
  filePath: string;
  selectedIds: number[];
  contentMap: Map<number, string>;
  loadingContent: number | null;
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onRowClick: (index: number) => void;
  onDiff: (from: number, to: number) => void;
  fetchInlineContent: (versionId: number) => Promise<void>;
  onCopyContent: (content: string) => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

const TimelineRowComponent = ({
  index,
  style,
  filtered,
  expandedId,
  setExpandedId,
  contentVersionId,
  setContentVersionId,
  filePath,
  selectedIds,
  contentMap,
  loadingContent,
  onVersionClick,
  onRowClick,
  onDiff,
  fetchInlineContent,
  onCopyContent,
  t,
}: RowComponentProps<TimelineRowData>) => {
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
};

interface TimelineListProps {
  filtered: VersionEntry[];
  expandedId: number | null;
  setExpandedId: (id: number | null) => void;
  contentVersionId: number | null;
  setContentVersionId: (id: number | null) => void;
  filePath: string;
  selectedIds: number[];
  contentMap: Map<number, string>;
  loadingContent: number | null;
  onListRef: (ref: ListImperativeAPI | null | undefined) => void;
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onRowClick: (index: number) => void;
  onDiff: (from: number, to: number) => void;
  fetchInlineContent: (versionId: number) => Promise<void>;
  onCopyContent: (content: string) => void;
  onRowHeightChange: ((versionId: number, height: number) => void) | null;
  t: (key: string, params?: Record<string, unknown>) => string;
}

function TimelineList({
  filtered,
  expandedId,
  setExpandedId,
  contentVersionId,
  setContentVersionId,
  filePath,
  selectedIds,
  contentMap,
  loadingContent,
  onListRef,
  onVersionClick,
  onRowClick,
  onDiff,
  fetchInlineContent,
  onCopyContent,
  onRowHeightChange,
  t,
}: TimelineListProps) {
  const internalListRef = useRef<ListImperativeAPI | null>(null);
  const dynamicRowHeight = useDynamicRowHeight({ defaultRowHeight: DEFAULT_ROW_HEIGHT });

  useEffect(() => {
    onListRef(internalListRef.current);
  }, [internalListRef, onListRef]);

  useEffect(() => {
    for (let i = 0; i < filtered.length; i++) {
      const entry = filtered[i];
      if (!entry) continue;
      const isMetadataExpanded = expandedId === entry.versionId;
      const isContentExpanded = contentVersionId === entry.versionId;
      const hasMeta = entry.sessionId || entry.model || entry.message;
      let height: number;
      if (hasMeta) {
        height = ROW_METADATA;
      } else {
        height = ROW_BASE;
      }
      if (isMetadataExpanded) {
        height += ROW_EXPANDED - ROW_METADATA;
      }
      if (isContentExpanded) {
        if (loadingContent === entry.versionId) height += ROW_CONTENT_LOADING;
        else if (contentMap.has(entry.versionId)) height += ROW_CONTENT_LOADED;
      }
      dynamicRowHeight.setRowHeight(i, height);
      onRowHeightChange?.(entry.versionId, height);
    }
  }, [filtered, expandedId, contentVersionId, contentMap, loadingContent, dynamicRowHeight, onRowHeightChange]);

  const rowData = useMemo<TimelineRowData>(
    () => ({
      filtered,
      expandedId,
      setExpandedId,
      contentVersionId,
      setContentVersionId,
      filePath,
      selectedIds,
      contentMap,
      loadingContent,
      onVersionClick,
      onRowClick,
      onDiff,
      fetchInlineContent,
      onCopyContent,
      t,
    }),
    [filtered, expandedId, setExpandedId, contentVersionId,
      setContentVersionId,
      filePath, selectedIds, contentMap, loadingContent, onVersionClick, onRowClick, onDiff, fetchInlineContent, onCopyContent, t]
  );

  const listHeight = typeof window !== "undefined" ? Math.max(300, window.innerHeight - 300) : 500;

  return (
    <>
      {filtered.length === 0 ? (
        <p className="py-8 text-center text-sm text-gray-500">
          {t("timeline.no_versions_match")}
        </p>
      ) : (
        <List
          listRef={(inst) => { internalListRef.current = inst ?? null; }}
          style={{ height: listHeight, width: "100%" }}
          rowCount={filtered.length}
          rowHeight={dynamicRowHeight}
          overscanCount={2}
          rowComponent={TimelineRowComponent}
          rowProps={rowData}
        />
      )}

      {selectedIds.length >= 2 && (
        <div className="sticky bottom-0 z-10 flex items-center justify-center gap-3 rounded-t-lg border-t border-gray-700 bg-gray-800/95 p-3 backdrop-blur">
          <span className="text-sm text-gray-400">
            v{selectedIds[0]} → v{selectedIds[1]}
          </span>
          <button
            onClick={() => {
              const [from, to] = selectedIds[0] < selectedIds[1] ? selectedIds : [selectedIds[1], selectedIds[0]];
              onDiff(from, to);
            }}
            className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-500"
          >
            {t("timeline.view_diff")}
          </button>
        </div>
      )}
    </>
  );
}

export default function Timeline({
  entries,
  selectedIds,
  filePath,
  onVersionClick,
  onDiff,
}: {
  entries: VersionEntry[];
  selectedIds: number[];
  filePath: string;
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onDiff: (from: number, to: number) => void;
}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [contentVersionId, setContentVersionId] = useState<number | null>(null);
  const [sources, setSources] = useState<string[]>([]);
  const [contentMap, setContentMap] = useState(new Map<number, string>());
  const [loadingContent, setLoadingContent] = useState<number | null>(null);
  const [versionJump, setVersionJump] = useState("");
  const listRef = useRef<ListImperativeAPI | null>(null);
  const versionJumpRef = useRef<HTMLInputElement>(null);

  const sourceFilter = searchParams.get("source") || "";
  const actionFilter = searchParams.get("action") || "";

  const setSourceFilter = (value: string) => {
    if (value) searchParams.set("source", value);
    else searchParams.delete("source");
    setSearchParams(searchParams);
  };
  const setActionFilter = (value: string) => {
    if (value) searchParams.set("action", value);
    else searchParams.delete("action");
    setSearchParams(searchParams);
  };

  const { t } = useTranslation();

  useEffect(() => {
    apiJSON<SourcesResponse>("/api/sources").then(
      (res) => setSources(res.sources.map((s) => s.name)),
      () => setSources([])
    );
  }, []);

  const fetchInlineContent = useCallback(
    async (versionId: number) => {
      setLoadingContent(versionId);
      try {
        const data = await apiJSON<RestoreResponse>(
          `/api/files/restore?path=${encodeURIComponent(filePath)}&version=${versionId}`
        );
        setContentMap((prev) => new Map(prev).set(versionId, data.content));
      } catch {
        setContentMap((prev) => {
          const next = new Map(prev);
          next.set(versionId, "");
          return next;
        });
      } finally {
        setLoadingContent(null);
      }
    },
    [filePath]
  );

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "g") {
        e.preventDefault();
        versionJumpRef.current?.focus();
        versionJumpRef.current?.select();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const filtered = useMemo(() => {
    if (!sourceFilter && !actionFilter) return entries;
    return entries.filter((e) => {
      if (sourceFilter && e.source !== sourceFilter) return false;
      if (actionFilter && e.action !== actionFilter) return false;
      return true;
    });
  }, [entries, sourceFilter, actionFilter]);

  const hasFilters = Boolean(sourceFilter || actionFilter);

  const handleVersionJump = useCallback(() => {
    const targetId = parseInt(versionJump, 10);
    if (isNaN(targetId)) return;
    const idx = filtered.findIndex((e) => e.versionId === targetId);
    if (idx >= 0 && listRef.current) {
      listRef.current.scrollToRow({ index: idx, align: "start" });
    }
    setVersionJump("");
  }, [versionJump, filtered]);

  const handleRowClick = useCallback(
    (index: number) => {
      const entry = filtered[index];
      setExpandedId((prev) => (prev === entry.versionId ? null : entry.versionId));
    },
    [filtered]
  );

  const handleCopyContent = useCallback(
    async (content: string) => {
      try {
        await navigator.clipboard.writeText(content);
      } catch {
        // Fallback for older browsers
        const ta = document.createElement("textarea");
        ta.value = content;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
      }
    },
    []
  );

  return (
    <div>
      <TimelineFilter
        sources={sources}
        sourceFilter={sourceFilter}
        actionFilter={actionFilter}
        hasFilters={hasFilters}
        filteredCount={filtered.length}
        totalCount={entries.length}
        versionJumpRef={versionJumpRef}
        versionJump={versionJump}
        onVersionJumpChange={setVersionJump}
        onVersionJumpSubmit={handleVersionJump}
        onSourceChange={setSourceFilter}
        onActionChange={setActionFilter}
        onClear={() => {
          setSourceFilter("");
          setActionFilter("");
        }}
        t={t}
      />
      <TimelineList
         filtered={filtered}
         expandedId={expandedId}
         setExpandedId={setExpandedId}
         contentVersionId={contentVersionId}
        setContentVersionId={setContentVersionId}
        filePath={filePath}
          selectedIds={selectedIds}
         contentMap={contentMap}
         loadingContent={loadingContent}
         onListRef={(ref) => { listRef.current = ref ?? null; }}
         onVersionClick={onVersionClick}
         onRowClick={handleRowClick}
         onDiff={onDiff}
         fetchInlineContent={fetchInlineContent}
         onCopyContent={handleCopyContent}
         onRowHeightChange={null}
         t={t}
       />
    </div>
  );
}
