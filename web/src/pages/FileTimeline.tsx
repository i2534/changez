import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { apiJSON } from "../api/client";
import { VersionResponse, RestoreResponse } from "../api/types";
import Timeline from "../components/Timeline";
import CodeView from "../components/CodeView";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function FileTimeline() {
  const { project, path } = useParams<{ project: string; path: string }>();
  const navigate = useNavigate();
  const [response, setResponse] = useState<VersionResponse | null>(null);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [loading, setLoading] = useState(true);
   const [content, setContent] = useState<{ version: number; content: string } | null>(null);
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");
  const filePath = decodeURIComponent(path || "");

  useEffect(() => {
    if (!projectName || !filePath) return;
    setContent(null);
    setSelectedIds([]);
    setLoading(true);
    apiJSON<VersionResponse>(
      `/api/files/versions?path=${encodeURIComponent(filePath)}`
    )
      .then(setResponse)
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : t("timeline.failed_to_load"));
      })
      .finally(() => setLoading(false));
  }, [projectName, filePath]);

  const handleVersionClick = (id: number) => {
    setSelectedIds((prev) => {
      if (prev.includes(id)) {
        return prev.filter((v) => v !== id);
      }
      if (prev.length >= 2) {
        return [prev[1], id];
      }
      return [...prev, id];
    });
  };

  const handleDiff = (from: number, to: number) => {
    navigate(
      `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}/diff?from=${from}&to=${to}`
    );
  };

  const handleViewContent = async (versionId: number) => {
    try {
      const data = await apiJSON<RestoreResponse>(
        `/api/files/restore?path=${encodeURIComponent(filePath)}&version=${versionId}`
      );
      setContent({ version: data.version, content: data.content });
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("timeline.failed_to_load_content");
      if (msg.includes("CORRUPTED_DATA") || msg.includes("base_id")) {
        toast.error(t("timeline.corrupted_restore", { version: versionId }));
      } else {
        toast.error(msg);
      }
    }
  };

  if (loading) {
    return (
      <div className="space-y-3">
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded-lg bg-gray-800" />
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

      {content && (
        <div className="mb-4">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm text-gray-400">
              {t("timeline.content_at_v", { version: content.version })}
            </span>
            <button
              onClick={() => setContent(null)}
              className="text-xs text-gray-500 hover:text-gray-300"
            >
              ✕
            </button>
          </div>
          <CodeView content={content.content} filePath={filePath} />
        </div>
      )}

      <Timeline
        entries={response.versions}
        selectedIds={selectedIds}
        onVersionClick={handleVersionClick}
        onDiff={handleDiff}
      />

      {selectedIds.length === 1 && (
        <div className="mt-3 flex gap-2">
          <button
            onClick={() => handleViewContent(selectedIds[0])}
            className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
          >
            {t("timeline.view_content", { version: selectedIds[0] })}
          </button>
          <button
            onClick={() => {
              apiJSON<RestoreResponse>(
                `/api/files/restore?path=${encodeURIComponent(filePath)}&version=${selectedIds[0]}`
              ).then((data) => {
                  navigator.clipboard.writeText(data.content).then(() => {
                    toast.success(t("timeline.copied"));
                  });
              });
            }}
            className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
          >
            {t("timeline.copy_clipboard", { version: selectedIds[0] })}
          </button>
        </div>
      )}
    </div>
  );
}
