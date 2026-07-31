"use client";

import { useMemo, useState } from "react";
import { FlaskConical, Plus } from "lucide-react";
import { useEditorState } from "../../state/editorState";
import { useTestSuites } from "../../providers/TestSuiteProvider";
import { parseSuite } from "../../suite/parse";
import { suiteFileName } from "../../suite/naming";
import SuiteRail, { type RailFlow } from "./SuiteRail";
import SuiteEditor from "./SuiteEditor";
import EmptyState from "./EmptyState";
import { useSuiteRun } from "../../run/SuiteRunContext";
import { scaffoldSuite } from "./scaffold";

/**
 * The Testing tab: the dolphin suites for this document's flows, and a YAML editor for
 * the selected one.
 *
 * Three states that look similar and are not, so each says which it is:
 *
 *   a draft            no id to store a suite against, so authoring is pointless — the
 *                      file would vanish with the session
 *   no flows           nothing to test yet; the canvas is where that starts
 *   a flow, no suite   the ordinary starting point, and the only one that is an
 *                      invitation
 *
 * What is deliberately NOT a state here is "no dolphin binary". Authoring works
 * regardless and only the Run button is gated, because a user who has written tests must
 * be able to find out why they cannot run them rather than watch the tab disappear.
 *
 * Selection lives in useState rather than a reducer, and stayed there once the form
 * arrived: the view mode belongs to the open suite and the selected case belongs to the
 * open suite's form, so both live where they are used and both are correctly forgotten
 * when another flow is opened. A reducer here would have had to remember to do that.
 */
export default function TestingView() {
  const { state } = useEditorState();
  const suites = useTestSuites();
  const suiteRun = useSuiteRun();
  const [selected, setSelected] = useState<string | null>(null);

  // A report belongs to the suite that produced it, so opening another flow drops it —
  // otherwise the console would show one suite's verdict under another's name.
  const select = (flow: string) => {
    setSelected(flow);
    suiteRun?.clear();
  };

  // Top-level flows only: those are what `octo invoke` — and so a suite — addresses.
  const flowNames = useMemo(
    () => state.document.flows.map((f) => f.name).filter(Boolean),
    [state.document.flows],
  );

  const rows = useMemo<RailFlow[]>(
    () =>
      flowNames.map((name) => {
        const content = suites?.suiteFor(name);
        if (content === undefined) return { name };
        const { suite, issues } = parseSuite(content);
        return { name, cases: suite.cases.length, issues: issues.length };
      }),
    [flowNames, suites],
  );

  if (!suites) return null; // the toggle hides the tab; this is belt and braces

  const active = selected && flowNames.includes(selected) ? selected : null;
  const content = active ? suites.suiteFor(active) : undefined;

  if (!suites.canPersist) {
    return (
      <EmptyState icon={<FlaskConical size={28} />} title="Save this integration to test it">
        A suite is a file that lives beside your flows — committed, and runnable from a
        terminal. There is nowhere to keep one until the integration has been saved.
      </EmptyState>
    );
  }

  if (flowNames.length === 0) {
    return (
      <EmptyState icon={<FlaskConical size={28} />} title="No flows to test yet">
        Add a flow on the canvas, then come back and write the tests that pin down what
        it does.
      </EmptyState>
    );
  }

  return (
    <div className="flex min-h-0 flex-1">
      <SuiteRail
        flows={rows}
        selected={active}
        onSelect={select}
        onAdd={(flow) => {
          suites.setSuite(flow, scaffoldSuite(flow));
          select(flow);
        }}
        canAdd
      />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {active === null ? (
          <EmptyState title="Pick a flow">
            Choose a flow on the left to see its tests.
          </EmptyState>
        ) : content === undefined ? (
          <EmptyState
            icon={<FlaskConical size={28} />}
            title={`No tests for "${active}" yet`}
            action={
              <button
                type="button"
                onClick={() => suites.setSuite(active, scaffoldSuite(active))}
                className="flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1.5 text-xs font-medium text-zinc-700 hover:bg-black/5 dark:border-white/15 dark:text-zinc-200 dark:hover:bg-white/10"
              >
                <Plus size={13} />
                Add tests
              </button>
            }
          >
            A suite is stored as <code>{suiteFileName(active)}</code>, beside your flows,
            so <code>dolphin test</code> gives the same verdict from a terminal.
          </EmptyState>
        ) : (
          // Keyed on the flow so the open suite's view mode and selected case are the
          // open suite's, and are gone when another one is opened.
          <SuiteEditor
            key={active}
            flow={active}
            content={content}
            onChange={(next) => suites.setSuite(active, next)}
            onRemove={() => void suites.removeSuite(active)}
          />
        )}
      </div>
    </div>
  );
}

