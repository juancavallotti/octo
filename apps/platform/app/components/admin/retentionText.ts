import type { RetentionPolicy, RetentionRun } from "@/app/model/retention";
import type { Draft } from "./RetentionWindows";

/**
 * The words the retention form puts around its numbers, and the parsing that
 * decides whether a number is one at all.
 *
 * Separated from the manager because they are pure functions over a policy and a
 * sweep result, and testable as such — the manager around them is loading,
 * saving, confirming and sweeping, none of which these need.
 */

/** The maximum the aggregator will store, mirrored here so the form says so first. */
export const MAX_DAYS = 3650;

/** Seed the form from a stored policy. */
export function toDraft(p: RetentionPolicy): Draft {
  return {
    logs: String(p.logsDays),
    traces: String(p.tracesDays),
    alerts: String(p.alertsDays),
  };
}

/** Parse one field. Returns null when it is not a window that can be stored. */
export function parseDays(raw: string): number | null {
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const days = Number(trimmed);
  return days > MAX_DAYS ? null : days;
}

/** What a stored window means, in words. */
export function describe(days: number): string {
  if (days === 0) return "Kept forever — nothing is deleted.";
  if (days === 1) return "Deleted the day after it is recorded.";
  return `Deleted after ${days} days.`;
}

/** One line summarising a sweep, in the order the tables matter. */
export function describeRun(run: RetentionRun): string {
  const parts = [
    `${run.logsDeleted.toLocaleString()} log ${plural(run.logsDeleted, "event")}`,
    `${run.tracesDeleted.toLocaleString()} trace ${plural(run.tracesDeleted, "record")}`,
    `${run.traceSummariesDeleted.toLocaleString()} ${plural(run.traceSummariesDeleted, "trace")}`,
    `${run.alertEvaluationsDeleted.toLocaleString()} alert ${plural(run.alertEvaluationsDeleted, "evaluation")}`,
  ];
  return `Deleted ${parts.join(", ")}.`;
}

/** What the sweep about to run would delete, for the confirmation. */
export function describeScope(p: RetentionPolicy): string {
  return [
    p.logsDays > 0 ? `log events older than ${p.logsDays} days` : null,
    p.tracesDays > 0 ? `traces older than ${p.tracesDays} days` : null,
    p.alertsDays > 0 ? `alert history older than ${p.alertsDays} days` : null,
  ]
    .filter(Boolean)
    .join(" and ");
}

function plural(n: number, word: string): string {
  return n === 1 ? word : `${word}s`;
}
