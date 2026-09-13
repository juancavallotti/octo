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
import { newId, type EditorDocument } from "../model/document";
import { toRunnableYaml } from "../model/runConfig";
import { useEditorState } from "../state/editorState";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import type { TestInput } from "../meta/types";
import { debugConflict, planBreakpoint } from "./address";
import { toMockSpecs } from "./debug";
import { useConsole } from "./console";
import { classify, crossings, inputValues, type FlowRunEntry } from "./outcome";
import type { RunSetup } from "../suite/promote";
import type { MockSpec, RunTransport, SpyRecord, SpyTrace } from "./transport";

/**
 * Running ONE flow, and what came back. Separate from RunContext, which owns the
 * long-lived runner: these runs are one-shot, can overlap, and leave a history.
 *
 * The results here are flow *output* — the message a run produced, or the message it
 * was carrying at a breakpoint. Log lines are not results and do not appear.
 */

/** How many runs to keep. Old results are interesting; ancient ones are not. */
const MAX_RESULTS = 50;

/**
 * What one run stands in for and what it watches, REPLACING what the canvas has set up
 * rather than adding to it.
 *
 * Replacement is dolphin's rule (`File.MocksFor`): a test case is a complete statement of
 * the world it runs in, so a caller that hands one over is saying "these mocks, no
 * others". An override that omits a field still replaces it: `{ mocks }` with no `spies`
 * runs with no spies, not with the canvas's.
 */
export interface RunOverrides {
  mocks?: Record<string, MockSpec>;
  spies?: string[];
}

interface FlowRunValue {
  /** Newest first. */
  results: FlowRunEntry[];
  /** Blocks with a run in flight, for their spinners. */
  busyBlockIds: ReadonlySet<string>;
  /** True while any run is in flight. */
  busy: boolean;
  /**
   * What each spied block has seen, by address — ACROSS runs, not just the last one, so
   * a user iterating can compare one run against the next. Records accumulate until
   * cleared.
   */
  spyRecords(address: string): SpyRecord[];
  /** Forget what a spy has collected — or, with no address, what all of them have. */
  clearSpies(address?: string): void;
  /**
   * Run a whole flow, optionally with a saved input, and optionally standing in for the
   * canvas's mocks and spies with {@link RunOverrides} — which is how a suite case is
   * replayed from the ▶ menu.
   */
  runFlow(flowId: string, input?: TestInput, overrides?: RunOverrides): Promise<void>;
  /**
   * Run until execution reaches `blockId`, then stop and report the message there. The
   * flow to run is derived from where the block sits, so a caller holding only a block
   * (a node deep inside a composite) needs to know nothing else. Absent an input, the
   * one its root flow was last run with is reused.
   */
  runToBlock(blockId: string, input?: TestInput): Promise<void>;
  /** The input a flow was last run with, so run-to-here needn't ask again. */
  lastInput(flowId: string): TestInput | undefined;
  clear(): void;
}

const FlowRunContext = createContext<FlowRunValue | null>(null);

