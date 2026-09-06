"use server";

/**
 * Server actions for pod stats. Authorizes (any signed-in caller) and delegates to
 * the stats client (`_stats.ts`), which reads the telemetry service's query API
 * directly. The model unwraps the ActionResult. Read-only — stored samples are
 * never mutated here, and the sidecar that writes them is configured through the
 * chart rather than through the platform.
 */

import type {
  StatsMetricsPage,
  StatsPodsPage,
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
