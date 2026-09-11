/**
 * The data-retention client — the middle layer between the retention server
 * actions and the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (getRetention()) → requestJson() → fetch
 *
 * It talks to the observability service rather than the orchestrator, like
 * `_logs.ts` and `_traces.ts` beside it and for the same reason: that service
 * owns the tables a retention policy governs, and serves the policy alongside
 * the queries over them. The other admin settings go through
 * `actions/client/settings.ts` to the orchestrator, so this is deliberately not
 * there — the split follows which service owns the data, not which page the form
 * happens to sit on.
 *
 * The server-only address and the snake_case→camelCase shaping are internal;
 * callers see only the types in `app/model/retention.ts`, which is where they
 * live so that nothing client-side has to import this module to name them.
 */

import type { ActionResult } from "@octo/http";
import { observabilityCall } from "./_observability";
import type {
  RetentionPolicy,
  RetentionPolicyInput,
  RetentionRun,
} from "@/app/model/retention";

/** The policy as the service emits it (snake_case). */
interface RawPolicy {
  logs_days: number;
  traces_days: number;
  alerts_days: number;
  updated_at: string | null;
}

/** A sweep's report as the service emits it. */
interface RawRun {
  logs_deleted: number;
  traces_deleted: number;
  trace_summaries_deleted: number;
  alert_evaluations_deleted: number;
  alert_incidents_deleted: number;
  logs_cutoff: string | null;
  traces_cutoff: string | null;
  alerts_cutoff: string | null;
  duration_ms: number;
}

/** Issue a request against the observability service. */
function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  return observabilityCall<T>("retention", method, path, body);
}

function toPolicy(r: RawPolicy): RetentionPolicy {
  return {
    logsDays: r.logs_days,
    tracesDays: r.traces_days,
    alertsDays: r.alerts_days,
    updatedAt: r.updated_at,
  };
}

/** Read the stored policy. */
export async function getRetention(): Promise<ActionResult<RetentionPolicy>> {
  const res = await call<RawPolicy>("GET", "/settings/retention");
  if (!res.ok) return res;
  return { ok: true, data: toPolicy(res.data) };
}

/** Save the policy. Every window is sent; the service refuses a partial one. */
export async function saveRetention(
  input: RetentionPolicyInput,
): Promise<ActionResult<RetentionPolicy>> {
  const res = await call<RawPolicy>("PUT", "/settings/retention", {
    logs_days: input.logsDays,
    traces_days: input.tracesDays,
    alerts_days: input.alertsDays,
  });
  if (!res.ok) return res;
  return { ok: true, data: toPolicy(res.data) };
}

/** Enforce the stored policy now, and report what went. */
export async function runRetention(): Promise<ActionResult<RetentionRun>> {
  const res = await call<RawRun>("POST", "/retention/run");
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      logsDeleted: res.data.logs_deleted,
      tracesDeleted: res.data.traces_deleted,
      traceSummariesDeleted: res.data.trace_summaries_deleted,
      alertEvaluationsDeleted: res.data.alert_evaluations_deleted,
      alertIncidentsDeleted: res.data.alert_incidents_deleted,
      logsCutoff: res.data.logs_cutoff,
      tracesCutoff: res.data.traces_cutoff,
      alertsCutoff: res.data.alerts_cutoff,
      durationMs: res.data.duration_ms,
    },
  };
}
