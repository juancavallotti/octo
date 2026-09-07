import type {
  AlertAction,
  AlertAggregate,
  AlertCondition,
  AlertConditionKind,
  AlertSource,
  WatchInput,
} from "@/app/model/alerts";

/**
 * What the editor offers, and what a new watch starts as.
 *
 * The bucket width is not on the form: it follows from how often the watch is
 * checked, and windows are shown as durations rather than as counts of it. See
 * resolution.ts.
 *
 * The metric list is a UI-side copy of the service's catalogue, for the pickers.
 * The service validates every definition and refuses one it cannot evaluate,
 * naming the field, so this being briefly out of date is a save that is refused
 * rather than a watch that quietly measures the wrong thing.
 */

/**
 * One measurable thing: a source and a metric, offered together.
 *
 * Together rather than as two dropdowns because the second only ever means
 * anything given the first, and picking "Logs" and then finding out what logs
 * can measure is a worse way round than reading the list of things you can
 * measure.
 */
export interface Measure {
  source: AlertSource;
  metric: string;
  label: string;
  group: string;
  aggregates: AlertAggregate[];
  /** What the number is, so a threshold can be rendered in its own units. */
  unit?: string;
}

export const MEASURES: Measure[] = [
  {
    source: "traces",
    metric: "error_rate",
    label: "Error rate",
    group: "Traces",
    aggregates: ["ratio"],
    unit: "ratio",
  },
  {
    source: "traces",
    metric: "failed_traces",
    label: "Failed runs",
    group: "Traces",
    aggregates: ["count"],
  },
  {
    source: "traces",
    metric: "traces",
    label: "Runs",
    group: "Traces",
    aggregates: ["count"],
  },
  {
    source: "traces",
    metric: "duration_ns",
    label: "Run duration",
    group: "Traces",
    aggregates: ["p95", "avg", "max"],
    unit: "ns",
  },
  {
    source: "traces",
    metric: "cost_usd",
    label: "Model cost",
    group: "Traces",
    aggregates: ["sum"],
    unit: "usd",
  },
  {
    source: "traces",
    metric: "tokens",
    label: "Tokens",
    group: "Traces",
    aggregates: ["sum"],
  },
  {
    source: "traces",
    metric: "llm_calls",
    label: "Model calls",
    group: "Traces",
    aggregates: ["sum"],
  },
  {
    source: "traces",
    metric: "unpriced_calls",
    label: "Unpriced model calls",
    group: "Traces",
    aggregates: ["sum"],
  },
  {
    source: "logs",
    metric: "error_rate",
    label: "Error rate",
    group: "Logs",
    aggregates: ["ratio"],
    unit: "ratio",
  },
  {
    source: "logs",
    metric: "events",
    label: "Log lines",
    group: "Logs",
    aggregates: ["count"],
  },
  {
    source: "pod_stats",
    metric: "",
    label: "A runtime metric…",
    group: "Pod stats",
    aggregates: ["max", "avg", "sum", "min"],
  },
];

/** The stable key a measure is chosen by. */
export function measureKey(source: AlertSource, metric: string): string {
  return source === "pod_stats" ? "pod_stats" : `${source}:${metric}`;
}

export function measureOf(condition: AlertCondition): Measure | undefined {
  return MEASURES.find(
    (m) =>
      measureKey(m.source, m.metric) ===
      measureKey(condition.source, condition.metric),
  );
}

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
    "Compares the last window with this app's own recent history. Fires only when the change is large statistically, absolutely and proportionally.",
  absence:
    "Fires when nothing has been recorded for a while — and only if it was reporting before, so an app that never ran does not alert forever.",
};

/** A short, stable id for a new row. Collisions within one watch are what matter. */
export function newId(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * A new condition, defaulted to the question most people come here to ask: is
 * this app's error rate above something.
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
 * A new watch: one condition, and no actions.
 *
 * No action rather than a harmless-looking default. A watch that fires and tells
 * nobody is the easiest mistake to make here, and the way to stop somebody
 * making it is to leave the section visibly empty and say so — not to fill it
 * with something that looks configured and reaches no one.
 */
export function newWatch(): WatchInput {
  return {
    name: "",
    description: "",
    enabled: true,
    severity: "warning",
    combinator: "all",
    conditions: [newCondition()],
    actions: [] as AlertAction[],
    onNoData: "ok",
    stepSeconds: 60,
    intervalSeconds: 60,
    forSeconds: 300,
    renotifySeconds: 0,
    cooldownSeconds: 0,
  };
}
