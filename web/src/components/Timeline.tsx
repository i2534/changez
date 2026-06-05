import { useState, useMemo, useCallback, useEffect, useRef } from "react";
import type { ListImperativeAPI } from "react-window";
import { List, useDynamicRowHeight } from "react-window";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiJSON } from "../api/client";
import { VersionEntry, RestoreResponse } from "../api/types";
import { TimelineProvider, useTimelineContext } from "./TimelineContext";
import { TimelineFilter } from "./TimelineFilter";
import { TimelineRow } from "./TimelineRow";

const ROW_BASE = 70;
const ROW_SUMMARY = 80;
const ROW_METADATA = 90;
const ROW_EXPANDED = 220;
const ROW_CONTENT_LOADING = 130;
const ROW_CONTENT_LOADED = 310;
const DEFAULT_ROW_HEIGHT = 80;
interface TimelineListProps {
  filtered: VersionEntry[];
  onListRef: (ref: ListImperativeAPI | null | undefined) => void;
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onRowClick: (index: number) => void;
  onDiff: (from: number, to: number) => void;
  fetchInlineContent: (versionId: number) => Promise<void>;
  onCopyContent: (content: string) => void;
}

function TimelineList({ filtered, onListRef, onVersionClick, onRowClick, onDiff, fetchInlineContent, onCopyContent }: TimelineListProps) {
  const { expandedId, contentVersionId, contentMap, loadingContent, summaries } = useTimelineContext();
  const internalListRef = useRef<ListImperativeAPI | null>(null);
  const dynamicRowHeight = useDynamicRowHeight({ defaultRowHeight: DEFAULT_ROW_HEIGHT });
  useEffect(() => { onListRef(internalListRef.current); }, [internalListRef, onListRef]);
  useEffect(() => {
    for (let i = 0; i < filtered.length; i++) {
      const entry = filtered[i];
      if (!entry) continue;
      const isMeta = expandedId === entry.versionId;
      const isContent = contentVersionId === entry.versionId;
      const hasMeta = entry.sessionId || entry.model || entry.message;
      const hasSummary = summaries.has(entry.versionId);
      let height = hasMeta ? ROW_METADATA : hasSummary ? ROW_SUMMARY : ROW_BASE;
      if (isMeta) height += ROW_EXPANDED - (hasMeta ? ROW_METADATA : ROW_BASE);
      if (isContent) {
        if (loadingContent === entry.versionId) height += ROW_CONTENT_LOADING;
        else if (contentMap.has(entry.versionId)) height += ROW_CONTENT_LOADED;
      }
      dynamicRowHeight.setRowHeight(i, height);
    }
  }, [filtered, expandedId, contentVersionId, contentMap, loadingContent, summaries, dynamicRowHeight]);

  const rowData = useMemo(() => ({ filtered, onVersionClick, onRowClick, onDiff, fetchInlineContent, onCopyContent }),
    [filtered, onVersionClick, onRowClick, onDiff, fetchInlineContent, onCopyContent]);
  const listHeight = typeof window !== "undefined" ? Math.max(300, window.innerHeight - 300) : 500;
  return (
    <>
      {filtered.length === 0 ? (
        <p className="py-8 text-center text-sm text-gray-500">{useTimelineContext().t("timeline.no_versions_match")}</p>
      ) : (
        <List
          listRef={(inst) => { internalListRef.current = inst ?? null; }}
          style={{ height: listHeight, width: "100%" }}
          rowCount={filtered.length}
          rowHeight={dynamicRowHeight}
          overscanCount={2}
          rowComponent={TimelineRow}
          rowProps={rowData}
        />
      )}
    </>
  );
}

