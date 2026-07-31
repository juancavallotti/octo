"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { TestCaseResult, TestCaseStatus } from "../../run/testTransport";

/**
 * What the last run said, case by case.
 *
 * A failure is shown beside what the flow actually produced, because "want X, got Y" is
 * most of what makes one legible — and dolphin's failure detail is already a multi-line
 * diff, so it is rendered preformatted rather than reflowed into prose.
 *
 * Deliberately absent: dolphin's `reproduce` command. run-host strips it before this
 * ever reaches the browser — it names a staged config that was deleted on the way out,
 * so it is a command that cannot be run offered to someone who would reasonably try.
 */

const STATUS: Record<TestCaseStatus, { label: string; className: string }> = {
  passed: { label: "PASS", className: "text-emerald-600 dark:text-emerald-400" },
  failed: { label: "FAIL", className: "text-red-600 dark:text-red-400" },
  errored: { label: "ERROR", className: "text-amber-600 dark:text-amber-400" },
  skipped: { label: "SKIP", className: "text-zinc-400 dark:text-zinc-500" },
  "not-run": { label: "NOT RUN", className: "text-zinc-400 dark:text-zinc-500" },
};

export default function CaseResults({ cases }: { cases: TestCaseResult[] }) {
  return (
    <ul className="divide-y divide-black/5 dark:divide-white/5">
      {cases.map((c) => (
        <CaseRow key={c.name} result={c} />
      ))}
    </ul>
  );
}

function CaseRow({ result }: { result: TestCaseResult }) {
  const status = STATUS[result.status];
  const failing = result.status === "failed" || result.status === "errored";
  // A passing case has nothing to read, so it starts closed; a failure is the reason
  // the user pressed Run, so it starts open.
  const [open, setOpen] = useState(failing);
  const detail = failing || result.skip || result.outcome;

  return (
    <li>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={detail ? open : undefined}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-black/5 dark:hover:bg-white/5"
      >
        {detail ? (
          open ? (
            <ChevronDown size={12} className="shrink-0 text-zinc-400" />
          ) : (
            <ChevronRight size={12} className="shrink-0 text-zinc-400" />
          )
        ) : (
          <span className="w-3 shrink-0" />
        )}
        <span
          className={`w-14 shrink-0 text-[10px] font-semibold tabular-nums ${status.className}`}
        >
          {status.label}
        </span>
        <span className="min-w-0 flex-1 truncate text-zinc-700 dark:text-zinc-200">
          {result.name}
        </span>
        <span className="shrink-0 text-[11px] tabular-nums text-zinc-400 dark:text-zinc-500">
          {result.elapsedMs}ms
        </span>
      </button>

      {open && detail && (
        <div className="space-y-2 px-3 pb-3 pl-[4.75rem]">
          {result.skip && (
            <p className="text-[11px] text-zinc-500 dark:text-zinc-400">
              skipped: {result.skip}
            </p>
          )}
          {result.failures?.map((failure, i) => (
            <div key={i}>
              <p className="text-[11px] font-medium text-red-600 dark:text-red-400">
                {failure.summary}
              </p>
              <Pre>{failure.detail}</Pre>
            </div>
          ))}
          {result.error && (
            <div>
              <p className="text-[11px] font-medium text-amber-600 dark:text-amber-400">
                the case could not be run
              </p>
              <Pre>{result.error}</Pre>
            </div>
          )}
          {result.stderr && <Pre>{result.stderr}</Pre>}
          {result.outcome && <Outcome outcome={result.outcome} />}
        </div>
      )}
    </li>
  );
}

/** What the flow actually did, as distinct from what the case wanted. */
function Outcome({ outcome }: { outcome: NonNullable<TestCaseResult["outcome"]> }) {
  if (outcome.error) {
    return (
      <Labelled label="the flow failed">
        <Pre>{outcome.error}</Pre>
      </Labelled>
    );
  }
  if (outcome.dropped) {
    return (
      <p className="text-[11px] text-zinc-500 dark:text-zinc-400">
        the flow dropped the message
      </p>
    );
  }
  if (outcome.result === undefined) return null;
  return (
    <Labelled label="the flow returned">
      <Pre>{JSON.stringify(outcome.result, null, 2)}</Pre>
    </Labelled>
  );
}

function Labelled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-[11px] text-zinc-500 dark:text-zinc-400">{label}</p>
      {children}
    </div>
  );
}

/** Preformatted, because a body diff is several lines and reflowing it destroys it. */
function Pre({ children }: { children: React.ReactNode }) {
  return (
    <pre className="mt-0.5 overflow-x-auto whitespace-pre rounded bg-black/5 p-2 font-mono text-[11px] leading-relaxed text-zinc-700 dark:bg-white/5 dark:text-zinc-300">
      {children}
    </pre>
  );
}
