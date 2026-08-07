import type { ActionResult } from "./result";

/**
 * Pull an `{ error }` message out of a parsed failure body, falling back to the status.
 *
 * `res.json()` resolves to whatever the body decoded to — `null`, a number, an array —
 * not necessarily an object, so reading `.error` off it directly would throw on a literal
 * `null` body, and this module promises never to throw.
 */
function errorMessage(body: unknown, status: number): string {
  if (body !== null && typeof body === "object" && "error" in body) {
    const err = (body as { error?: unknown }).error;
    if (typeof err === "string") return err;
  }
  return `request failed (${status})`;
}

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
    const errorBody: unknown = await res.json().catch(() => null);
    return { ok: false, error: errorMessage(errorBody, res.status) };
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
 * Perform `method url` and hand back the response body as a byte stream, without
 * reading it.
 *
 * For a response that is a live stream rather than a document — a log follow, an
 * event stream — where {@link requestJson} would buffer until the far end closed,
 * which for a follow is never. The caller owns the stream and must consume or cancel
 * it; pass `signal` so an abandoned one is actually cancelled rather than left open
 * holding a socket.
 *
 * Never throws, like requestJson: a network error or a non-2xx becomes an error
 * result, and a failure body carrying the usual `{ error }` envelope is unwrapped.
 */
export async function requestStream(
  method: string,
  url: string,
  opts?: { signal?: AbortSignal },
): Promise<ActionResult<ReadableStream<Uint8Array>>> {
  let res: Response;
  try {
    res = await fetch(url, { method, signal: opts?.signal });
  } catch (err) {
    return { ok: false, error: `request failed: ${(err as Error).message}` };
  }

  if (!res.ok) {
    const errorBody: unknown = await res.json().catch(() => null);
    return { ok: false, error: errorMessage(errorBody, res.status) };
  }
  if (!res.body) {
    // A 2xx with no body at all (a 204, a HEAD). Reported as an error rather than
    // handed back as an empty stream, which a caller would read as a source that had
    // simply gone quiet and wait on forever.
    return { ok: false, error: `no response body (${res.status})` };
  }
  return { ok: true, data: res.body };
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
