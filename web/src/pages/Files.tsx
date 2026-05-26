import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useNavigate, useSearchParams } from "react-router-dom";
import { apiJSON } from "../api/client";
import { File } from "../api/types";
import FileList from "../components/FileList";
import { toast } from "sonner";

export default function Files() {
  const { project } = useParams<{ project: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [files, setFiles] = useState<File[]>([]);
  const [search, setSearch] = useState(searchParams.get("filter") || "");
  const [loading, setLoading] = useState(true);
  const { t } = useTranslation();

  const projectName = decodeURIComponent(project || "");

  // Sync URL filter param to search input when navigating between directory breadcrumbs
  useEffect(() => {
    setSearch(searchParams.get("filter") || "");
  }, [searchParams]);

  useEffect(() => {
    if (!projectName) return;
    apiJSON<{ files: File[] }>(`/api/files?project=${encodeURIComponent(projectName)}`)
      .then((data) => setFiles(data.files))
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : t("files.failed_to_load"));
      })
      .finally(() => setLoading(false));
  }, [projectName]);

  if (loading) {
    return (
      <div className="space-y-2">
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="h-10 animate-pulse rounded bg-gray-800" />
        ))}
      </div>
    );
  }

  const filterPrefix = searchParams.get("filter") || "";

  return (
    <div>
      <div className="mb-2 text-sm text-gray-400">
        {t("files.project_label")}: <span className="text-gray-200">{projectName}</span>
        {filterPrefix && (
          <>
            {" / "}<span className="text-gray-300">{filterPrefix.replace(/\/$/, "")}</span>
          </>
        )}
        {" · "}
        <span>{t("files.files_count", { count: files.length })}</span>
      </div>
      <FileList
        files={files}
        onFileClick={(path) =>
          navigate(
            `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(path)}`
          )
        }
        searchQuery={search}
        onSearchChange={setSearch}
      />
    </div>
  );
}
