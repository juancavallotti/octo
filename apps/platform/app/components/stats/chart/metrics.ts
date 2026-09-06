/**
 * The two metrics the platform charts, and the arithmetic that turns what the
 * service stores into what a reader wants to see.
 *
 * Both come from the Prometheus process collector, so every runtime pod exposes
 * them without any configuration — which is what makes them the right pair for a
 * view that has to work on a deployment nobody instrumented.
 */

import type { Reading, StatsSeries } from "@/app/model/stats";

/** Cumulative CPU seconds. A counter, so the service reports its growth. */
export const CPU_METRIC = "process_cpu_seconds_total";

/** Resident set size in bytes. A gauge, reported as read. */
export const MEM_METRIC = "process_resident_memory_bytes";

/** A column ready to plot. */
export interface Points {
  times: number[];
  values: Reading[];
}

const EMPTY: Points = { times: [], values: [] };

/**
 * A counter's growth, divided by the interval it covers, so CPU seconds per
 * second reads as **cores**.
 *
 * The divisor is per point, never one global step, and that is the whole care in
 * this function. A counter's value at a point is what it grew since the point
 * before, so the interval that produced it is whatever elapsed — and a scrape gap
 * widens that. Dividing everything by the nominal step would report a pod as
 * having burned a minute of CPU in a second every time a sample was missed.
 *
 *   rollup tier   ends[i] − times[i], the bucket's own width. Exact, and the
 *                 reason the API carries both edges: buckets are not contiguous.
 *   live tier     times[i] − times[i−1], for the same reason.
 *   first point   no predecessor, so the nominal step. Real data, not a zero to
 *                 be dropped: the service seeds the first delta from a row before
 *                 the window precisely so this point exists.
 *
 * An interval that is not positive yields a gap rather than an infinity. Nothing
 * should produce one, and a chart is not the place to find out.
 */
export function toCores(series: StatsSeries, stepMs: number): Points {
  const count = Math.min(series.times.length, series.values.length);
  if (count === 0) return EMPTY;

  const out: Points = { times: [], values: [] };
  for (let i = 0; i < count; i++) {
    out.times.push(series.times[i]);

    const value = series.values[i];
    const spanMs = intervalMs(series, i, stepMs);
    if (value === null || !Number.isFinite(value) || !(spanMs > 0)) {
      out.values.push(null);
      continue;
    }
    out.values.push(value / (spanMs / 1000));
  }
  return out;
}

/** How long the point at `i` covers, in milliseconds. */
function intervalMs(series: StatsSeries, i: number, stepMs: number): number {
  const end = series.ends[i];
  if (end !== undefined && end > series.times[i]) return end - series.times[i];
  if (i > 0) return series.times[i] - series.times[i - 1];
  return stepMs;
}

/** A gauge, as stored. Present so both metrics reach a chart the same way. */
export function toGauge(series: StatsSeries): Points {
  const count = Math.min(series.times.length, series.values.length);
  return {
    times: series.times.slice(0, count),
    values: series.values.slice(0, count),
  };
}

/**
 * The mean of every reading that is not a gap, or null when none is.
 *
 * What a rate should be summarized by. `process_cpu_seconds_total` advances in
 * ten-millisecond steps, so at one-second sampling an idle pod reads 0 or 0.1
 * cores and nothing between — and `latest` on that series reports "0.0000
 * cores" about half the time it is asked, which looks like a broken reading
 * rather than a quantized one. Averaged over the window it reads 0.010, which
 * is both stable and what `kubectl top` says.
 */
export function mean(points: Points): Reading {
  let total = 0;
  let count = 0;
  for (const value of points.values) {
    if (value === null || !Number.isFinite(value)) continue;
    total += value;
    count++;
  }
  return count === 0 ? null : total / count;
}

/** The most recent reading that is not a gap, or null when none is.
 *
 * What a gauge should be summarized by: it is a level, and the current level is
 * the fact. Use `mean` for a rate. */
export function latest(points: Points): Reading {
  for (let i = points.values.length - 1; i >= 0; i--) {
    const value = points.values[i];
    if (value !== null && Number.isFinite(value)) return value;
  }
  return null;
}

/** Render a core count at the precision the number deserves: a busy pod in
 * hundredths, an idle one in thousandths rather than as a flat "0.00". */
export function formatCores(value: Reading): string {
  if (value === null || !Number.isFinite(value)) return "—";
  if (value >= 1) return value.toFixed(2);
  if (value >= 0.01) return value.toFixed(3);
  return value.toFixed(4);
}
