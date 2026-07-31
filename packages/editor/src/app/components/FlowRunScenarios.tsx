"use client";

import { useMemo } from "react";
import { FlaskConical, SkipForward } from "lucide-react";
import { useEditorState } from "../state/editorState";
import { useFlowRun } from "../run/FlowRunContext";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { parseSuite } from "../suite/parse";
import { envFor, inputFor, mocksFor, spiedBy } from "../suite/merge";
import type { Suite, SuiteCase } from "../suite/types";
import type { TestInput } from "../meta/types";

/**
 * The flow's test cases, offered in the ▶ menu as scenarios to run.
 *
 * This is the first of the two bridges between the Testing tab and the canvas, and it
 * earns its keep by removing a duplicate: the input and the mocks a case declares are the
 * same ones you would otherwise set up by hand every time you came back to debug it.
 * A committed test case is a scenario someone already wrote down.
 *
 * What it runs is the case's SETUP, resolved through {@link inputFor} and {@link mocksFor}
 * so the ▶ menu and `dolphin test` agree on what a case means — the mocks replace the
 * canvas's rather than merging with them, which is dolphin's rule and the reason
 * `runFlow` takes overrides at all.
 *
 * What it does NOT do is assert. The result lands in the console like any other run, to be
 * read; the verdict comes from the Testing tab, which runs the real thing. Saying so
 * matters — a green run here is not a passing test.
 */
export default function FlowRunScenarios({
  flowId,
  onStart,
}: {
  flowId: string;
  /** Close the menu — running is what the menu was opened to do. */
  onStart: () => void;
}) {
  const suites = useTestSuites();
  const flowRun = useFlowRun();
  const { state } = useEditorState();

  const flowName = state.document.flows.find((f) => f.id === flowId)?.name;
  const content = flowName ? suites?.suiteFor(flowName) : undefined;
  const suite = useMemo(() => (content ? parseSuite(content).suite : null), [content]);

  if (!suite || suite.cases.length === 0) return null;

  const start = (c: SuiteCase) => {
    onStart();
    void flowRun?.runFlow(flowId, scenarioInput(suite, c), {
      mocks: mocksFor(suite, c),
      spies: spiedBy(c),
    });
  };

  // A suite's `env:` is passed to the child process by `dolphin test`, and there is no
  // such channel on a flow run — an invoke uses the editor's own environment. Cases that
  // stand or fall on a fake credential will behave differently here, so say so rather
  // than let it look like the same run.
  const usesEnv = suite.cases.some((c) => Object.keys(envFor(suite, c)).length > 0);

  return (
    <div className="border-t border-black/10 dark:border-white/10">
      <div className="flex items-center gap-1.5 px-3 pb-1 pt-2 text-xs font-medium text-zinc-500">
        <FlaskConical size={12} className="shrink-0 text-zinc-400" />
        Scenarios
      </div>
      <p className="px-3 pb-1 text-xs text-zinc-400 dark:text-zinc-500">
        Runs the case’s input and mocks. Assertions are checked in the Testing tab.
        {usesEnv && " Its environment variables are not applied here."}
      </p>
      <ul className="pb-1">
        {suite.cases.map((c, i) => (
          <li key={i}>
            <button
              type="button"
              onClick={() => start(c)}
              className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
            >
              <FlaskConical size={13} className="shrink-0 text-zinc-400" />
              <span className="min-w-0 truncate">{c.name.trim() || "unnamed"}</span>
              {/* A skipped case is skipped by the BATTERY. Running one by hand is exactly
                  what you do while fixing it, so it stays clickable and only says so. */}
              {c.skip && (
                <span className="ml-auto flex shrink-0 items-center gap-1 text-xs text-zinc-400">
                  <SkipForward size={11} aria-label="skipped" />
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * A case's input as the run transport takes it: JSON text, named after the case so the
 * result reads as the scenario it was. The id is not persisted anywhere — the suite is
 * the record of this input, and saving a copy beside it would leave two to keep in step.
 */
function scenarioInput(suite: Suite, c: SuiteCase): TestInput {
  const input = inputFor(suite, c);
  return {
    id: `scenario:${c.name}`,
    name: c.name.trim() || "unnamed",
    ...(input.data === undefined ? {} : { data: JSON.stringify(input.data) }),
    ...(input.vars === undefined ? {} : { vars: JSON.stringify(input.vars) }),
  };
}
