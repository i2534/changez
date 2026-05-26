import { useTranslation } from "react-i18next";
import { sourceColor } from "../utils";

export default function SourceBar({
  sources,
}: {
  sources: Record<string, number>;
}) {
  const { t } = useTranslation();
  const total = Object.values(sources).reduce((a, b) => a + b, 0);
  if (total === 0) return null;

  const sorted = Object.entries(sources).sort((a, b) => b[1] - a[1]);

  return (
    <div className="rounded-lg bg-gray-800 p-5">
      <h3 className="mb-3 text-sm font-semibold text-gray-300">{t("dashboard.change_sources")}</h3>
      <div className="space-y-2">
        {sorted.map(([name, count]) => {
          const pct = Math.round((count / total) * 100);
          return (
            <div key={name} className="flex items-center gap-3">
              <span className="w-24 truncate text-right text-sm text-gray-400">
                {name}
              </span>
              <div className="flex h-5 flex-1 overflow-hidden rounded bg-gray-700">
                <div
                  className={`h-full ${sourceColor(name)} transition-all`}
                  style={{ width: `${pct}%` }}
                />
              </div>
              <span className="w-16 text-sm text-gray-300">
                {count.toLocaleString()}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
