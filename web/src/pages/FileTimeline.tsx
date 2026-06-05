import { useState, useEffect, useMemo } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { apiJSON, getAISummaries } from "../api/client";
import { VersionResponse, SummaryResponse, DiffResponse } from "../api/types";
import Timeline from "../components/Timeline";
import Skeleton from "../components/Skeleton";
import DiffViewer from "../components/DiffViewer";
import CodeView from "../components/CodeView";
import { useFileContent } from "../hooks/useFileContent";
import { CloseIcon } from "../components/Icons";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function FileTimeline() {
  const params = useParams();
  const [searchParams] = useSearchParams();
  const project = params.project;
  const pathSegments = params["*"];
  const navigate = useNavigate();
  const [response, setResponse] = useState<VersionResponse | null>(null);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [lastClickedId, setLastClickedId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [summaries, setSummaries] = useState(new Map<number, { summary: string; status: string; model: string }>());
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");
  const rawPath = decodeURIComponent(pathSegments || "");

  const isDiff = useMemo(() => rawPath.endsWith("/diff"), [rawPath]);
  const filePath = useMemo(() => isDiff ? rawPath.slice(0, -5) : rawPath, [rawPath, isDiff]);

  const from = parseInt(searchParams.get("from") || "0", 10);
  const to = parseInt(searchParams.get("to") || "0", 10);

  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);
  const { content, fetchContent, clearContent } = useFileContent({
    filePath,
    corruptedKey: "diff.corrupted_view",
    failedKey: "diff.failed_to_load_content",
    t,
  });

  useEffect(() => {
    if (!projectName || !filePath) return;
    const abort = new AbortController();
    setSelectedIds([]);
    setLastClickedId(null);
    setLoading(true);
    setSummaries(new Map());
    apiJSON<VersionResponse>(
      `/api/files/versions?path=${encodeURIComponent(filePath)}`, { signal: abort.signal }
    )
      .then(setResponse)
      .catch((err) => {
        if (abort.signal.aborted) return;
        toast.error(err instanceof Error ? err.message : t("timeline.failed_to_load"));
      })
      .finally(() => setLoading(false));

    getAISummaries({ path: filePath })
      .then((res: SummaryResponse) => {
        if (abort.signal.aborted) return;
        const m = new Map<number, { summary: string; status: string; model: string }>();
        for (const s of res.summaries) {
          m.set(s.versionId, {
            summary: s.summary ?? "",
            status: s.summaryStatus ?? "",
            model: s.aiModel ?? "",
          });
        }
        setSummaries(m);
      })
      .catch(() => {});

    return () => abort.abort();
  }, [projectName, filePath]);

  useEffect(() => {
    if (!isDiff || !projectName || !filePath || !from || !to) return;
    const abort = new AbortController();
    setDiffLoading(true);
    setDiffData(null);
    apiJSON<DiffResponse>(
      `/api/files/diff?path=${encodeURIComponent(filePath)}&from=${from}&to=${to}`, { signal: abort.signal }
    )
      .then(setDiffData)
      .catch((err) => {
        if (abort.signal.aborted) return;
        toast.error(err instanceof Error ? err.message : t("diff.failed_to_load"));
      })
      .finally(() => setDiffLoading(false));
    return () => abort.abort();
  }, [projectName, filePath, from, to, isDiff]);

  useEffect(() => {
    if (!isDiff) {
      clearContent();
    }
  }, [isDiff]);

  const handleVersionClick = (id: number, shiftKey: boolean) => {
    setSelectedIds((prev) => {
      if (prev.includes(id)) {
        setLastClickedId(id);
        return prev.filter((v) => v !== id);
      }
      if (shiftKey && lastClickedId !== null) {
        const base = prev[0] ?? lastClickedId;
        const start = Math.min(base, id);
        const end = Math.max(base, id);
        setLastClickedId(id);
        return [start, end];
      }
      if (prev.length >= 2) {
        setLastClickedId(id);
        return [prev[1], id];
      }
      setLastClickedId(id);
      return [...prev, id];
    });
  };

  const handleDiff = (fromV: number, toV: number) => {
    navigate(
      `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}/diff?from=${fromV}&to=${toV}`
    );
  };

  const handleBack = () => {
    if (window.history.length <= 1) {
      navigate(`/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}`);
    } else {
      navigate(-1);
    }
  };

  if (!projectName || !filePath) {
    return <p className="py-8 text-center text-gray-500">Select a file to view history.</p>;
  }

  if (isDiff) {
    return (
      <div>
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <button
            onClick={handleBack}
            className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
          >
            {t("diff.back")}
          </button>
          <button
            onClick={() => fetchContent(from)}
            className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
          >
            {t("diff.view_content", { version: from })}
          </button>
          <button
            onClick={() => fetchContent(to)}
            className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
          >
            {t("diff.view_content", { version: to })}
          </button>
        </div>

        {content && (
          <div className="mb-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-sm text-gray-400">
                {t("diff.content_at_v", { version: content.version })}
              </span>
              <button
                onClick={() => clearContent()}
                className="text-xs text-gray-500 hover:text-gray-300"
              >
                <CloseIcon size={14} />
              </button>
            </div>
            <CodeView content={content.content} filePath={filePath} />
          </div>
        )}

        {diffLoading && <Skeleton variant="block" />}
        {diffData && (
          <DiffViewer diff={diffData.diff} fromVersion={from} toVersion={to} />
        )}
      </div>
    );
  }

  if (loading) {
    return (
      <div className="space-y-3">
        {[1, 2, 3, 4, 5].map((i) => (
          <Skeleton key={i} variant="timeline" />
        ))}
      </div>
    );
  }

  if (!response) {
    return <p className="py-8 text-center text-gray-500">{t("timeline.no_history")}</p>;
  }

  return (
    <div>
      <div className="mb-4 flex items-center gap-3 text-sm text-gray-400">
        <span className="font-medium text-gray-200">{filePath}</span>
        <span>·</span>
        <span>{t("timeline.versions_count", { count: response.totalVersions })}</span>
        <span>·</span>
        <span>{t("timeline.project_label")}: {projectName}</span>
      </div>

      <Timeline
        entries={response.versions}
        selectedIds={selectedIds}
        filePath={filePath}
        project={projectName}
        summaries={summaries}
        onVersionClick={handleVersionClick}
        onDiff={handleDiff}
      />
    </div>
  );
}
