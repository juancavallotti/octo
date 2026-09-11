/**
 * The transport every call in this folder goes through: the base URL, the two
 * request primitives, and the encoders paths are built with.
 *
 *     serverAction (auth) → a client module → call() → requestJson() → fetch
 *
 * It is the only place in the folder that knows about HTTP. The sibling modules
 * are lists of typed operations; none of them names a verb, a path shape, or the
 * server-only ORCHESTRATOR_URL.
 *
 * It is also the only place that attaches the caller's credential, for the same
 * reason: one place knows how to reach the orchestrator, so one place knows how
 * to speak to it as somebody. The operations keep their signatures — a token in a
 * hundred function signatures is a token in a hundred places it could be logged.
 */

import {
  requestBytes,
  requestJson,
  requestStream,
  sendBytes,
  type ActionResult,
  type RequestOptions,
} from "@octo/http";
import { callerToken } from "@/app/auth/callerToken";

export type { ActionResult } from "@octo/http";

export const enc = encodeURIComponent;

/**
 * Encode an object key for the `{key...}` path wildcard: keys may contain slashes
 * (which must stay real path separators), so encode each segment but keep the
 * slashes between them.
 */
export const encKey = (key: string): string =>
  key.split("/").map(enc).join("/");

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
export function baseUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/**
 * Issue one orchestrator request. Internal: the public API is the named domain
 * functions below, never a verb. Returns an error result when the orchestrator is
 * unconfigured (mirroring the route proxy's 503).
 */
export async function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = baseUrl();
  if (!base) return unconfigured();
  return requestJson<T>(method, `${base}${path}`, body, await authorized());
}

/**
 * Issue one orchestrator request whose response is a live stream rather than a document
 * — today only the dev-run log follow. Internal, like {@link call}, and reporting the
 * same error result when the orchestrator is unconfigured.
 */
export async function callStream(
  method: string,
  path: string,
  signal?: AbortSignal,
): Promise<ActionResult<ReadableStream<Uint8Array>>> {
  const base = baseUrl();
  if (!base) return unconfigured();
  return requestStream(method, `${base}${path}`, {
    ...(await authorized()),
    signal,
  });
}

/**
 * Issue one orchestrator request and hand back the raw Response.
 *
 * For the proxies: a route handler that streams the orchestrator's answer
 * straight to the browser needs the headers and the body untouched, which the
 * helpers above deliberately do not give it. It exists so those routes do not
 * have to build a URL and attach a credential themselves — the two things this
 * module is here to be the only place for.
 *
 * Null when the orchestrator is unconfigured, which the caller reports as a 503.
 */
export async function callRaw(
  path: string,
  init?: RequestInit,
): Promise<Response | null> {
  const base = baseUrl();
  if (!base) return null;
  const auth = await authorized();
  return fetch(`${base}${path}`, {
    ...init,
    headers: { ...(init?.headers ?? {}), ...(auth?.headers ?? {}) },
  });
}

/**
 * Issue one orchestrator request whose response is an opaque document rather than
 * JSON — a bundle download, a frozen resource's raw bytes. Internal, like
 * {@link call}, and reporting the same error result when the orchestrator is
 * unconfigured.
 */
export async function callBytes(
  method: string,
  path: string,
): Promise<ActionResult<Uint8Array>> {
  const base = baseUrl();
  if (!base) return unconfigured();
  return requestBytes(method, `${base}${path}`, await authorized());
}

/**
 * Issue one orchestrator request whose *body* is an opaque document and whose
 * reply is JSON — the bundle uploads. Internal, like {@link call}.
 */
export async function callWithBytes<T>(
  method: string,
  path: string,
  body: Uint8Array,
  contentType: string,
): Promise<ActionResult<T>> {
  const base = baseUrl();
  if (!base) return unconfigured();
  return sendBytes<T>(
    method,
    `${base}${path}`,
    body,
    contentType,
    await authorized(),
  );
}

/** The error result every call reports when ORCHESTRATOR_URL is unset. */
function unconfigured(): { ok: false; error: string } {
  return {
    ok: false,
    error: "orchestrator not configured (ORCHESTRATOR_URL unset)",
  };
}

/**
 * The caller's credential, as request options.
 *
 * Absent when there is none — an install with no identity provider, or a call
 * made outside any request. That is not an error here: the orchestrator decides
 * what it will do without one, and an install that is not enforcing will do it
 * happily.
 */
async function authorized(): Promise<RequestOptions | undefined> {
  const token = await callerToken();
  return token ? { headers: { Authorization: `Bearer ${token}` } } : undefined;
}
