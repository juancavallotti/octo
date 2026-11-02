"use server";

/**
 * Server actions for pod stats. Authorizes (any signed-in caller) and delegates to
 * the stats client (`_stats.ts`), which reads the observability service's API
 * directly. The model unwraps the ActionResult. Read-only — stored samples are
 * never mutated here, and the sidecar that writes them is configured through the
 * chart rather than through the platform.
 */

import type {
  StatsMetricsPage,
  StatsPodsPage,
  StatsSeriesOutcome,
  StatsSeriesPage,
  StatsSeriesQuery,
} from "@/app/model/stats";
import { withRead } from "./_auth";
import * as stats from "./_stats";
import type { ActionResult } from "./_client";

export async function listStatsPods(
  deploymentId: string,
): Promise<ActionResult<StatsPodsPage>> {
  return withRead(() => stats.getPods(deploymentId));
}

export async function listStatsMetrics(
  deploymentId: string,
  opts: { pods?: string[]; prefix?: string },
): Promise<ActionResult<StatsMetricsPage>> {
  return withRead(() => stats.getMetrics(deploymentId, opts));
}

export async function readStatsSeries(
  deploymentId: string,
  query: StatsSeriesQuery,
): Promise<ActionResult<StatsSeriesPage>> {
  return withRead(() => stats.getSeries(deploymentId, query));
}

/**
 * The same query against several deployments, read in parallel here: the browser
 * sends actions one at a time, so asking per deployment from there is a round trip
 * each. One deployment failing is its own outcome, not the whole call's.
 */
export async function readStatsSeriesForEach(
  deploymentIds: string[],
  query: StatsSeriesQuery,
): Promise<ActionResult<StatsSeriesOutcome[]>> {
  return withRead(async () => {
    const pages = await Promise.all(
      deploymentIds.map((id) => stats.getSeries(id, query)),
    );
    return {
      ok: true,
      data: pages.map((page, i) =>
        page.ok
          ? { deploymentId: deploymentIds[i], page: page.data }
          : { deploymentId: deploymentIds[i], error: page.error },
      ),
    };
  });
}
