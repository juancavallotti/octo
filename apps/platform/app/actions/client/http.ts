/**
 * The transport every call in this folder goes through: the base URL, the two
 * request primitives, and the encoders paths are built with.
 *
 *     serverAction (auth) → a client module → call() → requestJson() → fetch
 *
 * It is the only place in the folder that knows about HTTP. The sibling modules
 * are lists of typed operations; none of them names a verb, a path shape, or the
 * server-only ORCHESTRATOR_URL.
 */

import { requestJson, requestStream, type ActionResult } from "@octo/http";

export type { ActionResult } from "@octo/http";

export const enc = encodeURIComponent;

/**
 * Encode an object key for the `{key...}` path wildcard: keys may contain slashes
 * (which must stay real path separators), so encode each segment but keep the
 * slashes between them.
 */
export const encKey = (key: string): string => key.split("/").map(enc).join("/");

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
export function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/**
 * Issue one orchestrator request. Internal: the public API is the named domain
 * functions below, never a verb. Returns an error result when the orchestrator is
 * unconfigured (mirroring the route proxy's 503).
 */
export function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = baseUrl();
  if (!base) {
    return Promise.resolve({
      ok: false,
      error: "orchestrator not configured (ORCHESTRATOR_URL unset)",
    });
  }
  return requestJson<T>(method, `${base}${path}`, body);
}

/**
 * Issue one orchestrator request whose response is a live stream rather than a document
 * — today only the dev-run log follow. Internal, like {@link call}, and reporting the
 * same error result when the orchestrator is unconfigured.
 */
export function callStream(
  method: string,
  path: string,
  signal?: AbortSignal,
): Promise<ActionResult<ReadableStream<Uint8Array>>> {
  const base = baseUrl();
  if (!base) {
    return Promise.resolve({
      ok: false,
      error: "orchestrator not configured (ORCHESTRATOR_URL unset)",
    });
  }
  return requestStream(method, `${base}${path}`, { signal });
}
