"use client";

import { formatDuration } from "./format";
import type { WorkBreakdown } from "./blockClasses";

/**
 * Where a trace's time went: waiting on something else, working here, or
 * unaccounted for.
 *
 * Drawn as one bar per class rather than as a single stacked bar, which is the
 * whole reason this reads honestly. Concurrent work of different kinds — a fork
 * with a REST call in one branch and a transform in another — spends the same
 * wall-clock in two classes at once, so the shares genuinely can sum to more
 * than the trace lasted. A stacked bar cannot draw that without either
 * normalizing the numbers (making them wrong) or overflowing (making them look
 * broken). Separate bars are each independently true.
 *
 * Unattributed is a bar like the others rather than the leftover gap, because it
 * is a real answer: engine overhead, queueing, and composites that held nothing.
 * Leaving it implicit is what lets the other three quietly absorb it.
 */

const SEGMENTS: {
  key: keyof Pick<WorkBreakdown, "ioNs" | "cpuNs" | "unclassifiedNs" | "unattributedNs">;
  label: string;
  color: string;
  title: string;
}[] = [
  {
    key: "ioNs",
    label: "Waiting",
    color: "bg-sky-500/70",
    title: "Time with at least one call to something outside the process in flight — HTTP, SQL, a broker, a model provider.",
  },
  {
    key: "cpuNs",
    label: "Working",
    color: "bg-emerald-500/70",
    title: "Time running in the process: transforms, validation, templating, signature checks.",
  },
  {
    key: "unclassifiedNs",
    label: "Unclassified",
    color: "bg-amber-500/60",
    title: "Blocks nobody has classified yet. Kept apart rather than folded into one of the others, because guessing would put real time in the wrong place.",
  },
  {
    key: "unattributedNs",
    label: "Unattributed",
    color: "bg-zinc-400/50",
    title: "Time inside the trace that no block accounted for: engine overhead, queueing, and composites that ran nothing.",
  },
];

export default function WorkSplit({ breakdown }: { breakdown: WorkBreakdown }) {
  const total = Math.max(breakdown.totalNs, 1);

  return (
    <div className="space-y-1.5">
      {SEGMENTS.map((segment) => {
        const value = breakdown[segment.key];
        if (value <= 0) return null;
        return (
          <div key={segment.key} className="flex items-center gap-2 text-xs">
            <span className="w-20 shrink-0 text-zinc-500 dark:text-zinc-400" title={segment.title}>
              {segment.label}
            </span>
            <span className="h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-black/[0.06] dark:bg-white/[0.08]">
              <span
                style={{ width: `${Math.min((value / total) * 100, 100)}%` }}
                className={`block h-full rounded-full ${segment.color}`}
              />
            </span>
            <span className="w-16 shrink-0 text-right font-mono text-[11px] text-zinc-500 dark:text-zinc-400">
              {formatDuration(value)}
            </span>
          </div>
        );
      })}

      {breakdown.concurrent && (
        <p className="text-[11px] text-zinc-400">
          These overlap: work of different kinds ran at the same time, so they
          add up to more than the trace lasted.
        </p>
      )}

      {breakdown.unknownTypes.length > 0 && (
        <p className="text-[11px] text-amber-600 dark:text-amber-400">
          Unclassified: {breakdown.unknownTypes.join(", ")}
        </p>
      )}
    </div>
  );
}
