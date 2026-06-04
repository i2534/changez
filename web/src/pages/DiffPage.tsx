import { useState, useEffect } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { apiJSON } from "../api/client";
import { DiffResponse } from "../api/types";
import DiffViewer from "../components/DiffViewer";
import CodeView from "../components/CodeView";
import { useFileContent } from "../hooks/useFileContent";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function DiffPage() {
  const { project, path } = useParams<{ project: string; path: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");
  const filePath = decodeURIComponent(path || "");
  const from = parseInt(searchParams.get("from") || "0", 10);
  const to = parseInt(searchParams.get("to") || "0", 10);

  const { content, fetchContent, clearContent } = useFileContent({
    filePath,
    corruptedKey: "diff.corrupted_view",
    failedKey: "diff.failed_to_load_content",
    t,
  });

  useEffect(() => {
    if (!projectName || !filePath || !from || !to) return;
    const abort = new AbortController();
    clearContent();
    setLoading(true);
    apiJSON<DiffResponse>(
      `/api/files/diff?path=${encodeURIComponent(filePath)}&from=${from}&to=${to}`, { signal: abort.signal }
    )
      .then(setDiffData)
      .catch((err) => {
        if (abort.signal.aborted) return;
        toast.error(err instanceof Error ? err.message : t("diff.failed_to_load"));
      })
      .finally(() => setLoading(false));
    return () => abort.abort();
  }, [projectName, filePath, from, to]);

  if (!from || !to) {
    return <p className="py-8 text-center text-gray-500">Select two versions to compare.</p>;
  }

  if (loading) {
    return (
      <div className="h-64 animate-pulse rounded-lg bg-gray-800" />
    );
  }

  const handleBack = () => {
    if (window.history.length <= 1) {
      navigate(`/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}`);
    } else {
      navigate(-1);
    }
  };

  return (
    <div>
      <nav className="mb-3 flex flex-wrap items-center gap-1 text-sm text-gray-400">
        <button
          onClick={handleBack}
          className="hover:text-gray-200"
        >
          {t("diff.back")}
        </button>
        <span>/</span>
        <button
          onClick={() => navigate(`/projects/${encodeURIComponent(projectName)}`)}
          className="hover:text-gray-200"
        >
          {projectName}
        </button>
        <span>/</span>
        <button
          onClick={() => navigate(`/projects/${encodeURIComponent(projectName)}/files`)}
          className="hover:text-gray-200"
        >
          {t("layout.files")}
        </button>
        <span>/</span>
        <button
          onClick={() => navigate(`/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePath)}`)}
          className="hover:text-gray-200"
        >
          {filePath}
        </button>
        <span>/</span>
        <span className="font-medium text-gray-200">
          {t("layout.diff")} (v{from} → v{to})
        </span>
      </nav>

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
        </>
      )}
    </div>
  );
}
