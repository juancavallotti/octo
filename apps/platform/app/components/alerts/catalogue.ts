import type {
  AlertAggregate,
  AlertCondition,
  AlertConditionKind,
  AlertSource,
  WatchInput,
} from "@/app/model/alerts";

/**
 * What the editor's pickers offer, and what a new watch starts as.
 *
 * The metric list is a UI-side copy of the service's catalogue. It is here for
 * the dropdowns and nothing else — the service validates every definition and
 * refuses one it cannot evaluate, with a message naming the field, so this being
 * momentarily out of date shows up as a save that is refused rather than as a
 * watch that silently measures the wrong thing.
 *
 * Pod-stat metrics are deliberately absent: their names come from whatever the
 * runtime exports, which is a per-deployment question answered by
 * `GET /stats/{id}/metrics` rather than something to enumerate here.
 */

export interface MetricChoice {
  metric: string;
  label: string;
  aggregates: AlertAggregate[];
  /** What the number is, so the editor can render a threshold in its own units. */
  unit?: string;
}

export const METRICS: Record<AlertSource, MetricChoice[]> = {
  traces: [
    { metric: "traces", label: "Traces started", aggregates: ["count"] },
    { metric: "failed_traces", label: "Failed traces", aggregates: ["count"] },
    {
      metric: "error_rate",
      label: "Error rate",
      aggregates: ["ratio"],
      unit: "ratio",
    },
    {
      metric: "duration_ns",
      label: "Trace duration",
      aggregates: ["p95", "avg", "max"],
      unit: "ns",
    },
    {
      metric: "cost_usd",
      label: "Model cost",
      aggregates: ["sum"],
      unit: "usd",
    },
    { metric: "tokens", label: "Tokens", aggregates: ["sum"] },
    { metric: "llm_calls", label: "Model calls", aggregates: ["sum"] },
    { metric: "unpriced_calls", label: "Unpriced calls", aggregates: ["sum"] },
  ],
  logs: [
    { metric: "events", label: "Log events", aggregates: ["count"] },
    {
      metric: "error_rate",
      label: "Error rate",
      aggregates: ["ratio"],
      unit: "ratio",
    },
  ],
  pod_stats: [],
};

export const SOURCE_LABEL: Record<AlertSource, string> = {
  traces: "Traces",
  logs: "Logs",
  pod_stats: "Pod stats",
};

export const KIND_LABEL: Record<AlertConditionKind, string> = {
  threshold: "Above or below a number",
  spike: "Suddenly changed",
  absence: "Stopped reporting",
};

/** One sentence on what a kind is for, under the picker. */
export const KIND_HINT: Record<AlertConditionKind, string> = {
  threshold:
    "Compares a windowed number with one you choose. A rate is judged by a confidence bound, so a handful of requests cannot clear it.",
  spike:
    "Compares the last window with this series' own recent history. Fires only when the change is large statistically, absolutely and proportionally.",
  absence:
    "Fires when nothing has been recorded for a while — and only if the series was reporting before, so a deployment that never ran does not alert forever.",
};

export const METRIC_UNITS: Record<string, string | undefined> =
  Object.fromEntries(
    Object.values(METRICS)
      .flat()
      .map((m) => [m.metric, m.unit]),
  );

/** A short, stable id for a new row. Collisions within one watch are what matter. */
export function newId(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * A new condition, defaulted to the most common question anybody asks: is the
 * error rate for this integration above something.
 */
export function newCondition(): AlertCondition {
  return {
    id: newId("c"),
    type: "threshold",
    source: "traces",
    metric: "error_rate",
    aggregate: "ratio",
    scope: {},
    params: { op: "gt", threshold: 0.05, windowBuckets: 5 },
  };
}

/** The parameters a kind starts with when somebody switches to it. */
export function defaultParams(
  kind: AlertConditionKind,
): Record<string, unknown> {
  switch (kind) {
    case "spike":
      return { direction: "up", windowBuckets: 1, baselineBuckets: 30 };
    case "absence":
      return { forBuckets: 5 };
    default:
      return { op: "gt", threshold: 1, windowBuckets: 5 };
  }
}

/**
 * A new watch.
 *
 * It starts with a `log` action rather than none: a watch that fires and tells
 * nobody is the easiest mistake to make here, and the log action needs no
 * configuration at all, so the default is a watch that at least records itself
 * somewhere an operator will see.
 */
export function newWatch(): WatchInput {
  return {
    name: "",
    description: "",
    enabled: true,
    severity: "warning",
    combinator: "all",
    conditions: [newCondition()],
    actions: [{ id: newId("a"), type: "log", params: {} }],
    onNoData: "ok",
    stepSeconds: 60,
    intervalSeconds: 60,
    forSeconds: 300,
    renotifySeconds: 0,
  };
}
