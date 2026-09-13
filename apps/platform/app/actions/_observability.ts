/**
 * The transport every call to the observability service goes through: its
 * address, the caller's credential, and one request primitive.
 *
 *     serverAction (auth) → a client module → observabilityCall() → requestJson()
 *
 * One service stores the logs, traces and pod stats and owns the retention policy
 * over them, so one server-only address reaches every client beside this file. It
 * is also the only place that attaches the caller's credential — a token in a
 * hundred function signatures is a token in a hundred places it could be logged.
 *
 * Unset means the feature is off rather than broken: each call answers with an
 * error result naming the variable.
 */

import { requestJson, type ActionResult, type RequestOptions } from "@octo/http";
import { callerToken } from "@/app/auth/callerToken";

/** The env var that carries the address — the chart sets it; `pnpm dev` needs a port-forward. */
const envVar = "OBSERVABILITY_URL";

/** The service's base URL with any trailing slash trimmed, or "" when unset. */
export function observabilityBaseUrl(): string {
  return (process.env[envVar] ?? "").replace(/\/+$/, "");
}

/** The error result a client returns for `feature` when the address is unset. */
export function observabilityUnconfigured<T>(feature: string): ActionResult<T> {
  return { ok: false, error: `${feature} not configured (${envVar} unset)` };
}

/**
 * Issue one request against the observability service, as the caller.
 *
 * `feature` names what the call is for, and appears in the error result when the
 * address is unset — "trace query", "retention", "pod stats". Paths are absolute
 * and already encoded.
 */
export async function observabilityCall<T>(
  feature: string,
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = observabilityBaseUrl();
  if (!base) return observabilityUnconfigured<T>(feature);
  return requestJson<T>(method, `${base}${path}`, body, await authorized());
}

/**
 * The caller's credential, as request options. Absent when there is none — a call
 * made outside any request — which is not an error here: what to do without one is
 * the service's decision.
 */
async function authorized(): Promise<RequestOptions | undefined> {
  const token = await callerToken();
  return token ? { headers: { Authorization: `Bearer ${token}` } } : undefined;
}
