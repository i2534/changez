import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { apiJSON } from "../api/client";
import { Stats, ActivityResponse } from "../api/types";
import StatCard from "../components/StatCard";
import SourceBar from "../components/SourceBar";
import ActivityFeed from "../components/ActivityFeed";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export default function Dashboard() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const [stats, setStats] = useState<Stats | null>(null);
  const [activity, setActivity] = useState<ActivityResponse["activity"]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      apiJSON<Stats>("/api/stats"),
      apiJSON<ActivityResponse>("/api/recent-activity?limit=20"),
    ])
      .then(([statsData, activityData]) => {
        setStats(statsData);
        setActivity(activityData.activity);
      })
      .catch((err) => {
        toast.error(err instanceof Error ? err.message : t("dashboard.failed_to_load"));
      })
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return <LoadingSkeleton />;
  }

  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          label={t("dashboard.projects")}
          value={stats?.projects ?? 0}
          onClick={() => navigate("/projects")}
        />
        <StatCard label={t("dashboard.files")} value={stats?.files ?? 0} />
        <StatCard label={t("dashboard.versions")} value={stats?.versions ?? 0} />
      </div>

      {stats?.sources && <SourceBar sources={stats.sources} />}

      <ActivityFeed
        items={activity}
        onFileClick={(project, path) =>
          navigate(`/projects/${encodeURIComponent(project)}/files/${encodeURIComponent(path)}`)
        }
      />
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-3">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-24 animate-pulse rounded-lg bg-gray-800" />
        ))}
      </div>
      <div className="h-40 animate-pulse rounded-lg bg-gray-800" />
      <div className="h-64 animate-pulse rounded-lg bg-gray-800" />
    </div>
  );
}
