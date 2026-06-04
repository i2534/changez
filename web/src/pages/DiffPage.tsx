import { useState, useEffect } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { apiJSON } from "../api/client";
import { DiffResponse } from "../api/types";
import DiffViewer from "../components/DiffViewer";
import CodeView from "../components/CodeView";
import Skeleton from "../components/Skeleton";
import { useFileContent } from "../hooks/useFileContent";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { CloseIcon } from "../components/Icons";

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
    return <Skeleton variant="block" />;
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

     {diffData && (
        <DiffViewer diff={diffData.diff} fromVersion={from} toVersion={to} />
      )}
    </div>
  );
}
