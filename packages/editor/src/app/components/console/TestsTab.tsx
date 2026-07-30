"use client";

import { useSuiteRun } from "../../run/SuiteRunContext";
import CaseResults from "./CaseResults";

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
      <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
        No test runs yet. Open the Testing tab and press Run tests.
      </p>
    );
  }

  if (run.running && !run.outcome) {
    return (
      <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
        Running {run.flow ? <code>{run.flow}</code> : null}…
      </p>
    );
  }

  if (run.error) {
    return (
      <p
        role="alert"
        className="px-3 py-2 font-mono text-xs text-red-600 dark:text-red-400"
      >
        {run.error}
      </p>
    );
  }

  if (!run.outcome) return null;

  if (!run.outcome.ok) {
    return (
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
    );
  }

  const cases = run.outcome.suites.flatMap((s) => s.cases);
  if (cases.length === 0) {
    return (
      <p className="px-3 py-2 text-xs text-zinc-500 dark:text-zinc-400">
        The suite ran, but it has no cases.
      </p>
    );
  }

  return <CaseResults cases={cases} />;
}
