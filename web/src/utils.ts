export function relativeTime(dateStr: string, t?: (key: string, vars?: any) => string): string {
  try {
    const date = new Date(dateStr);
    if (isNaN(date.getTime())) return dateStr;
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    if (diffMs < 0) return date.toLocaleDateString();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);

    if (diffSec < 60) return t ? t("time.sec_ago", { count: diffSec }) : `${diffSec} sec ago`;
    if (diffMin < 60) return t ? t("time.min_ago", { count: diffMin }) : `${diffMin} min ago`;
    if (diffHour < 24) return t ? t("time.hr_ago", { count: diffHour }) : `${diffHour} hr ago`;
    if (diffDay < 30) return t ? t("time.day_ago", { count: diffDay }) : `${diffDay} day ago`;
    return date.toLocaleDateString();
  } catch {
    return dateStr;
  }
}

export function sourceColor(source: string): string {
  const map: Record<string, string> = {
    opencode: "bg-blue-500",
    "claude-code": "bg-green-500",
    cursor: "bg-yellow-500",
    human: "bg-gray-400",
  };
  return map[source] || "bg-gray-500";
}

export function actionIcon(action: string): string {
  if (action === "delete") return "■";
  return "●";
}
