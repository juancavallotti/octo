/**
 * Splitting a deployment's whole catalogue into queries the service will answer.
 *
 * Two limits apply, and only one of them is about names. The service caps a
 * query at twenty `metric` parameters, which is easy to respect. The one that
 * matters is series: a metric name is not a series, and on a real deployment
 * `octo_flow_duration_seconds_bucket` alone resolves to a hundred and eight of
 * them.
 *
 * So batches are packed by **estimated resolved series**, which the catalogue
 * already tells us exactly: each metric lists its label sets and the pods
 * exposing each one.
 *
 * A single metric larger than the budget goes in a request of its own rather
 * than being dropped: the service has no way to return part of a metric.
 */

import type { StatsMetric } from "@/app/model/stats";

/** The service's own cap on `metric` parameters per query. */
export const MAX_NAMES = 20;

/**
 * Series per request. Below podstats.MaxSelectedSeries, the declared bound on a
 * query: a batch that trips a limit fails the whole request rather than trimming
 * it.
 */
export const SERIES_BUDGET = 180;

/** How many series a metric will resolve to, from the catalogue alone. */
export function seriesCount(metric: StatsMetric): number {
  return metric.series.reduce((total, s) => total + Math.max(s.pods.length, 1), 0);
}

/** Pack the catalogue into requests, each a list of metric names. */
export function planBatches(metrics: StatsMetric[]): string[][] {
  const batches: string[][] = [];
  let current: string[] = [];
  let series = 0;

  // Largest first, so the one oversized metric claims its own request early and
  // the rest pack densely behind it instead of around it.
  const ordered = [...metrics].sort((a, b) => seriesCount(b) - seriesCount(a));

  for (const metric of ordered) {
    const cost = seriesCount(metric);

    if (current.length > 0 && (series + cost > SERIES_BUDGET || current.length >= MAX_NAMES)) {
      batches.push(current);
      current = [];
      series = 0;
    }
    current.push(metric.name);
    series += cost;
  }
  if (current.length > 0) batches.push(current);
  return batches;
}
