/**
 * The pod stats client — the middle layer between the stats server actions and
 * the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (getSeries()) → requestJson() → fetch
 *
 * It reads the observability service, like logs, traces and retention: that
 * service owns the stats query API too (see `_observability.ts` for why directly
 * and not through the orchestrator). The address and the wire shaping are
 * internal; callers see only the model's types.
 *
 * The routes are deployment-scoped because the storage is. A pod's rows live under
 * `octo:stats:v0:{deployment}:{pod}:`, and the deployment id is the only key into
 * them — there is no way to ask this API about a pod without knowing which
 * deployment it belongs to.
 */

import { requestJson, type ActionResult } from "@octo/http";
import { observabilityBaseUrl, observabilityUnconfigured } from "./_observability";
import type {
  StatsMetricsPage,
  StatsPodsPage,
  StatsSeriesPage,
  StatsSeriesQuery,
} from "@/app/model/stats";
import {
  toMetric,
  toPod,
  toSeries,
  toTier,
  toWarning,
  type RawMetricsPage,
  type RawPodsPage,
  type RawSeriesPage,
} from "./_statsWire";

/** GET `path?query` against the observability service, or an error result when
 * it is unconfigured. Path segments are already encoded by the caller. */
async function get<T>(path: string, query = ""): Promise<ActionResult<T>> {
  const base = observabilityBaseUrl();
  if (!base) {
    return observabilityUnconfigured("pod stats");
  }
  return requestJson<T>("GET", query ? `${base}${path}?${query}` : `${base}${path}`);
}

/** The stats routes for one deployment. Ids are opaque, so they are encoded. */
function statsPath(deploymentId: string, leaf: string): string {
  return `/stats/${encodeURIComponent(deploymentId)}/${leaf}`;
}

/** List the pods of a deployment that have reported stats. */
export async function getPods(
  deploymentId: string,
): Promise<ActionResult<StatsPodsPage>> {
  const res = await get<RawPodsPage>(statsPath(deploymentId, "pods"));
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      deploymentId: res.data.deploymentId,
      items: (res.data.items ?? []).map(toPod),
      truncated: res.data.truncated,
    },
  };
}

/** List the metrics a deployment's pods expose, reading no rows. */
export async function getMetrics(
  deploymentId: string,
  opts: { pods?: string[]; prefix?: string } = {},
): Promise<ActionResult<StatsMetricsPage>> {
  const params = new URLSearchParams();
  // Repeated rather than joined: the service reads query["pod"] as a list.
  for (const pod of opts.pods ?? []) params.append("pod", pod);
  if (opts.prefix) params.set("prefix", opts.prefix);

  const res = await get<RawMetricsPage>(
    statsPath(deploymentId, "metrics"),
    params.toString(),
  );
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      deploymentId: res.data.deploymentId,
      items: (res.data.items ?? []).map(toMetric),
      warnings: (res.data.warnings ?? []).map(toWarning),
      truncated: res.data.truncated,
    },
  };
}

/** Build the `/series` query string, omitting everything left at its default. */
function seriesQuery(q: StatsSeriesQuery): string {
  const params = new URLSearchParams();
  for (const metric of q.metrics) params.append("metric", metric);
  for (const pod of q.pods ?? []) params.append("pod", pod);
  for (const [key, value] of Object.entries(q.labels ?? {})) {
    params.append("label", `${key}=${value}`);
  }
  if (q.tier) params.set("tier", q.tier);
  if (q.from) params.set("from", q.from);
  if (q.to) params.set("to", q.to);
  if (q.stats?.length) params.set("stats", q.stats.join(","));
  if (q.counters) params.set("counters", q.counters);
  if (q.limit != null) params.set("limit", String(q.limit));
  return params.toString();
}

/**
 * Read points for the named metrics.
 *
 * A query naming no metric is refused here rather than at the service, which
 * answers it with a 400. The bound is the point of the parameter — rows are stored
 * positionally, so an unfiltered query reads every series of every pod — and a
 * caller that lost its metric list along the way should not be told about it by an
 * error page.
 */
export async function getSeries(
  deploymentId: string,
  query: StatsSeriesQuery,
): Promise<ActionResult<StatsSeriesPage>> {
  if (query.metrics.length === 0) {
    return { ok: false, error: "name at least one metric to read" };
  }

  const res = await get<RawSeriesPage>(
    statsPath(deploymentId, "series"),
    seriesQuery(query),
  );
  if (!res.ok) return res;
  return {
    ok: true,
    data: {
      deploymentId: res.data.deploymentId,
      tier: toTier(res.data.tier),
      step: res.data.step,
      from: res.data.from,
      to: res.data.to,
      series: (res.data.series ?? []).map(toSeries),
      warnings: (res.data.warnings ?? []).map(toWarning),
      truncated: res.data.truncated,
    },
  };
}
