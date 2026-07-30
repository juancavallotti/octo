"use client";

import { Loader2, Play } from "lucide-react";
import type { TestTotals } from "../../run/testTransport";

/**
 * The Testing tab's toolbar: run the open suite, and the tally of what happened.
 *
 * The Run button stays VISIBLE when there is no dolphin binary and explains itself in a
 * tooltip, rather than disappearing. Someone who has just written a suite needs to learn
 * why they cannot run it; a button that is not there teaches nothing.
 */

/** The tally chips, in the order a reader cares about them. */
const CHIPS: { key: keyof TestTotals; label: string; className: string }[] = [
  { key: "failed", label: "failed", className: "text-red-600 dark:text-red-400" },
  { key: "errored", label: "errored", className: "text-amber-600 dark:text-amber-400" },
  { key: "passed", label: "passed", className: "text-emerald-600 dark:text-emerald-400" },
  { key: "skipped", label: "skipped", className: "text-zinc-500 dark:text-zinc-400" },
  { key: "notRun", label: "not run", className: "text-zinc-500 dark:text-zinc-400" },
];

export default function TestingToolbar({
  fileName,
  cases,
  totals,
  running,
  testAvailable,
  blockedReason,
  onRun,
  children,
}: {
  fileName: string;
  cases: number;
  /** The last run's tally, or null before the first run. */
  totals: TestTotals | null;
  running: boolean;
  testAvailable: boolean;
  /** Why Run is unavailable beyond a missing binary (a suite dolphin would refuse). */
  blockedReason?: string;
  onRun: () => void;
  /** Trailing controls (the delete button). */
  children?: React.ReactNode;
}) {
  const reason = !testAvailable
    ? "No test runner: DOLPHIN_BIN_PATH is not set on the server."
    : (blockedReason ?? "");
  const blocked = reason !== "";

  return (
    <div className="flex items-center gap-2 border-b border-black/10 px-3 py-1.5 dark:border-white/10">
      <button
        type="button"
        onClick={onRun}
        disabled={blocked || running || cases === 0}
        title={blocked ? reason : `Run ${fileName}`}
        className="flex shrink-0 items-center gap-1.5 rounded-md border border-black/10 px-2 py-1 text-xs font-medium text-zinc-700 transition-colors hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:border-white/15 dark:text-zinc-200 dark:hover:bg-white/10"
      >
        {running ? (
          <Loader2 size={13} className="animate-spin" />
        ) : (
          <Play size={13} />
        )}
        {running ? "Running…" : "Run tests"}
      </button>

      <code className="min-w-0 flex-1 truncate text-[11px] text-zinc-500 dark:text-zinc-400">
        {fileName}
      </code>

      {totals ? (
        <span className="flex shrink-0 items-center gap-2 text-[11px] tabular-nums">
          {CHIPS.filter((c) => (totals[c.key] as number) > 0).map((c) => (
            <span key={c.key} className={c.className}>
              {totals[c.key] as number} {c.label}
            </span>
          ))}
          {totals.cases === 0 && (
            <span className="text-zinc-400 dark:text-zinc-500">no cases ran</span>
          )}
        </span>
      ) : (
        <span className="shrink-0 text-[11px] text-zinc-400 dark:text-zinc-500">
          {cases} {cases === 1 ? "case" : "cases"}
        </span>
      )}

      {children}
    </div>
  );
}
