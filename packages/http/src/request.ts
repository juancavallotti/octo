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
  try {
    return { ok: true, data: (await res.json()) as T };
  } catch {
    // A 2xx with a non-JSON body (e.g. a plain-text health probe). Surface it as
    // an error result rather than throwing — a thrown server-action error is
    // redacted in production. Use requestOk for endpoints that aren't JSON.
    return { ok: false, error: `invalid JSON response (${res.status})` };
  }
}

/**
 * Perform `method url` and report only whether it succeeded (2xx), without reading
 * the body. For liveness/health probes whose response may not be JSON. Never
 * throws — a network error is reported as `false`.
 */
export async function requestOk(method: string, url: string): Promise<boolean> {
  try {
    const res = await fetch(url, { method });
    return res.ok;
  } catch {
    return false;
  }
}
