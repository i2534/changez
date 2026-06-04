import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { relativeTime, sourceColor, actionIcon } from "../utils";
import { ActivityItem } from "../api/types";

function groupConsecutive(items: ActivityItem[]): ActivityGroup[] {
  const groups: ActivityGroup[] = [];
  for (const item of items) {
    const key = `${item.projectName}::${item.filePath}`;
    const last = groups[groups.length - 1];
    if (last && last.key === key) {
      last.items.push(item);
      last.latest = item.timestamp;
      if (item.action !== last.firstAction) {
        last.actions.set(item.action, (last.actions.get(item.action) ?? 0) + 1);
      } else {
        last.actions.set(item.action, (last.actions.get(item.action) ?? 0) + 1);
      }
    } else {
      groups.push({
        key,
        projectName: item.projectName,
        filePath: item.filePath,
        source: item.source,
        items: [item],
        latest: item.timestamp,
        firstAction: item.action,
        actions: new Map([[item.action, 1]]),
      });
    }
  }
  return groups;
}

type ActivityGroup = {
  key: string;
  projectName: string;
  filePath: string;
  source: string;
  items: ActivityItem[];
  latest: string;
  firstAction: string;
  actions: Map<string, number>;
};

export default function ActivityFeed({
  items,
  onFileClick,
}: {
  items: ActivityItem[];
  onFileClick: (project: string, path: string) => void;
 }) {
  const { t } = useTranslation();
  const groups = useMemo(() => groupConsecutive(items), [items]);

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
        {groups.map((group) => {
          const count = group.items.length;
          const isMerged = count > 1;
          const actionSummary = getActionSummary(group.actions, count);
          return (
            <button
              key={group.key}
              onClick={() => onFileClick(group.projectName, group.filePath)}
              className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-left hover:bg-gray-700"
            >
              <span className={`flex h-2.5 w-2.5 flex-shrink-0 items-center justify-center rounded-sm ${sourceColor(group.source)}`} />
              <span className="flex-shrink-0 text-xs text-gray-500">
                {relativeTime(group.latest, t)}
              </span>
              <span
                className={`flex-shrink-0 rounded px-1.5 py-0.5 text-xs ${sourceColor(group.source)} text-white`}
              >
                {group.source}
              </span>
              <span className="flex-shrink-0 text-xs text-gray-400">
                {isMerged ? (
                  <span title={actionSummary.detail}>
                    {actionIcon(group.firstAction)} {actionSummary.short}
                  </span>
                ) : (
                  <>
                    {actionIcon(group.firstAction)} {group.firstAction}
                  </>
                )}
              </span>
              <span className="truncate text-sm text-gray-300">
                {group.filePath}
              </span>
              {isMerged && (
                <span className="flex-shrink-0 rounded-full bg-gray-600 px-1.5 py-0.5 text-xs text-gray-300">
                  ×{count}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}

function getActionSummary(actions: Map<string, number>, total: number): { short: string; detail: string } {
  const entries = [...actions.entries()];
  if (entries.length === 1) {
    const [action, count] = entries[0];
    return { short: `${count} ${action}s`, detail: `${count} ${action}s` };
  }
  const detail = entries.map(([a, c]) => `${c} ${a}`).join(", ");
  return { short: `${total} changes`, detail };
}
