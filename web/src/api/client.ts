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
  if (!text) return {} as T;
  return JSON.parse(text) as T;
}

export function encodeFilePath(path: string): string {
  return encodeURIComponent(encodeURIComponent(path));
}
