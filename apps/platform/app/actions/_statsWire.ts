/**
 * The stats service's JSON shapes, and how they become the model's.
 *
 * These interfaces mirror `logs/internal/api/stats.go` field for field and **must
 * stay in sync with it**. Kept apart from the client so the one file that tracks a
 * Go struct is the one file to review when that struct changes.
 *
 * Unlike the logs and traces surfaces, this API is already camelCase on the wire,
 * so the mapping renames nothing. It is still written out by hand, because the two
 * things it does are the two things a generic converter gets wrong:
 *
 *  - **A null reading stays null.** `values` is `(number | null)[]` and a null is
 *    a gap in the scrape. `?? 0` anywhere in here would turn "nothing was
 *    reported" into "the measurement was zero" — the same mistake the storage side
 *    spent a commit removing.
 *  - **Absent collections become empty ones.** The Go side omits empty arrays and
 *    maps (`omitempty`), so `ends`, `min`, `labels` and the rest arrive undefined
 *    rather than empty, and every consumer would otherwise need its own guard.
 */

import type {
  MetricKind,
  Reading,
  ResolvedTier,
  StatsMetric,
  StatsPod,
  StatsSeries,
  StatsWarning,
} from "@/app/model/stats";

/** One pod's reporting state as the service emits it. */
export interface RawPod {
  pod: string;
  lastSeen: string;
  reporting: boolean;
  startedAt?: string;
  sampleInterval: string;
  rollupInterval: string;
  retention: string;
  generation: number;
  series: number;
  liveRows: number;
  rollupRows: number;
}

export interface RawPodsPage {
  deploymentId: string;
  items: RawPod[] | null;
  truncated: boolean;
}

export interface RawMetricSeries {
  labels?: Record<string, string>;
  pods: string[] | null;
}

export interface RawMetric {
  name: string;
  kind: string;
  series: RawMetricSeries[] | null;
}

export interface RawWarning {
  pod: string;
  reason: string;
}

export interface RawMetricsPage {
  deploymentId: string;
  items: RawMetric[] | null;
  warnings: RawWarning[] | null;
  truncated: boolean;
}

export interface RawSeries {
  pod: string;
  name: string;
  kind: string;
  labels?: Record<string, string>;
  times: number[] | null;
  ends?: number[];
  values: Reading[] | null;
  min?: Reading[];
  max?: Reading[];
  last?: Reading[];
  samples?: number[];
}

export interface RawSeriesPage {
  deploymentId: string;
  tier: string;
  step: string;
  from: string;
  to: string;
  series: RawSeries[] | null;
  warnings: RawWarning[] | null;
  truncated: boolean;
}

/** The kinds the service expands to; anything else is reported as unknown. */
const KINDS: MetricKind[] = ["counter", "gauge", "untyped"];

/** Narrow the wire's kind string, defaulting to the service's own fallback. */
export function toKind(kind: string): MetricKind {
  return (KINDS as string[]).includes(kind) ? (kind as MetricKind) : "unknown";
}

/** Narrow the resolved tier. The service never echoes `auto`, but a caller
 * charting the answer must not be handed a string it cannot branch on. */
export function toTier(tier: string): ResolvedTier {
  return tier === "rollup" ? "rollup" : "live";
}

export function toPod(r: RawPod): StatsPod {
  return {
    pod: r.pod,
    lastSeen: r.lastSeen,
    reporting: r.reporting,
    // Absent when the sidecar never wrote a start time, which is a fact about
    // the pod rather than a zero-valued date.
    startedAt: r.startedAt ?? null,
    sampleInterval: r.sampleInterval,
    rollupInterval: r.rollupInterval,
    retention: r.retention,
    generation: r.generation,
    series: r.series,
    liveRows: r.liveRows,
    rollupRows: r.rollupRows,
  };
}

export function toMetric(r: RawMetric): StatsMetric {
  return {
    name: r.name,
    kind: toKind(r.kind),
    series: (r.series ?? []).map((s) => ({
      labels: s.labels ?? {},
      pods: s.pods ?? [],
    })),
  };
}

export function toWarning(r: RawWarning): StatsWarning {
  return { pod: r.pod, reason: r.reason };
}

export function toSeries(r: RawSeries): StatsSeries {
  return {
    pod: r.pod,
    name: r.name,
    kind: toKind(r.kind),
    labels: r.labels ?? {},
    times: r.times ?? [],
    ends: r.ends ?? [],
    values: r.values ?? [],
    min: r.min ?? [],
    max: r.max ?? [],
    last: r.last ?? [],
    samples: r.samples ?? [],
  };
}
