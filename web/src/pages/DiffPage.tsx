import { useState, useEffect } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { apiJSON } from "../api/client";
import { DiffResponse, RestoreResponse } from "../api/types";
import DiffViewer from "../components/DiffViewer";
import CodeView from "../components/CodeView";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function DiffPage() {
  const { project, path } = useParams<{ project: string; path: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [content, setContent] = useState<{ version: number; content: string } | null>(null);
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");
  const filePath = decodeURIComponent(path || "");
  const from = parseInt(searchParams.get("from") || "0", 10);
  const to = parseInt(searchParams.get("to") || "0", 10);

  useEffect(() => {
    if (!projectName || !filePath || !from || !to) return;
    setContent(null);
    setLoading(true);
    apiJSON<DiffResponse>(
      `/api/files/diff?path=${encodeURIComponent(filePath)}&from=${from}&to=${to}`
    )
      .then(setDiffData)
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : t("diff.failed_to_load"));
      })
      .finally(() => setLoading(false));
  }, [projectName, filePath, from, to]);

  const handleViewContent = async (versionId: number) => {
    try {
      const data = await apiJSON<RestoreResponse>(
        `/api/files/restore?path=${encodeURIComponent(filePath)}&version=${versionId}`
      );
      setContent({ version: data.version, content: data.content });
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("diff.failed_to_load_content");
      if (msg.includes("CORRUPTED_DATA") || msg.includes("base_id")) {
        toast.error(t("diff.corrupted_view", { version: versionId }));
      } else {
        toast.error(msg);
      }
    }
  };

  if (loading) {
    return (
      <div className="h-64 animate-pulse rounded-lg bg-gray-800" />
    );
  }

  return (
    <div>
      <div className="mb-3 flex items-center gap-3 text-sm text-gray-400">
        <button
          onClick={() => navigate(-1)}
          className="hover:text-gray-200"
        >
           {t("diff.back")}
         </button>
        <span className="font-medium text-gray-200">{filePath}</span>
        <span>·</span>
        <span>v{from} → v{to}</span>
      </div>

      {content && (
        <div className="mb-4">
          <div className="mb-2 flex items-center justify-between">
           <span className="text-sm text-gray-400">
               {t("diff.content_at_v", { version: content.version })}
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

      {diffData && (
        <>
          <DiffViewer diff={diffData.diff} fromVersion={from} toVersion={to} />
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => handleViewContent(from)}
              className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
         >
               {t("diff.view_content", { version: from })}
             </button>
            <button
              onClick={() => handleViewContent(to)}
              className="rounded bg-gray-700 px-3 py-1.5 text-sm text-gray-200 hover:bg-gray-600"
        >
               {t("diff.view_content", { version: to })}
             </button>
          </div>
        </>
      )}
    </div>
  );
}
