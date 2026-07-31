"use client";

import type { ReactNode } from "react";
import { useSuiteRun } from "../../run/SuiteRunContext";
import type { SkippedSuite } from "../../suite/runAll";
import SuiteResults from "./SuiteResults";

/**
 * The panel body. Every console tab owns its own scroll region — the console has a fixed,
 * user-dragged height, and a report is as long as the run was.
 */
function Body({ children }: { children: ReactNode }) {
  return <div className="min-h-0 flex-1 overflow-auto">{children}</div>;
}

/**
 * The console's Tests tab: what the last dolphin run said.
 *
 * Three outcomes that are not the same thing, and the distinction is the tab's whole
 * job:
 *
 *   error      the run could not be MADE — no runner, no session, a transport failure
 *   !ok        dolphin ran but wrote nothing readable (or was killed on the timeout)
 *   ok         the report, whatever verdict it carries. A failing test is a successful
 *              run, and the reason the button was pressed.
 */
export default function TestsTab() {
  const run = useSuiteRun();

  if (!run || (!run.outcome && !run.error && !run.running)) {
    return (
      <Body>
        <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
          No test runs yet. Open the Testing tab and press Run tests.
        </p>
      </Body>
    );
  }

  if (run.running && !run.outcome) {
    return (
      <Body>
        <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
          {run.flows.length === 1 ? (
            <>
              Running <code>{run.flows[0]}</code>…
            </>
          ) : (
            `Running ${run.flows.length} suites…`
          )}
          {/* dolphin runs one case at a time on purpose (a suite may open a real dev
              database), and there is no progress stream — so a big run is a long wait
              behind a spinner, and saying so beats looking stuck. */}
          {run.flows.length > 3 && (
            <span className="text-zinc-400 dark:text-zinc-500"> this can take a while.</span>
          )}
        </p>
      </Body>
    );
  }

  if (run.error) {
    return (
      <Body>
        <p
          role="alert"
          className="px-3 py-2 font-mono text-xs text-red-600 dark:text-red-400"
        >
          {run.error}
        </p>
      </Body>
    );
  }

  if (!run.outcome) return null;

  if (!run.outcome.ok) {
    return (
      <Body>
        <div role="alert" className="px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
          <p>
            {run.outcome.timedOut
              ? "The run took too long and was stopped."
              : (run.outcome.error ?? "The run produced no report.")}
          </p>
          {run.outcome.logs.length > 0 && (
            <pre className="mt-1 overflow-x-auto whitespace-pre font-mono text-[11px] text-zinc-600 dark:text-zinc-400">
              {run.outcome.logs.join("\n")}
            </pre>
          )}
        </div>
      </Body>
    );
  }

  const suites = run.outcome.suites;
  if (suites.every((s) => s.cases.length === 0)) {
    return (
      <Body>
        <Skipped skipped={run.skipped} />
        <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
          The run produced no cases.
        </p>
      </Body>
    );
  }

  return (
    <Body>
      <Skipped skipped={run.skipped} />
      {suites.map((suite) => (
        <SuiteResults key={suite.name} suite={suite} />
      ))}
    </Body>
  );
}

/**
 * The suites that were left out of the run, and why.
 *
 * Above the results, not below them: the tally underneath is only the truth about what
 * ran, and a green one that silently covered three suites which never did would be a
 * worse answer than a red one.
 */
function Skipped({ skipped }: { skipped: SkippedSuite[] }) {
  if (skipped.length === 0) return null;
  return (
    <p className="border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-[11px] text-amber-800 dark:text-amber-200/90">
      {skipped.length === 1 ? "1 suite was" : `${skipped.length} suites were`} not run:{" "}
      {skipped.map((s) => `${s.flow} (${s.reason})`).join(", ")}
    </p>
  );
}
