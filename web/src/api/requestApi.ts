import { state } from "../core/state";

export async function requestApi<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(opts.headers as Record<string, string> | undefined),
  };
  if (state.token) headers.Authorization = "Bearer " + state.token;
  const res = await fetch(path, { ...opts, headers });
  const text = await res.text();
  let body: { error?: string } | null = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = { error: text };
  }
  if (!res.ok) throw new Error((body && body.error) || res.statusText);
  return body as T;
}
