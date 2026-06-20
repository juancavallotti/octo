/**
 * Browser-side helpers for the editor's BFF proxy routes under `/api`. Every
 * orchestrator client call goes through these so the orchestrator's URL stays a
 * server-only secret (see `app/api/orchestrator/client.ts`). Failures unwrap the
 * orchestrator's `{ error }` envelope.
 */

/** Perform a JSON request against a BFF route, unwrapping the `{ error }` envelope. */
export async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `request failed (${res.status})`);
  }
  // 204 No Content (delete / folder assignment) carries no body.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** Build a POST RequestInit with a JSON body. */
export function jsonBody(data: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  };
}