export default function Timeline({ entries, selectedIds, filePath, project, summaries, onVersionClick, onDiff }: {
  entries: VersionEntry[];
  selectedIds: number[];
  filePath: string;
  project: string;
  summaries: Map<number, { summary: string; status: string; model: string }>;
  onVersionClick: (id: number, shiftKey: boolean) => void;
  onDiff: (from: number, to: number) => void;
}) {
  const { t } = useTranslation();
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

  useEffect(() => { apiJSON<{ sources: { name: string; version_count: number }[] }>("/api/sources").then(
      (res) => setSources(res.sources.map((s) => s.name)), () => setSources([]));
  }, []);

  const fetchInlineContent = useCallback(async (vid: number) => {
    setLoadingContent(vid);
    try {
      const data = await apiJSON<RestoreResponse>(`/api/files/restore?path=${encodeURIComponent(filePath)}&version=${vid}`);
      setContentMap((prev) => new Map(prev).set(vid, data.content));
    } catch {
      setContentMap((prev) => { const n = new Map(prev); n.set(vid, ""); return n; });
    } finally { setLoadingContent(null); }
  }, [filePath]);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "g") { e.preventDefault(); versionJumpRef.current?.focus(); versionJumpRef.current?.select(); }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  const filtered = useMemo(() => {
    if (!sourceFilter && !actionFilter) return entries;
    return entries.filter((e) =>
      (!sourceFilter || e.source === sourceFilter) && (!actionFilter || e.action === actionFilter));
  }, [entries, sourceFilter, actionFilter]);
  const hasFilters = Boolean(sourceFilter || actionFilter);
  const handleVersionJump = useCallback(() => {
    const id = parseInt(versionJump, 10); if (isNaN(id)) return;
    const idx = filtered.findIndex((e) => e.versionId === id);
    if (idx >= 0 && listRef.current) listRef.current.scrollToRow({ index: idx, align: "start" });
    setVersionJump("");
  }, [versionJump, filtered]);
  const handleRowClick = useCallback((i: number) => {
    setExpandedId((prev) => (prev === filtered[i].versionId ? null : filtered[i].versionId));
  }, [filtered]);
  const handleCopyContent = useCallback(async (content: string) => {
    try { await navigator.clipboard.writeText(content); } catch {
      const ta = document.createElement("textarea"); ta.value = content;
      document.body.appendChild(ta); ta.select(); document.execCommand("copy"); document.body.removeChild(ta);
    }
  }, []);
  const ctxValue = useMemo(() => ({
    expandedId, setExpandedId, contentVersionId, setContentVersionId,
    contentMap, loadingContent, selectedIds, filePath, project, t, summaries,
  }), [expandedId, contentVersionId, contentMap, loadingContent, selectedIds, filePath, project, t, summaries]);

  return (
    <TimelineProvider value={ctxValue}>
      <div>
        <TimelineFilter
          sources={sources} sourceFilter={sourceFilter} actionFilter={actionFilter}
          hasFilters={hasFilters} filteredCount={filtered.length} totalCount={entries.length}
          versionJumpRef={versionJumpRef} versionJump={versionJump}
          onVersionJumpChange={setVersionJump} onVersionJumpSubmit={handleVersionJump}
          onSourceChange={(v) => { v ? searchParams.set("source", v) : searchParams.delete("source"); setSearchParams(searchParams); }}
          onActionChange={(v) => { v ? searchParams.set("action", v) : searchParams.delete("action"); setSearchParams(searchParams); }}
          onClear={() => { searchParams.delete("source"); searchParams.delete("action"); setSearchParams(searchParams); }}
        />
        <TimelineList
          filtered={filtered} onListRef={(ref) => { listRef.current = ref ?? null; }}
          onVersionClick={onVersionClick} onRowClick={handleRowClick} onDiff={onDiff}
          fetchInlineContent={fetchInlineContent} onCopyContent={handleCopyContent}
        />
        {selectedIds.length >= 2 && (
          <div className="sticky bottom-0 z-10 flex items-center justify-center gap-3 rounded-t-lg border-t border-gray-700 bg-gray-800/95 p-3 backdrop-blur">
            <span className="text-sm text-gray-400">v{selectedIds[0]} → v{selectedIds[1]}</span>
            <button onClick={() => {
              const [from, to] = selectedIds[0] < selectedIds[1] ? selectedIds : [selectedIds[1], selectedIds[0]];
              onDiff(from, to);
            }} className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-500">
              {t("timeline.view_diff")}
            </button>
          </div>
        )}
      </div>
    </TimelineProvider>
  );
}
