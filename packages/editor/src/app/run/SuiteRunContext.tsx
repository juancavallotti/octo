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
import type { SkippedSuite } from "../suite/runAll";
import type { TestRunOutcome, TestSuiteInput } from "./testTransport";

/**
 * Running dolphin suites, and the last thing they said.
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
 *
 * The suites come from the CALLER rather than from the store, deliberately — the open
 * suite's Run button sends the string being edited, and the header's sends every suite
 * that can run (see suite/runAll.ts). Reading them here would tie this provider to
 * TestSuiteProvider for no gain, and would run what was last saved rather than what is on
 * screen.
 */
export interface SuiteRunValue {
  running: boolean;
  /** The last run's report, or null before the first one. */
  outcome: TestRunOutcome | null;
  /** The flows the last run asked for, so the console can name what it is showing. */
  flows: string[];
  /** Suites the caller left out of it, so a green tally cannot hide that they never ran. */
  skipped: SkippedSuite[];
  /** Why the run could not be made at all — distinct from a run whose tests failed. */
  error: string | null;
  run: (targets: TestSuiteInput[], skipped?: SkippedSuite[]) => Promise<void>;
  /** Forget the last report. */
  clear: () => void;
}

const SuiteRunContext = createContext<SuiteRunValue | null>(null);

export function SuiteRunProvider({ children }: { children: ReactNode }) {
  const { state } = useEditorState();
  const run = useRun();
  const consoleTabs = useConsole();
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<TestRunOutcome | null>(null);
  const [flows, setFlows] = useState<string[]>([]);
  const [skipped, setSkipped] = useState<SkippedSuite[]>([]);
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
    setFlows([]);
    setSkipped([]);
  }, []);

  const start = useCallback(
    async (targets: TestSuiteInput[], left: SkippedSuite[] = []) => {
      // Nothing to run is not an error to report: the host would reject the request, and
      // every caller disables its button in this state anyway.
      if (!run || inFlight.current || targets.length === 0) return;
      const runGeneration = generation.current;
      inFlight.current = true;
      setRunning(true);
      setError(null);
      setFlows(targets.map((t) => t.name));
      setSkipped(left);
      // Surface the console on the way in, so the report lands where the user is already
      // looking — the same thing a flow run does with its output.
      consoleTabs.openTo("tests");
      try {
        const resolved = await run.runTests({
          yaml: toRunnableYaml(state.document),
          integrationId: state.integration.id ?? undefined,
          suites: targets,
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

  const value = useMemo<SuiteRunValue>(
    () => ({ running, outcome, flows, skipped, error, run: start, clear }),
    [running, outcome, flows, skipped, error, start, clear],
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
