export interface Stats {
  projects: number;
  files: number;
  versions: number;
  sources: Record<string, number>;
}

export interface Project {
  id: number;
  name: string;
  rootPath: string;
  extra: string;
  createdAt: string;
  fileCount: number;
}

export interface File {
  project: string;
  path: string;
  latestVersionId: number | null;
  createdAt: string;
}

export interface VersionEntry {
  versionId: number;
  timestamp: string;
  source: string;
  action: string;
  sessionId?: string;
  model?: string;
  message?: string;
}

export interface VersionResponse {
  file: string;
  project: string;
  totalVersions: number;
  versions: VersionEntry[];
}

export interface DiffResponse {
  path: string;
  from: number;
  to: number;
  diff: string;
}

export interface RestoreResponse {
  path: string;
  version: number;
  timestamp: string;
  content: string;
}

export interface ActivityItem {
  fileId: number;
  filePath: string;
  projectName: string;
  projectId: number;
  versionId: number;
  action: string;
  source: string;
  timestamp: string;
}

export interface ActivityResponse {
  activity: ActivityItem[];
}

// AI Summary types
export interface SummaryEntry {
  versionId: number;
  action: string;
  timestamp: string;
  source: string;
  summary?: string;
  summaryStatus?: string;
  aiModel?: string;
}

export interface SummaryResponse {
  summaries: SummaryEntry[];
}

// AI Session types
export interface SessionChangeEntry {
  filePath: string;
  projectName: string;
  action: string;
  message?: string;
  timestamp: string;
}

export interface SessionResponse {
  sessionId: string;
  status: string;
  summary?: string;
  model?: string;
  changes: SessionChangeEntry[];
}

// AI Trends types
export interface FileChangeCount {
  filePath: string;
  count: number;
}

export interface TrendsResponse {
  period: string;
  totalFiles: number;
  totalChanges: number;
  sourceBreakdown: Record<string, number>;
  topFiles: FileChangeCount[];
  status?: string;
  summary?: string;
  model?: string;
}
