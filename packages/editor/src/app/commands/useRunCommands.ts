"use client";

import { useMemo, useRef } from "react";
import { useTestSuites } from "../providers/TestSuiteProvider";
import { useFlowRun } from "../run/FlowRunContext";
import { useRun } from "../run/RunContext";
import { useSuiteRun } from "../run/SuiteRunContext";
import { currentFlowId } from "../state/currentFlow";
import { useEditorState } from "../state/editorState";
import { useProjectView } from "../state/useProject";
import { planRunAll } from "../suite/runAll";

export interface RunCommands {
  /** What `run` runs here: the integration, or on the Testing tab, the suites. */
  runs: "integration" | "tests";
  canRun: boolean;
  canRunFlow: boolean;
  canStop: boolean;
  /** Run whatever the view is about; a no-op when it cannot. */
  run(): void;
  /** Run the current flow once; a no-op when there is none or it cannot. */
  runFlow(): void;
  stop(): void;
}

/**
 * Run, run one flow, and stop: what Cmd+Enter, Shift+Cmd+Enter and Cmd+. do, and what
 * a host's Run menu does, from one place. Each is possible exactly when the control
 * it stands for is enabled — RunBar for the integration (a runner, and a project that
 * validates), TestRunButton for the suites (dolphin, and a loaded, runnable plan).
 */
export function useRunCommands(): RunCommands {
  const { state } = useEditorState();
  const project = useProjectView();
  const run = useRun();
  const flowRun = useFlowRun();
  const suiteRun = useSuiteRun();
  const suites = useTestSuites();

  const testing = state.viewMode === "testing";
  const plan = useMemo(
    () => planRunAll(suites?.all() ?? [], project.flows.map((f) => f.name).filter(Boolean)),
    [suites, project.flows],
  );
  const flowId = currentFlowId(state);

  const canRunTests =
    !!run?.testAvailable &&
    !!suiteRun &&
    !suiteRun.running &&
    !!suites?.loaded &&
    plan.targets.length > 0;
  const canRunIntegration =
    !!run && run.available && !run.running && !run.busy && run.validation.ok;
  const canRun = testing ? canRunTests : canRunIntegration;
  const canRunFlow = !testing && !!flowRun && !flowRun.busy && flowId !== null;
  const canStop = !!run?.running && !run.busy;

  // Read through a ref so the returned functions keep one identity and still act on
  // the latest state: a listener registered once must not run a stale closure.
  const latest = useRef({ canRun, canRunFlow, canStop, testing, plan, flowId, run, flowRun, suiteRun });
  latest.current = { canRun, canRunFlow, canStop, testing, plan, flowId, run, flowRun, suiteRun };

  const actions = useRef({
    run() {
      const l = latest.current;
      if (!l.canRun) return;
      if (l.testing) void l.suiteRun?.run(l.plan.targets, l.plan.skipped);
      else l.run?.start();
    },
    runFlow() {
      const l = latest.current;
      if (l.canRunFlow && l.flowId) void l.flowRun?.runFlow(l.flowId);
    },
    stop() {
      const l = latest.current;
      if (l.canStop) l.run?.stop();
    },
  }).current;

  return { runs: testing ? "tests" : "integration", canRun, canRunFlow, canStop, ...actions };
}
