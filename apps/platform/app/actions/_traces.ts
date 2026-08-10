/**
 * The trace-store client — the middle layer between the trace server actions and
 * the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (getTraces()) → requestJson() → fetch
 *
 * Like the logs client, the platform talks to the trace service directly: it owns
 * the traces tables and exposes its own in-cluster query API, so there is no
 * reason to hop through the orchestrator. It is the same service and the same
 * `LOGS_URL` — traces live inside the logs binary. That URL and the wire shaping
 * are internal; callers see only the model's types.
 */

import { requestJson, type ActionResult } from "@octo/http";
import type {
  TraceAppsPage,
  TraceDetail,
  TraceFilters,
  TracePage,
  TraceRecord,
  TraceWindow,
} from "@/app/model/traces";
import {
  toApp,
  toRecord,
  toSummary,
  type RawApps,
  type RawDetail,
  type RawPage,
  type RawRecord,
} from "./_tracesWire";

/** The query base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.LOGS_URL ?? "").replace(/\/+$/, "");
}

/** GET `path?query` against the trace service, or an error result when it is
 * unconfigured. Path segments are already encoded by the caller. */
async function get<T>(path: string, query = ""): Promise<ActionResult<T>> {
  const base = baseUrl();
  if (!base) {
    return { ok: false, error: "trace query not configured (LOGS_URL unset)" };
  }
  return requestJson<T>("GET", query ? `${base}${path}?${query}` : `${base}${path}`);
}

/** Build the `/traces` query string from the filters, omitting empty axes. */
function traceQuery(f: TraceFilters): string {
  const params = new URLSearchParams();
  if (f.deploymentId) params.set("deploymentId", f.deploymentId);
  if (f.integrationId) params.set("integrationId", f.integrationId);
  if (f.appName) params.set("appName", f.appName);
  if (f.appVersion) params.set("appVersion", f.appVersion);
  if (f.flow) params.set("flow", f.flow);
  for (const status of f.statuses ?? []) params.append("status", status);
  if (f.minDurationMs != null) {
    params.set("minDurationMs", String(f.minDurationMs));
  }
  // Sent only when true: the service reads the parameter as a flag, and
  // "hasLlm=false" would filter nothing while reading as if it did.
  if (f.hasLlm) params.set("hasLlm", "true");
  if (f.q) params.set("q", f.q);
  if (f.from) params.set("from", f.from);
  if (f.to) params.set("to", f.to);
  if (f.before) params.set("before", f.before);
  if (f.limit != null) params.set("limit", String(f.limit));
  return params.toString();
}

/** List the apps with trace activity in a window. */
export async function getApps(
  w: TraceWindow,
): Promise<ActionResult<TraceAppsPage>> {
  const params = new URLSearchParams();
  if (w.from) params.set("from", w.from);
  if (w.to) params.set("to", w.to);

  const res = await get<RawApps>("/traces/apps", params.toString());
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      items: (res.data.items ?? []).map(toApp),
      from: res.data.from,
      to: res.data.to,
    },
  };
}

/** Fetch one page of traces matching the filters. */
export async function getTraces(
  f: TraceFilters,
): Promise<ActionResult<TracePage>> {
  const res = await get<RawPage>("/traces", traceQuery(f));
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      items: (res.data.items ?? []).map(toSummary),
      nextBefore: res.data.next_before ?? null,
    },
  };
}

/**
 * Fetch one trace and its records. Trace ids are opaque strings from the wire, so
 * they are percent-encoded into the path rather than concatenated into it.
 */
export async function getTrace(
  traceId: string,
  withBodies: boolean,
): Promise<ActionResult<TraceDetail>> {
  // bodies defaults to on at the service, so only the declining case is sent.
  const res = await get<RawDetail>(
    `/traces/${encodeURIComponent(traceId)}`,
    withBodies ? "" : "bodies=0",
  );
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      summary: toSummary(res.data.summary),
      items: (res.data.items ?? []).map(toRecord),
      truncated: res.data.truncated,
    },
  };
}

/** Fetch one record of one trace, with its captured payloads. */
export async function getRecord(
  traceId: string,
  id: string,
): Promise<ActionResult<TraceRecord>> {
  const path = `/traces/${encodeURIComponent(traceId)}/records/${encodeURIComponent(id)}`;
  const res = await get<RawRecord>(path);
  if (!res.ok) return res;
  return { ok: true, data: toRecord(res.data) };
}
