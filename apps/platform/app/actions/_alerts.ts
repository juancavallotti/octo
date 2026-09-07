/**
 * The alerting client — the middle layer between the alerting server actions and
 * the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (listWatches()) → requestJson() → fetch
 *
 * It talks to the observability service rather than the orchestrator, like
 * `_logs.ts`, `_traces.ts` and `_retention.ts` beside it: that service owns both
 * the table a watch is stored in and the telemetry a watch reads. The split
 * follows which service owns the data, not which page the form happens to sit on.
 *
 * The server-only address and the snake_case shaping are internal; callers see
 * only the types in `app/model/alerts.ts`.
 */

import { requestJson, type ActionResult } from "@octo/http";
import {
  observabilityBaseUrl,
  observabilityUnconfigured,
} from "./_observability";
import type {
  Evaluation,
  EvaluationPage,
  EvaluationQuery,
  Incident,
  IncidentQuery,
  Watch,
  WatchInput,
  WatchListItem,
  WatchPreview,
} from "@/app/model/alerts";
import {
  fromWatch,
  toListItem,
  toWatch,
  type RawWatch,
  type RawWatchList,
} from "./_alertsWire";
import {
  toEvaluation,
  toIncident,
  toPreview,
  type RawEvaluationList,
  type RawIncidentList,
  type RawPreview,
} from "./_alertsHistoryWire";

function unconfigured<T>(): ActionResult<T> {
  return observabilityUnconfigured("alerting");
}

/** Issue a request against the observability service. */
async function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = observabilityBaseUrl();
  if (!base) return unconfigured();
  return requestJson<T>(method, `${base}${path}`, body);
}

/** Build a query string, dropping every parameter that was not supplied. */
function query(params: Record<string, unknown>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    if (Array.isArray(value)) {
      for (const item of value) search.append(key, String(item));
      continue;
    }
    search.set(key, String(value));
  }
  const rendered = search.toString();
  return rendered ? `?${rendered}` : "";
}

/** Every watch with its current state. */
export async function listWatches(): Promise<ActionResult<WatchListItem[]>> {
  const res = await call<RawWatchList>("GET", "/alerts/watches");
  if (!res.ok) return res;
  return { ok: true, data: (res.data.items ?? []).map(toListItem) };
}

export async function getWatch(id: string): Promise<ActionResult<Watch>> {
  const res = await call<RawWatch>(
    "GET",
    `/alerts/watches/${encodeURIComponent(id)}`,
  );
  if (!res.ok) return res;
  return { ok: true, data: toWatch(res.data) };
}

export async function createWatch(
  input: WatchInput,
  actorId: string,
): Promise<ActionResult<Watch>> {
  const res = await call<RawWatch>("POST", "/alerts/watches", {
    ...fromWatch(input),
    actorId,
  });
  if (!res.ok) return res;
  return { ok: true, data: toWatch(res.data) };
}

export async function saveWatch(
  id: string,
  input: WatchInput,
  actorId: string,
): Promise<ActionResult<Watch>> {
  const res = await call<RawWatch>(
    "PUT",
    `/alerts/watches/${encodeURIComponent(id)}`,
    { ...fromWatch(input), actorId },
  );
  if (!res.ok) return res;
  return { ok: true, data: toWatch(res.data) };
}

export async function deleteWatch(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/alerts/watches/${encodeURIComponent(id)}`);
}

/** Evaluate a definition now. Stores nothing and notifies nobody. */
export async function previewWatch(
  input: WatchInput,
): Promise<ActionResult<WatchPreview>> {
  const res = await call<RawPreview>(
    "POST",
    "/alerts/preview",
    fromWatch(input),
  );
  if (!res.ok) return res;
  return { ok: true, data: toPreview(res.data) };
}

/** Suppress notifications until `until`. Null lifts the mute. */
export async function muteWatch(
  id: string,
  until: string | null,
): Promise<ActionResult<void>> {
  return call<void>("POST", `/alerts/watches/${encodeURIComponent(id)}/mute`, {
    until,
  });
}

export async function listIncidents(
  q: IncidentQuery,
): Promise<ActionResult<Incident[]>> {
  const res = await call<RawIncidentList>(
    "GET",
    `/alerts/incidents${query({
      watchId: q.watchId,
      open: q.open ? "true" : undefined,
      from: q.from,
      to: q.to,
      limit: q.limit,
    })}`,
  );
  if (!res.ok) return res;
  return { ok: true, data: (res.data.items ?? []).map(toIncident) };
}

export async function acknowledgeIncident(
  id: string,
  actorId: string,
): Promise<ActionResult<void>> {
  return call<void>("POST", `/alerts/incidents/${encodeURIComponent(id)}/ack`, {
    actorId,
  });
}

/**
 * One page of the execution log, newest first.
 *
 * A watch id goes in the path when there is one, so the service's per-watch route
 * (and its index) serves it, and in the query string only when the page is asking
 * across every watch.
 */
export async function listEvaluations(
  q: EvaluationQuery,
): Promise<ActionResult<EvaluationPage>> {
  const params = query({
    watchId: q.watchId,
    incidentId: q.incidentId,
    notable: q.notable ? "true" : undefined,
    status: q.statuses,
    from: q.from,
    to: q.to,
    before: q.before,
    limit: q.limit,
  });
  const res = await call<RawEvaluationList>(
    "GET",
    `/alerts/evaluations${params}`,
  );
  if (!res.ok) return res;
  const items: Evaluation[] = (res.data.items ?? []).map(toEvaluation);
  return {
    ok: true,
    data: { items, nextBefore: res.data.next_before ?? null },
  };
}
