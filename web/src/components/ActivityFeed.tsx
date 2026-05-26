import { useTranslation } from "react-i18next";
import { relativeTime, sourceColor, actionIcon } from "../utils";
import { ActivityItem } from "../api/types";

export default function ActivityFeed({
  items,
  onFileClick,
}: {
  items: ActivityItem[];
  onFileClick: (project: string, path: string) => void;
 }) {
  const { t } = useTranslation();
  if (items.length === 0) {
    return (
      <div className="rounded-lg bg-gray-800 p-5">
        <h3 className="mb-2 text-sm font-semibold text-gray-300">{t("dashboard.recent_activity")}</h3>
        <p className="text-sm text-gray-500">{t("dashboard.no_recent_activity")}</p>
      </div>
    );
  }

  return (
    <div className="rounded-lg bg-gray-800 p-5">
      <h3 className="mb-3 text-sm font-semibold text-gray-300">{t("dashboard.recent_activity")}</h3>
      <div className="space-y-1">
        {items.map((item) => (
          <button
            key={`${item.fileId}-${item.versionId}`}
            onClick={() =>
              onFileClick(item.projectName, item.filePath)
            }
            className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-left hover:bg-gray-700"
          >
            <span className={`flex h-2.5 w-2.5 flex-shrink-0 items-center justify-center rounded-sm ${sourceColor(item.source)}`} />
            <span className="flex-shrink-0 text-xs text-gray-500">
              {relativeTime(item.timestamp, t)}
            </span>
            <span
              className={`flex-shrink-0 rounded px-1.5 py-0.5 text-xs ${sourceColor(item.source)} text-white`}
            >
              {item.source}
            </span>
            <span className="flex-shrink-0 text-xs text-gray-400">
              {actionIcon(item.action)} {item.action}
            </span>
            <span className="truncate text-sm text-gray-300">
              {item.filePath}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
