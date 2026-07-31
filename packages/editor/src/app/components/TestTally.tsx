"use client";

import type { TestTotals } from "../run/testTransport";

/**
 * What a test run added up to, as chips.
 *
 * Shared by the Testing tab's toolbar (which speaks for one open suite) and the console's
 * per-suite group headers, so the same numbers are always coloured and ordered the same
 * way wherever they appear.
 *
 * Only non-zero counts are shown: a run that passed says "12 passed" and nothing else,
 * which is the whole of what the reader wanted to know.
 */
const CHIPS: { key: keyof TestTotals; label: string; className: string }[] = [
  { key: "failed", label: "failed", className: "text-red-600 dark:text-red-400" },
  { key: "errored", label: "errored", className: "text-amber-600 dark:text-amber-400" },
  { key: "passed", label: "passed", className: "text-emerald-600 dark:text-emerald-400" },
  { key: "skipped", label: "skipped", className: "text-zinc-500 dark:text-zinc-400" },
  { key: "notRun", label: "not run", className: "text-zinc-500 dark:text-zinc-400" },
];

export default function TestTally({ totals }: { totals: TestTotals }) {
  return (
    <span className="flex shrink-0 items-center gap-2 text-[11px] tabular-nums">
      {CHIPS.filter((c) => totals[c.key] > 0).map((c) => (
        <span key={c.key} className={c.className}>
          {totals[c.key]} {c.label}
        </span>
      ))}
      {totals.cases === 0 && (
        <span className="text-zinc-400 dark:text-zinc-500">no cases ran</span>
      )}
    </span>
  );
}
