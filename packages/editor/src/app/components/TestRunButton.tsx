"use client";

import { useMemo } from "react";
import { FlaskConical, Loader2 } from "lucide-react";
import { useEditorState } from "../state/editorState";
import { useRun } from "../run/RunContext";
import { useSuiteRun } from "../run/SuiteRunContext";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { NO_TEST_RUNNER } from "../run/testTransport";
import { planRunAll, type SkippedSuite } from "../suite/runAll";

/**
 * Run every suite this document has, from the header. It is what the RUN control becomes
 * on the Testing tab — see RunBar.
 *
 * Running the whole set is the ordinary thing to want there. Starting the integration is
 * not: nothing on that tab is about a live runner, and a button that did it would be a
 * button nobody meant to press.
 *
 * Which suites those are is {@link planRunAll}'s decision, and it holds back the ones
 * dolphin would refuse — it loads every named file before running any case, so one bad
 * suite would take the whole run down and report nothing about the good ones. What was
 * held back is named in the tooltip before the run and in the console after it, because a
 * green tally that quietly covered three suites is worse than a red one.
 *
 * Like the open suite's Run button, this stays VISIBLE when it cannot run and explains
 * itself instead of disappearing: someone who has just written tests needs to learn why
 * they cannot run them.
 */

/** Flow names to list in the tooltip before it stops being a help and starts being a wall. */
const NAME_LIMIT = 6;

export default function TestRunButton() {
  const { state } = useEditorState();
  const run = useRun();
  const suiteRun = useSuiteRun();
  const suites = useTestSuites();

  const flowNames = useMemo(
    () => state.document.flows.map((f) => f.name).filter(Boolean),
    [state.document.flows],
  );
  // `suites` is memoized by its provider on the stored suites, so these recompute exactly
  // when a suite changes rather than on every render.
  const stored = useMemo(() => suites?.all() ?? [], [suites]);
  const plan = useMemo(() => planRunAll(stored, flowNames), [stored, flowNames]);

  // Testing is unreachable without both, but this component is exported into an app-owned
  // header slot, so it does not assume the tree it is mounted in.
  if (!suites || !suiteRun) return null;

  const running = suiteRun.running;
  const reason = blockedReason({
    testAvailable: run?.testAvailable ?? false,
    loaded: suites.loaded,
    canPersist: suites.canPersist,
    running,
    targets: plan.targets.length,
    stored: stored.length,
    skipped: plan.skipped,
  });

  return (
    <button
      type="button"
      onClick={() => void suiteRun.run(plan.targets, plan.skipped)}
      disabled={reason !== null}
      title={reason ?? readyTitle(plan.targets.map((t) => t.name), plan.skipped)}
      className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-3 py-1 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
    >
      {running ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
      ) : (
        <FlaskConical className="h-3.5 w-3.5" />
      )}
      {running ? "Running…" : "Run tests"}
    </button>
  );
}

/**
 * Why the button cannot be pressed, or null when it can. Ordered so the first thing the
 * user could act on is the thing they are told about.
 */
function blockedReason(s: {
  testAvailable: boolean;
  loaded: boolean;
  canPersist: boolean;
  running: boolean;
  targets: number;
  stored: number;
  skipped: SkippedSuite[];
}): string | null {
  if (!s.testAvailable) return NO_TEST_RUNNER;
  if (!s.canPersist) return "Save this integration to write tests.";
  if (!s.loaded) return "Loading test suites…";
  if (s.running) return "A test run is already in flight.";
  if (s.targets > 0) return null;
  if (s.stored === 0) return "No test suites yet — add one in the Testing tab.";
  return `Nothing can run:\n${bullets(s.skipped)}`;
}

/** What pressing it will do, and what it will leave out. */
function readyTitle(names: string[], skipped: SkippedSuite[]): string {
  const shown = names.slice(0, NAME_LIMIT).join(", ");
  const rest = names.length - NAME_LIMIT;
  const suites = `${names.length} ${names.length === 1 ? "suite" : "suites"}`;
  const head = `Run ${suites}: ${shown}${rest > 0 ? `, and ${rest} more` : ""}`;
  if (skipped.length === 0) return head;
  return `${head}\n\n${skipped.length} not run:\n${bullets(skipped)}`;
}

const bullets = (skipped: SkippedSuite[]) =>
  skipped.map((s) => `• ${s.flow} — ${s.reason}`).join("\n");
