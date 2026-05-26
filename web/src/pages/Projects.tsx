import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiJSON } from "../api/client";
import { Project } from "../api/types";
import { toast } from "sonner";

export default function Projects() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [projects, setProjects] = useState<Project[]>([]);
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiJSON<{ projects: Project[] }>("/api/projects")
      .then((data) => setProjects(data.projects))
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : t("projects.failed_to_load"));
      })
      .finally(() => setLoading(false));
  }, []);

  const filtered = search.trim()
    ? projects.filter(
        (p) =>
          p.name.toLowerCase().includes(search.toLowerCase()) ||
          p.rootPath.toLowerCase().includes(search.toLowerCase())
      )
    : projects;

  if (loading) {
    return (
      <div className="space-y-3">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-20 animate-pulse rounded-lg bg-gray-800" />
        ))}
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 flex gap-3">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t("projects.search_placeholder")}
          className="flex-1 rounded border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-gray-100 placeholder-gray-400 focus:border-blue-500 focus:outline-none"
        />
      </div>

      {filtered.length === 0 ? (
        <p className="py-8 text-center text-sm text-gray-500">{t("projects.no_projects")}</p>
      ) : (
        <div className="space-y-2">
          {filtered.map((project) => (
            <button
              key={project.id}
              onClick={() =>
                navigate(`/projects/${encodeURIComponent(project.name)}/files`)
              }
              className="flex w-full items-center justify-between rounded-lg bg-gray-800 px-4 py-3 text-left hover:bg-gray-700"
            >
              <div>
                <div className="font-medium text-gray-100">{project.name}</div>
                <div className="mt-0.5 text-xs text-gray-400">{project.rootPath}</div>
                <div className="mt-0.5 text-xs text-gray-500">
                  {t("projects.created")}: {new Date(project.createdAt).toLocaleDateString()}
                </div>
              </div>
              <div className="flex-shrink-0 text-sm text-gray-400">
                {t("projects.files_count", { count: project.fileCount })}
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
