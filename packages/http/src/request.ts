import type { ActionResult } from "./result";

/**
 * Perform `method url` (JSON-encoding `body` when present) and adapt the response
 * to an {@link ActionResult}, unwrapping a `{ error }` envelope on failure. Never
 * throws: a network error becomes an error result.
 *
 * This is the framework-agnostic abstraction over `fetch`; callers pass a full URL
 * and build their own typed, domain-oriented client on top.
 */
export async function requestJson<T>(
  method: string,
  url: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(url, init);
  } catch (err) {
    return { ok: false, error: `request failed: ${(err as Error).message}` };
  }

  if (!res.ok) {
    const errorBody = await res.json().catch(() => ({}));
    return {
      ok: false,
      error: errorBody.error ?? `request failed (${res.status})`,
    };
  }
  // 204 No Content carries no body.
  if (res.status === 204) return { ok: true, data: undefined as T };
  return { ok: true, data: (await res.json()) as T };
}
