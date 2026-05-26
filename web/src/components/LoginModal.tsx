import { useState } from "react";
import { useTranslation } from "react-i18next";
import { setToken, api } from "../api/client";

export default function LoginModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [token, setTokenInput] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const { t } = useTranslation();

  if (!open) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token.trim()) return;
    setLoading(true);
    setError("");
    setToken(token.trim());
    try {
      const res = await api("/health");
      if (res.ok) {
        setTokenInput("");
        setError("");
        onClose();
      } else {
        setError(t("login.invalid_token"));
      }
    } catch {
      setError(t("login.cannot_connect"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-lg bg-gray-800 p-6 shadow-xl">
        <h2 className="mb-4 text-xl font-bold text-gray-100">{t("login.title")}</h2>
        <p className="mb-4 text-sm text-gray-400">
          {t("login.description")}
        </p>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            value={token}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder={t("login.placeholder")}
            className="w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-gray-100 placeholder-gray-400 focus:border-blue-500 focus:outline-none"
            autoFocus
          />
          {error && <p className="mt-2 text-sm text-red-400">{error}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded px-3 py-1.5 text-sm text-gray-300 hover:bg-gray-700"
            >
              {t("login.cancel")}
            </button>
            <button
              type="submit"
              disabled={loading || !token.trim()}
              className="rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-500 disabled:opacity-50"
            >
              {loading ? t("login.verifying") : t("login.connect")}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
