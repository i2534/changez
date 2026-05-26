import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { checkAuthRequired, getToken, clearToken } from "../api/client";
import LoginModal from "./LoginModal";

export default function Layout({ children }: { children: React.ReactNode }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const [loginOpen, setLoginOpen] = useState(false);

  useEffect(() => {
    checkAuthRequired().then((required) => {
      if (required && !getToken()) setLoginOpen(true);
    });
  }, []);

  useEffect(() => {
    const handler = () => setLoginOpen(true);
    window.addEventListener("auth-required", handler);
    return () => window.removeEventListener("auth-required", handler);
  }, []);

  const pathBreadcrumbs = getBreadcrumbs(location.pathname, t, location.search);

  const toggleLang = () => {
    const next = i18n.language === "zh" ? "en" : "zh";
    i18n.changeLanguage(next);
  };

  return (
    <div className="min-h-screen bg-gray-900 text-gray-100">
      <nav className="border-b border-gray-700 bg-gray-800 px-4 py-3">
        <div className="mx-auto max-w-7xl flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={() => navigate("/")}
              className="text-xl font-bold text-blue-400 hover:text-blue-300"
            >
              {t("layout.app_name")}
            </button>
            <nav className="flex items-center gap-2 text-sm text-gray-400">
              {pathBreadcrumbs.map((crumb, i) => (
                <span key={crumb.path} className="flex items-center gap-2">
                  {i > 0 && <span>/</span>}
                  {crumb.path === "" || i === pathBreadcrumbs.length - 1 ? (
                    <span className="text-gray-200">{crumb.label}</span>
                  ) : (
                    <button
                      onClick={() => navigate(crumb.path)}
                      className="hover:text-gray-100"
                    >
                      {crumb.label}
                    </button>
                  )}
                </span>
              ))}
            </nav>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={toggleLang}
              className="text-sm text-gray-400 hover:text-gray-200"
            >
              {i18n.language === "zh" ? "EN" : "中文"}
            </button>
            <button
              onClick={() => {
                clearToken();
                setLoginOpen(true);
              }}
              className="text-sm text-gray-400 hover:text-gray-200"
            >
              {t("layout.change_token")}
            </button>
          </div>
        </div>
      </nav>
      <main className="mx-auto max-w-7xl px-4 py-6">{children}</main>
      <LoginModal open={loginOpen} onClose={() => setLoginOpen(false)} />
    </div>
  );
}

function getBreadcrumbs(pathname: string, t: (key: string) => string, search?: string): { path: string; label: string }[] {
  const parts = pathname.split("/").filter(Boolean);
  const crumbs: { path: string; label: string }[] = [
    { path: "/", label: t("layout.dashboard") },
  ];

  let accumulated = "";
  let projectName = "";
  for (let i = 0; i < parts.length; i++) {
    const part = decodeURIComponent(parts[i]);
    accumulated += `/${part}`;

    if (accumulated === "/projects") {
      crumbs.push({ path: accumulated, label: t("layout.projects") });
    } else if (i > 0 && parts[i - 1] === "projects") {
      projectName = part;
      crumbs.push({ path: accumulated + "/files", label: part });
    } else if (parts[i] === "files" && i > 1) {
      crumbs.push({ path: accumulated, label: t("layout.files") });
    } else if (i === parts.length - 1 && parts[i] !== "diff") {
      const segments = part.split("/");
      let filePathAccumulated = "";
      for (let j = 0; j < segments.length; j++) {
        filePathAccumulated += (j > 0 ? "/" : "") + segments[j];
        const isFile = j === segments.length - 1;
        const crumbPath = isFile
          ? `/projects/${encodeURIComponent(projectName)}/files/${encodeURIComponent(filePathAccumulated)}`
          : `/projects/${encodeURIComponent(projectName)}/files?filter=${encodeURIComponent(filePathAccumulated + "/")}`;
        crumbs.push({ path: crumbPath, label: segments[j] });
      }
    } else if (parts[i] === "diff") {
      crumbs.push({ path: accumulated, label: t("layout.diff") });
    }
  }

  // When on Files page with filter param, show directory breadcrumbs
  if (search && parts.includes("files") && !parts.includes("diff")) {
    const params = new URLSearchParams(search);
    const filter = params.get("filter");
    if (filter) {
      const filterSegments = filter.replace(/\/$/, "").split("/");
      let filterAccumulated = "";
      for (let j = 0; j < filterSegments.length; j++) {
        filterAccumulated += (j > 0 ? "/" : "") + filterSegments[j];
        const crumbPath = `/projects/${encodeURIComponent(projectName)}/files?filter=${encodeURIComponent(filterAccumulated + "/")}`;
        crumbs.push({ path: crumbPath, label: filterSegments[j] });
      }
    }
  }

  return crumbs;
}
