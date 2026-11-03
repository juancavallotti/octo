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
  StatsSeriesOutcome,
  StatsSeriesQuery,
} from "@/app/model/stats";
import {
  readCatalogue,
  readOverview,
  type CataloguePage,
  type Overview,
  type StatsReads,
} from "@/app/components/metrics/deploymentReads";
import type { View } from "@/app/components/metrics/range";
import { withRead } from "./_auth";
import * as stats from "./_stats";
import type { ActionResult } from "./_client";

export async function listStatsMetrics(
  deploymentId: string,
  opts: { pods?: string[]; prefix?: string },
): Promise<ActionResult<StatsMetricsPage>> {
  return withRead(() => stats.getMetrics(deploymentId, opts));
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

const reads: StatsReads = {
  getPods: stats.getPods,
  getMetrics: (id) => stats.getMetrics(id),
  getSeries: stats.getSeries,
};

/** A deployment page's overview chart and pods, as one round trip. */
export async function readDeploymentOverview(
  deploymentId: string,
  view: View,
  nowMs: number,
): Promise<ActionResult<Overview>> {
  return withRead(async () => ({
    ok: true,
    data: await readOverview(reads, deploymentId, view, nowMs),
  }));
}

/** A deployment page's full metric grid, as one round trip. */
export async function readDeploymentCatalogue(
  deploymentId: string,
  view: View,
  nowMs: number,
): Promise<ActionResult<CataloguePage>> {
  return withRead(() => readCatalogue(reads, deploymentId, view, nowMs));
}
