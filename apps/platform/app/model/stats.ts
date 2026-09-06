/**
 * Browser-side client for pod stats — the per-pod CPU, memory and runtime metrics
 * a sidecar samples into Redis and the telemetry service reads back. Backed by the
 * stats server actions, which call that service's query API directly; this wrapper
 * unwraps the ActionResult so callers keep a value-or-throw contract. Read-only.
 *
 * The shapes below mirror `logs/internal/api/stats.go`. Three of its rules survive
 * into these types and must survive into every consumer:
 *
 *  - A reading is `number | null`, and null is a **gap** — a series the pod's
 *    dictionary knows that the scrape did not report. It is not a zero. A chart
 *    that draws zero for it invents a cliff where a metric merely stopped being
 *    reported, and the whole storage layer went to some trouble to keep the two
 *    apart.
 *  - A counter's value is its **growth** over the interval ending at that point,
 *    on both tiers. That is what lets a caller chart one without knowing which
 *    tier answered — and it means the number is not the stored reading.
 *  - `truncated` and `warnings` are answers, not noise. A capped pod list yields a
 *    partial result that reads exactly like a complete one.
 */

import * as statsActions from "@/app/actions/stats";
import { unwrap } from "./bff";

/** Which stored resolution a query reads. `auto` resolves to one of the others. */
export type StatsTier = "auto" | "live" | "rollup";

/** A resolved tier, as the service echoes it back. Never `auto`. */
export type ResolvedTier = "live" | "rollup";

/** A metric's Prometheus type, expanded from the single letter Redis holds. */
export type MetricKind = "counter" | "gauge" | "untyped" | "unknown";

/** One point. `null` is a gap in the scrape, never a measurement of zero. */
export type Reading = number | null;

/** One pod's reporting state and the tier configuration it was sampled under. */
export interface StatsPod {
  pod: string;
  lastSeen: string;
  reporting: boolean;
  startedAt: string | null;

  /** Go duration strings, as the sidecar was configured — e.g. "1s", "1h0m0s". */
  sampleInterval: string;
  rollupInterval: string;
  retention: string;

  generation: number;
  series: number;

  /**
   * Row counts per tier, reported separately because zero live rows beside a full
   * history is the ordinary state of a pod that stopped a few hours ago: the live
   * tier is kept for twice the rollup interval while the pod stays indexed for the
   * whole retention window. Shown together, that reads as expected rather than as
   * a fault.
   */
  liveRows: number;
  rollupRows: number;
}

export interface StatsPodsPage {
  deploymentId: string;
  items: StatsPod[];
  truncated: boolean;
}

/** One label set of a metric, and the pods exposing it. */
export interface StatsMetricSeries {
  labels: Record<string, string>;
  pods: string[];
}

/** One metric name, with its label sets nested underneath. */
export interface StatsMetric {
  name: string;
  kind: MetricKind;
  series: StatsMetricSeries[];
}

export interface StatsMetricsPage {
  deploymentId: string;
  items: StatsMetric[];
  warnings: StatsWarning[];
  truncated: boolean;
}

/** One pod that could not be answered for, and why. Never fatal to a request. */
export interface StatsWarning {
  pod: string;
  reason: string;
}

/**
 * One decoded series of one pod, columnar and oldest-first. The columns are
 * parallel: `values[i]` is the reading at `times[i]`.
 */
export interface StatsSeries {
  pod: string;
  name: string;
  kind: MetricKind;
  labels: Record<string, string>;

  /** Unix milliseconds. On the rollup tier this is the bucket's start. */
  times: number[];
  /**
   * Bucket ends, rollup tier only. Carried because rows are not contiguous: when
   * a bucket's end does not meet the next one's start, scraping stopped between
   * them — which is visible only if both edges are known.
   */
  ends: number[];

  values: Reading[];
  min: Reading[];
  max: Reading[];
  last: Reading[];
  samples: number[];
}

export interface StatsSeriesPage {
  deploymentId: string;
  /** The tier that actually answered. The step differs between them. */
  tier: ResolvedTier;
  /** A Go duration string — the resolved tier's nominal spacing. */
  step: string;
  from: string;
  to: string;

  series: StatsSeries[];
  warnings: StatsWarning[];
  truncated: boolean;
}

/** Which numbers a query wants per point. Everything but `value` is rollup-only. */
export type StatName = "value" | "min" | "max" | "last" | "samples";

/**
 * One request for series data.
 *
 * `metrics` is required rather than optional, mirroring the service: rows are
 * stored positionally, so a query with no name filter reads every series of every
 * pod. It is the bound the whole read strategy rests on, and a caller should meet
 * it at compile time rather than as a 400.
 */
export interface StatsSeriesQuery {
  metrics: string[];
  pods?: string[];
  /** Narrowed within those metrics; every label must match. */
  labels?: Record<string, string>;
  tier?: StatsTier;
  from?: string;
  to?: string;
  stats?: StatName[];
  counters?: "delta" | "absolute";
  limit?: number;
}

/** The pods of a deployment that the stats index still holds. */
export async function listStatsPods(
  deploymentId: string,
): Promise<StatsPodsPage> {
  return unwrap(await statsActions.listStatsPods(deploymentId));
}

/** The catalogue: every series a deployment's pods describe, with no rows read. */
export async function listStatsMetrics(
  deploymentId: string,
  opts: { pods?: string[]; prefix?: string } = {},
): Promise<StatsMetricsPage> {
  return unwrap(await statsActions.listStatsMetrics(deploymentId, opts));
}

/** Points for the named metrics, per pod. */
export async function readStatsSeries(
  deploymentId: string,
  query: StatsSeriesQuery,
): Promise<StatsSeriesPage> {
  return unwrap(await statsActions.readStatsSeries(deploymentId, query));
}
