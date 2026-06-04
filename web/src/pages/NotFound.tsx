import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

export default function NotFound() {
  const navigate = useNavigate();
  const { t, ready } = useTranslation();
  if (!ready) return null;
  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <div className="text-center">
        <h2 className="text-lg font-bold text-gray-200">{t("notFound.title")}</h2>
        <p className="mt-2 text-sm text-gray-400">{t("notFound.message")}</p>
        <button onClick={() => navigate("/")} className="mt-4 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-500">
          {t("notFound.goHome")}
        </button>
      </div>
    </div>
  );
}
