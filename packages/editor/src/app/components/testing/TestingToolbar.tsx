"use client";

import { Loader2, Play } from "lucide-react";
import { NO_TEST_RUNNER, type TestTotals } from "../../run/testTransport";
import TestTally from "../TestTally";
import Segmented from "./Segmented";

/**
 * The Testing tab's toolbar: run the open suite, switch between the two views of it, and
 * the tally of what happened to it.
 *
 * The Run button stays VISIBLE when there is no dolphin binary and explains itself in a
 * tooltip, rather than disappearing. Someone who has just written a suite needs to learn
 * why they cannot run it; a button that is not there teaches nothing. Form is gated the
 * same way, for the same reason.
 *
 * `running` and `busy` are not the same thing, and the difference shows up the moment the
 * header runs every suite at once: `running` means THIS suite is in that run, and drives
 * the spinner; `busy` means some run is in flight, and only disables the button. A suite
 * left out of the run should not claim to be running.
 */

/** Which view of the open suite is showing. Per suite, not per editor. */
export type SuiteViewMode = "form" | "yaml";

const VIEWS: { key: SuiteViewMode; label: string }[] = [
  { key: "form", label: "Form" },
  { key: "yaml", label: "YAML" },
];

export default function TestingToolbar({
  fileName,
  cases,
  totals,
  running,
  busy,
  testAvailable,
  blockedReason,
  mode,
  onMode,
  formBlockedReason,
  onRun,
  children,
}: {
  fileName: string;
  cases: number;
  /** This suite's share of the last run, or null when the last run did not include it. */
  totals: TestTotals | null;
  /** This suite is in the run that is in flight. */
  running: boolean;
  /** Some run is in flight — this suite's, or one started from the header. */
  busy?: boolean;
  testAvailable: boolean;
  /** Why Run is unavailable beyond a missing binary (a suite dolphin would refuse). */
  blockedReason?: string;
  mode: SuiteViewMode;
  onMode: (next: SuiteViewMode) => void;
  /** Why the form is unavailable — a file it could not read without deleting some of it. */
  formBlockedReason?: string;
  onRun: () => void;
  /** Trailing controls (the delete button). */
  children?: React.ReactNode;
}) {
  const reason = !testAvailable ? NO_TEST_RUNNER : (blockedReason ?? "");
  const blocked = reason !== "";
  const waiting = !!busy && !running;

  return (
    <div className="flex items-center gap-2 border-b border-black/10 px-3 py-1.5 dark:border-white/10">
      <button
        type="button"
        onClick={onRun}
        disabled={blocked || running || waiting || cases === 0}
        title={
          blocked
            ? reason
            : waiting
              ? "Another test run is in flight."
              : `Run ${fileName}`
        }
        className="flex shrink-0 items-center gap-1.5 rounded-md border border-black/10 px-2 py-1 text-xs font-medium text-zinc-700 transition-colors hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-40 dark:border-white/15 dark:text-zinc-200 dark:hover:bg-white/10"
      >
        {running ? (
          <Loader2 size={13} className="animate-spin" />
        ) : (
          <Play size={13} />
        )}
        {running ? "Running…" : "Run tests"}
      </button>

      <Segmented
        options={VIEWS}
        value={mode}
        onChange={onMode}
        disabled={formBlockedReason ? ["form"] : []}
        disabledReason={formBlockedReason}
        ariaLabel="Suite view"
      />

      <code className="min-w-0 flex-1 truncate text-[11px] text-zinc-500 dark:text-zinc-400">
        {fileName}
      </code>

      {totals ? (
        <TestTally totals={totals} />
      ) : (
        <span className="shrink-0 text-[11px] text-zinc-400 dark:text-zinc-500">
          {cases} {cases === 1 ? "case" : "cases"}
        </span>
      )}

      {children}
    </div>
  );
}
