const TOKEN_KEY = "changez_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export async function checkAuthRequired(): Promise<boolean> {
  try {
    const res = await fetch("/api/ui/auth-required");
    if (!res.ok) return false;
    const data = await res.json();
    return data.required === true;
  } catch {
    return false;
  }
}

export async function api(path: string, options?: RequestInit): Promise<Response> {
  const token = getToken();
  const baseHeaders: Record<string, string> = {
    "Content-Type": "application/json",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };

  const existingHeaders = options?.headers;
  let mergedHeaders: HeadersInit = baseHeaders;
  if (existingHeaders) {
    if (existingHeaders instanceof Headers) {
      const h = new Headers(existingHeaders);
      Object.entries(baseHeaders).forEach(([k, v]) => h.set(k, v));
      mergedHeaders = h;
    } else {
      mergedHeaders = { ...baseHeaders, ...(existingHeaders as Record<string, string>) };
    }
  }

  const res = await fetch(path, { ...options, headers: mergedHeaders });
  if (res.status === 401) {
    clearToken();
    window.dispatchEvent(new CustomEvent("auth-required"));
  }
  return res;
}

export async function apiJSON<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await api(path, options);
  if (!res.ok) {
    const errText = await res.text().catch(() => '');
    throw new Error(`API error: ${res.status} ${res.statusText}${errText ? ': ' + errText : ''}`);
  }
  const text = await res.text();
  if (!text) throw new Error("Empty response");
  return JSON.parse(text) as T;
}

export function encodeFilePath(path: string): string {
  return encodeURIComponent(encodeURIComponent(path));
}

export function getAISummaries(params: { project?: string; path?: string; since?: string; until?: string; limit?: number; offset?: number }) {
  const q = new URLSearchParams();
  if (params.project) q.set("project", params.project);
  if (params.path) q.set("path", params.path);
  if (params.since) q.set("since", params.since);
  if (params.until) q.set("until", params.until);
  if (params.limit) q.set("limit", String(params.limit));
  if (params.offset) q.set("offset", String(params.offset));
  return apiJSON<import("./types").SummaryResponse>(`/api/files/summary?${q}`);
}

export function refreshAISummary(params: { project?: string; path?: string; version?: number }) {
  const q = new URLSearchParams();
  if (params.project) q.set("project", params.project);
  if (params.path) q.set("path", params.path);
  if (params.version) q.set("version", String(params.version));
  return apiJSON<Record<string, unknown>>(`/api/files/summary/refresh?${q}`, { method: "POST" });
}

export function getAISession(params: { project?: string; sessionId: string }) {
  const q = new URLSearchParams();
  q.set("sessionId", params.sessionId);
  if (params.project) q.set("project", params.project);
  return apiJSON<import("./types").SessionResponse>(`/api/files/session?${q}`);
}

export function getAITrends(params: { project?: string; since?: string; until?: string; topFiles?: number }) {
  const q = new URLSearchParams();
  if (params.project) q.set("project", params.project);
  if (params.since) q.set("since", params.since);
  if (params.until) q.set("until", params.until);
  if (params.topFiles) q.set("topFiles", String(params.topFiles));
  return apiJSON<import("./types").TrendsResponse>(`/api/files/trends?${q}`);
}
