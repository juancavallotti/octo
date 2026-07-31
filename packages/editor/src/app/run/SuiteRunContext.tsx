"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useEditorState } from "../state/editorState";
import { useRun } from "./RunContext";
import { useConsole } from "./console";
import { toRunnableYaml } from "../model/runConfig";
import type { TestCaseOutcome, TestRunOutcome } from "./testTransport";

/**
 * Running a dolphin suite, and the last thing it said.
 *
 * A provider rather than a hook because two places need it: the Testing tab starts the
 * run, and the bottom console shows what came back. Results belong down there beside the
 * logs, the problems and a flow run's output — every kind of run reports in the same
 * place, and one that took over the editor to do it would be the odd one out.
 *
 * Separate from RunProvider because a test run starts no long-lived process: nothing to
 * reattach to after a reload, no log stream, no config to hot-reload. A request and a
 * reply.
 *
 * The config is rendered from the CURRENT document, not from what was last saved: the
 * tab exists to tell you whether the flow in front of you does what you said, and
 * testing yesterday's copy of it would be worse than useless.
 */
export interface SuiteRunValue {
  running: boolean;
  /** The last run's report, or null before the first one. */
  outcome: TestRunOutcome | null;
  /** The flow whose suite produced it, so the console can name what it is showing. */
  flow: string | null;
  /** Why the run could not be made at all — distinct from a run whose tests failed. */
  error: string | null;
  run: (flow: string, content: string) => Promise<void>;
  /**
   * What the last run's named case actually produced, so the form can offer to evaluate
   * an assertion against it. Only ever the OPEN suite's, because opening another flow
   * clears the report.
   */
  outcomeFor: (caseName: string) => TestCaseOutcome | undefined;
  /** Forget the last report, e.g. when another suite is opened. */
  clear: () => void;
}

const SuiteRunContext = createContext<SuiteRunValue | null>(null);

export function SuiteRunProvider({ children }: { children: ReactNode }) {
  const { state } = useEditorState();
  const run = useRun();
  const consoleTabs = useConsole();
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<TestRunOutcome | null>(null);
  const [flow, setFlow] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Guards a second run arriving before the state update that disables the button.
  const inFlight = useRef(false);
  // Monotonic token for invalidating stale async completions.
  const generation = useRef(0);

  const clear = useCallback(() => {
    generation.current++;
    inFlight.current = false;
    setRunning(false);
    setOutcome(null);
    setError(null);
    setFlow(null);
  }, []);

  const start = useCallback(
    async (target: string, content: string) => {
      if (!run || inFlight.current) return;
      const runGeneration = generation.current;
      inFlight.current = true;
      setRunning(true);
      setError(null);
      setFlow(target);
      // Surface the console on the way in, so the report lands where the user is already
      // looking — the same thing a flow run does with its output.
      consoleTabs.openTo("tests");
      try {
        const resolved = await run.runTests({
          yaml: toRunnableYaml(state.document),
          integrationId: state.integration.id ?? undefined,
          suites: [{ name: target, content }],
        });
        if (generation.current !== runGeneration) return;
        setOutcome(resolved);
      } catch (e) {
        if (generation.current !== runGeneration) return;
        // The call could not be made at all: no runner, no session, a transport error.
        // A run whose TESTS failed comes back as a resolved outcome, not as this.
        setOutcome(null);
        setError((e as Error).message);
      } finally {
        if (generation.current !== runGeneration) return;
        inFlight.current = false;
        setRunning(false);
      }
    },
    [consoleTabs, run, state.document, state.integration.id],
  );

  const outcomeFor = useCallback(
    (caseName: string) =>
      outcome?.suites
        .flatMap((s) => s.cases)
        .find((c) => c.name === caseName)?.outcome,
    [outcome],
  );

  const value = useMemo<SuiteRunValue>(
    () => ({ running, outcome, flow, error, run: start, outcomeFor, clear }),
    [running, outcome, flow, error, start, outcomeFor, clear],
  );

  return <SuiteRunContext.Provider value={value}>{children}</SuiteRunContext.Provider>;
}

/**
 * The suite-run state, or null when no runner is wired at all — which is also what the
 * console reads to decide whether its Tests tab has anything behind it.
 */
export function useSuiteRun(): SuiteRunValue | null {
  return useContext(SuiteRunContext);
}