export function FlowRunProvider({
  transport,
  children,
}: {
  transport: RunTransport;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const { openTo } = useConsole();
  const meta = useEditorMeta();
  const doc = state.document;
  const integrationId = state.integration.id;

  const [results, setResults] = useState<FlowRunEntry[]>([]);
  const [busyBlockIds, setBusyBlockIds] = useState<ReadonlySet<string>>(new Set());
  const [busyCount, setBusyCount] = useState(0);
  const [spies, setSpies] = useState<ReadonlyMap<string, SpyRecord[]>>(new Map());
  // The last input used per flow, so the block-level play button can reuse it instead
  // of making the user pick one every time they move the breakpoint.
  const lastInputRef = useRef<Map<string, TestInput>>(new Map());

  const record = useCallback(
    (entry: FlowRunEntry) => {
      setResults((prev) => [entry, ...prev].slice(0, MAX_RESULTS));
      // Surface what the user just asked for: the output, or the reason there isn't any.
      openTo(entry.status === "error" || entry.status === "timeout" ? "problems" : "results");
    },
    [openTo],
  );

  /** Append what the spies saw on this run to what they saw on the last one. */
  const collect = useCallback((traces: SpyTrace[] | undefined) => {
    if (!traces || traces.length === 0) return;
    setSpies((prev) => {
      const next = new Map(prev);
      for (const trace of traces) {
        // An empty trace is not nothing: it says the run did not cross the block. But it
        // is not a record either, so it adds none — and must not wipe the ones already
        // there from a run that did.
        if (trace.records.length === 0) continue;
        next.set(trace.address, [...(next.get(trace.address) ?? []), ...trace.records]);
      }
      return next;
    });
  }, []);

  /**
   * The mocks and spies every run is made under, as the canvas currently shows them.
   * Both are sent on EVERY run — a whole-flow run and a run-to-here alike — because a
   * mock is a statement about what the flow does, not about one invocation of it.
   */
  const debugArgs = useCallback(() => {
    const mocks = toMockSpecs(meta?.enabledMocks() ?? []);
    const spyAddresses = meta?.allSpies() ?? [];
    return { mocks, spies: spyAddresses };
  }, [meta]);

  /** Run `doc` and record the outcome. The one place a run is actually made. */
  const run = useCallback(
    async (args: {
      runDoc: EditorDocument;
      flowName: string;
      input?: TestInput;
      breakAt?: string;
      breakpointLabel?: string;
      blockId?: string;
      overrides?: RunOverrides;
    }) => {
      const { runDoc, flowName, input, breakAt, breakpointLabel, blockId } = args;
      // An override replaces the canvas's setup outright — see RunOverrides. Each field
      // falls back to nothing rather than to debugArgs(), or a case that mocks a block
      // but watches none would silently pick up the canvas's spies.
      const source = args.overrides ?? debugArgs();
      const mocks = source.mocks ?? {};
      const spyAddresses = source.spies ?? [];

      // A mock deletes the subtree it replaces, so a spy or breakpoint inside one can
      // never fire and the runtime rejects the run outright. Catch it before spending a
      // run on it, and say what actually happened.
      const observers = [...spyAddresses, ...(breakAt ? [breakAt] : [])];
      const conflict = debugConflict(Object.keys(mocks), observers);
      if (conflict) {
        record({ id: newId(), flowName, inputName: input?.name, breakpointLabel, status: "error", error: conflict });
        return;
      }

      // Everything the run is made with, kept on the result so it can be promoted into a
      // test case. Known before the call, so a run that never came back still carries the
      // setup that provoked it.
      const sent = inputValues(input);
      const ran: RunSetup = {
        ...(sent ? { input: sent } : {}),
        ...(Object.keys(mocks).length > 0 ? { mocks } : {}),
      };

      setBusyCount((n) => n + 1);
      if (blockId) {
        setBusyBlockIds((prev) => new Set(prev).add(blockId));
      }
      try {
        const outcome = await transport.invoke({
          yaml: toRunnableYaml(runDoc),
          flow: flowName,
          integrationId: integrationId ?? undefined,
          data: input?.data,
          vars: input?.vars,
          breakAt,
          ...(spyAddresses.length > 0 ? { spies: spyAddresses } : {}),
          ...(Object.keys(mocks).length > 0 ? { mocks } : {}),
          // Every run is also a chance to learn what these messages look like: a source's
          // synthesized payload, a call's real response — things no reading of the
          // document could tell us.
          learnShapes: true,
        });
        // Spies report even when the flow failed — what a block was carrying when things
        // went wrong is the most useful thing on the screen — so collect before judging.
        collect(outcome.spies);
        // A failed run is as informative as a successful one: the blocks that DID run
        // carried real messages.
        if (outcome.shapes) meta?.learn(outcome.shapes);
        record({
          id: newId(),
          flowName,
          inputName: input?.name,
          breakpointLabel,
          ran: { ...ran, ...crossings(spyAddresses, outcome.spies) },
          ...classify(outcome, breakAt !== undefined),
        });
      } catch (err) {
        record({
          id: newId(),
          flowName,
          inputName: input?.name,
          breakpointLabel,
          ran,
          status: "error",
          error: (err as Error).message,
        });
      } finally {
        setBusyCount((n) => n - 1);
        if (blockId) {
          setBusyBlockIds((prev) => {
            const next = new Set(prev);
            next.delete(blockId);
            return next;
          });
        }
      }
    },
    [transport, integrationId, record, debugArgs, collect, meta],
  );

  const remember = useCallback((flowId: string, input?: TestInput) => {
    if (input) lastInputRef.current.set(flowId, input);
  }, []);

  const runFlow = useCallback(
    async (flowId: string, input?: TestInput, overrides?: RunOverrides) => {
      const flow = doc.flows.find((f) => f.id === flowId);
      if (!flow) return;
      // An overridden run is NOT remembered for run-to-here: only its input could be, and
      // a later run-to-here would then send a test case's body through the canvas's
      // mocks — neither the scenario nor the setup on screen.
      if (!overrides) remember(flowId, input);
      await run({ runDoc: doc, flowName: flow.name, input, overrides });
    },
    [doc, run, remember],
  );

  const runToBlock = useCallback(
    async (blockId: string, input?: TestInput) => {
      // The plan carries its own document: a clone in which the blocks on the path have
      // been made addressable. Run *that*, not the one on screen.
      const plan = planBreakpoint(doc, blockId);
      if (!plan) return;

      // A breakpoint is always reached by running the block's ROOT flow, which is what
      // the plan names — a block nested in a composite belongs to a sub-flow that is not
      // separately runnable, and whose id is not what an input was remembered against.
      const root = doc.flows.find((f) => f.name === plan.flow);
      if (!root) return;

      const chosen = input ?? lastInputRef.current.get(root.id);
      remember(root.id, chosen);
      await run({
        runDoc: plan.doc,
        flowName: plan.flow,
        input: chosen,
        breakAt: plan.address,
        breakpointLabel: plan.label,
        blockId,
      });
    },
    [doc, run, remember],
  );

  const value = useMemo<FlowRunValue>(
    () => ({
      results,
      busyBlockIds,
      busy: busyCount > 0,
      spyRecords: (address) => spies.get(address) ?? [],
      clearSpies: (address) =>
        setSpies((prev) => {
          if (address === undefined) return new Map();
          const next = new Map(prev);
          next.delete(address);
          return next;
        }),
      runFlow,
      runToBlock,
      lastInput: (flowId) => lastInputRef.current.get(flowId),
      clear: () => setResults([]),
    }),
    [results, busyBlockIds, busyCount, spies, runFlow, runToBlock],
  );

  return <FlowRunContext.Provider value={value}>{children}</FlowRunContext.Provider>;
}

/** Flow-run control, or null when no run capability is mounted. */
export function useFlowRun(): FlowRunValue | null {
  return useContext(FlowRunContext);
}
