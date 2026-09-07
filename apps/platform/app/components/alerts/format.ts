import type {
  AlertOutcome,
  AlertPhase,
  AlertStatus,
  Evaluation,
  Incident,
  Watch,
} from "@/app/model/alerts";

/**
 * How alerting reads on screen: the words, the numbers and the class maps.
 *
 * Pure, and separate from the components for that reason. What a phase is called,
 * what an outcome's number means in its own unit, and — most of all — what a
 * decline reason means in English are the parts worth pinning with tests, and
 * none of them need a DOM.
 */

/** The badge for a watch's phase, in the level-badge idiom the log table uses. */
export const PHASE_CLASS: Record<AlertPhase, string> = {
  firing: "bg-red-500/10 text-red-600 dark:text-red-400",
  pending: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  ok: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  // Not a severity — a watch this service cannot read. It reads as a fault
  // because it is one, and it is not being evaluated.
  invalid: "bg-zinc-500/10 text-zinc-600 dark:text-zinc-400",
};

export const PHASE_LABEL: Record<AlertPhase, string> = {
  firing: "Firing",
  pending: "Pending",
  ok: "OK",
  invalid: "Invalid",
};

export const STATUS_CLASS: Record<AlertStatus, string> = {
  firing: "bg-red-500/10 text-red-600 dark:text-red-400",
  ok: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  // The two that matter most, and they are deliberately not green: "we could
  // not look" must not read like "we looked and it was fine".
  insufficient: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  skipped: "bg-zinc-500/10 text-zinc-600 dark:text-zinc-400",
  error: "bg-red-500/10 text-red-600 dark:text-red-400",
};

export const SEVERITY_CLASS: Record<string, string> = {
  critical: "bg-red-500/10 text-red-600 dark:text-red-400",
  warning: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  info: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
};

/**
 * Why a condition declined, in English.
 *
 * This is the answer to "why did this not fire when I thought it would", which
 * is the question asked after every alert somebody expected. The service records
 * a machine code precisely so this page can say it in words without the service
 * having to guess how a page will phrase it.
 */
const REASONS: Record<string, string> = {
  no_data: "nothing to measure in the window",
  below_min_samples: "too few buckets reported to judge the window",
  below_min_baseline: "not enough history yet to say what normal is",
  denominator_too_small: "too few requests for the rate to mean anything",
  below_min_delta: "the change was real but too small to care about",
  below_min_ratio: "the change was not proportionally large enough",
  below_z: "within the range this series normally moves in",
  never_reported: "this has never reported, so its silence means nothing",
  no_ingest: "nothing was being ingested, so this was not evaluated",
  fetch_failed: "the data could not be read",
  watch_invalid: "this watch's definition could not be read",
  threshold_unmet: "did not reach the threshold",
  condition_met: "met",
};

export function explainReason(reason: string | undefined): string {
  if (!reason) return "";
  return REASONS[reason] ?? reason.replaceAll("_", " ");
}

/** A number in the units its metric is measured in. */
export function formatValue(
  value: number | null | undefined,
  unit?: string,
): string {
  if (value === null || value === undefined) return "—";
  switch (unit) {
    case "ratio":
      return `${(value * 100).toFixed(value < 0.01 ? 2 : 1)}%`;
    case "usd":
      return `$${value.toFixed(value < 0.01 ? 4 : 2)}`;
    case "ns":
      return formatDuration(value / 1_000_000);
    case "bytes":
      return formatBytes(value);
    default:
      return trim(value);
  }
}

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`;
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 1 : 0)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatBytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

/** Drop the trailing zeros a fixed-width number carries for nothing. */
function trim(value: number): string {
  if (Number.isInteger(value)) return value.toLocaleString();
  return Number(value.toFixed(4)).toLocaleString();
}

/** A duration in seconds, as somebody would say it. */
export function formatSeconds(seconds: number): string {
  if (seconds <= 0) return "off";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) {
    const minutes = seconds / 60;
    return `${Number.isInteger(minutes) ? minutes : minutes.toFixed(1)}m`;
  }
  const hours = seconds / 3600;
  return `${Number.isInteger(hours) ? hours : hours.toFixed(1)}h`;
}

/**
 * One condition's outcome as a sentence, in the same words the email uses.
 *
 * The threshold comes off the outcome rather than off the watch, so a row from
 * three weeks ago describes the comparison that was actually made rather than
 * one against a threshold somebody has since retuned.
 */
export function describeOutcome(o: AlertOutcome): string {
  const observed = formatValue(o.observed, o.unit);
  const threshold = formatValue(o.threshold, o.unit);
  const baseline =
    o.baseline === null || o.baseline === undefined
      ? ""
      : `, against a baseline of ${formatValue(o.baseline, o.unit)}`;
  return `observed ${observed} against ${threshold}${baseline}`;
}

/** "2 of 3 matched (any)" — how a composite verdict reads out loud. */
export function describeVerdict(
  e: Pick<Evaluation, "matched" | "total">,
  combinator: string,
): string {
  return `${e.matched} of ${e.total} matched (${combinator})`;
}

/** How long an episode has been running, or ran for. */
export function describeEpisode(i: Incident, now = Date.now()): string {
  const opened = Date.parse(i.openedAt);
  const ended = i.resolvedAt ? Date.parse(i.resolvedAt) : now;
  const minutes = Math.max(0, Math.round((ended - opened) / 60000));
  const span = minutes < 60 ? `${minutes}m` : `${(minutes / 60).toFixed(1)}h`;
  return i.resolvedAt
    ? `${span}, ${i.closedReason ?? "closed"}`
    : `${span} so far`;
}

/**
 * The schedule in one line, which is the part of a definition a list can show
 * without opening it.
 */
export function describeSchedule(w: Watch): string {
  const parts = [`every ${formatSeconds(w.intervalSeconds)}`];
  if (w.forSeconds > 0) parts.push(`held ${formatSeconds(w.forSeconds)}`);
  if (w.cooldownSeconds > 0)
    parts.push(`reports at most ${formatSeconds(w.cooldownSeconds)}`);
  return parts.join(" · ");
}
