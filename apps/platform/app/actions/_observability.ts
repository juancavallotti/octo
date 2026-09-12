/**
 * The transport every call to the observability service goes through: its
 * address, the caller's credential, and one request primitive.
 *
 *     serverAction (auth) → a client module → observabilityCall() → requestJson()
 *
 * One service stores the logs, traces and pod stats the platform reads and owns
 * the retention policy over them, so one server-only address reaches all of the
 * clients beside this file (`_logs.ts`, `_traces.ts`, `_stats.ts`,
 * `_retention.ts`, `_storage.ts`, `_alerts.ts`). The platform talks to it
 * directly rather than through the orchestrator: it owns the tables, and it
 * serves its own in-cluster API.
 *
 * It is also the only place that attaches the caller's credential, for the same
 * reason: one place knows how to reach the service, so one place knows how to
 * speak to it as somebody. The clients keep their signatures — a token in a
 * hundred function signatures is a token in a hundred places it could be logged.
 *
 * Unset means the feature is off rather than broken. Each call answers with an
 * error result naming the variable so the page can say what to set, instead of a
 * fetch against "" failing in a way that reads as an outage.
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
 * The caller's credential, as request options.
 *
 * Absent when there is none — a call made outside any request. That is not an
 * error here: the service decides what it will do without one, and an install
 * that is not enforcing will do it happily.
 */
async function authorized(): Promise<RequestOptions | undefined> {
  const token = await callerToken();
  return token ? { headers: { Authorization: `Bearer ${token}` } } : undefined;
}
