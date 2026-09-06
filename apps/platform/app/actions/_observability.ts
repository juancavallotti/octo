/**
 * Where the observability service is.
 *
 * One service stores the logs, traces and pod stats the platform reads and owns
 * the retention policy over them, so one server-only address reaches all four
 * clients beside this file (`_logs.ts`, `_traces.ts`, `_stats.ts`,
 * `_retention.ts`). The platform talks to it directly rather than through the
 * orchestrator: it owns the tables, and it serves its own in-cluster API.
 *
 * Unset means the feature is off rather than broken. Each client answers with an
 * error result naming the variable so the page can say what to set, instead of
 * a fetch against "" failing in a way that reads as an outage.
 */

import type { ActionResult } from "@octo/http";

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
