import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { apiJSON } from "../api/client";
import { VersionResponse } from "../api/types";
import Timeline from "../components/Timeline";
import Skeleton from "../components/Skeleton";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function FileTimeline() {
  const { project, path } = useParams<{ project: string; path: string }>();
  const navigate = useNavigate();
  const [response, setResponse] = useState<VersionResponse | null>(null);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [lastClickedId, setLastClickedId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");
  const filePath = decodeURIComponent(path || "");

  useEffect(() => {
    if (!projectName || !filePath) return;
    const abort = new AbortController();
    setSelectedIds([]);
    setLastClickedId(null);
    setLoading(true);
    apiJSON<VersionResponse>(
      `/api/files/versions?path=${encodeURIComponent(filePath)}`, { signal: abort.signal }
    )
      .then(setResponse)
      .catch((err) => {
        if (abort.signal.aborted) return;
        toast.error(err instanceof Error ? err.message : t("timeline.failed_to_load"));
      })
      .finally(() => setLoading(false));
    return () => abort.abort();
  }, [projectName, filePath]);

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

  const handleDiff = (from: number, to: number) => {
    navigate(
      `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}/diff?from=${from}&to=${to}`
    );
  };

  if (!projectName || !filePath) {
    return <p className="py-8 text-center text-gray-500">Select a file to view history.</p>;
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
        onVersionClick={handleVersionClick}
        onDiff={handleDiff}
      />
    </div>
  );
}
