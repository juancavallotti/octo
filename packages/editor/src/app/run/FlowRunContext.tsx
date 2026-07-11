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
import type { TestInput } from "../meta/types";
import { planBreakpoint } from "./breakpoint";
import { useConsole } from "./console";
import type { FlowRunOutcome, RunTransport } from "./transport";

/**
 * Running ONE flow, and what came back.
 *
 * This is deliberately separate from RunContext, which owns the long-lived runner: that
 * one is a single streaming process you start and stop, while these are one-shot, can
 * overlap, and leave a history worth keeping. Sharing a provider would have tangled two
 * unrelated lifecycles.
 *
 * The results here are flow *output* — the message a run produced, or the message it
 * was carrying at a breakpoint. Log lines are not results and do not appear; a manual
 * run is spawned at error level, so what little it prints is a problem, not output.
 */

/** How many runs to keep. Old results are interesting; ancient ones are not. */
const MAX_RESULTS = 50;

export type FlowRunStatus =
  | "running"
  /** The flow completed and produced a message. */
  | "ok"
  /** The flow filtered the message — a block returned nothing. No output, not a failure. */
  | "dropped"
  /** A breakpoint the flow never got to, because it took another branch. Not a failure. */
  | "not-reached"
  /** The flow failed, or the run could not be made. */
  | "error"
  | "timeout";

export interface FlowRunEntry {
  id: string;
  flowName: string;
  /** The input it was run with, when it was run with a saved one. */
  inputName?: string;
  /** The block it was told to stop at, when this was a run-to-here. */
  breakpointLabel?: string;
  status: FlowRunStatus;
  /** The flow's result body, or the message captured at the breakpoint. */
  output?: unknown;
  /** Why it failed — the flow's own error, or why the run could not be made. */
  error?: string;
}

interface FlowRunValue {
  /** Newest first. */
  results: FlowRunEntry[];
  /** Blocks with a run in flight, for their spinners. */
  busyBlockIds: ReadonlySet<string>;
  /** True while any run is in flight. */
  busy: boolean;
  /** Run a whole flow, optionally with a saved input. */
  runFlow(flowId: string, input?: TestInput): Promise<void>;
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

/** Read the flow's result body. `octo invoke` prints it as JSON on stdout. */
function parseOutput(text: string): unknown {
  const trimmed = text.trim();
  if (trimmed === "") return undefined;
  try {
    return JSON.parse(trimmed);
  } catch {
    return trimmed; // not JSON: show it as it came
  }
}

/**
 * Turn what the runner reported into one status plus what to show for it. The three
 * "nothing came back" cases are meaningfully different and must not be flattened into a
 * single failure: a dropped message means a filter rejected it, an unreached breakpoint
 * means the flow took another branch, and only a real error is an error.
 */
function classify(
  outcome: FlowRunOutcome,
  breaking: boolean,
): Pick<FlowRunEntry, "status" | "output" | "error"> {
  if (outcome.timedOut) return { status: "timeout", error: "The flow did not finish in time." };
  if (!outcome.ok) {
    return {
      status: "error",
      // A run that could not be made reports why; otherwise the runner's stderr is the
      // only account we have of what went wrong.
      error: outcome.error ?? outcome.logs.join("\n") ?? "The run failed.",
    };
  }

  if (breaking) {
    const bp = outcome.breakpoint;
    if (!bp) return { status: "error", error: "The runner reported no breakpoint." };
    if (bp.error) return { status: "error", error: bp.error };
    if (!bp.reached) return { status: "not-reached" };
    return { status: "ok", output: bp.message };
  }

  if (outcome.dropped) return { status: "dropped" };
  return { status: "ok", output: parseOutput(outcome.output) };
}

export function FlowRunProvider({
  transport,
  children,
}: {
  transport: RunTransport;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const { openTo } = useConsole();
  const doc = state.document;
  const integrationId = state.integration.id;

  const [results, setResults] = useState<FlowRunEntry[]>([]);
  const [busyBlockIds, setBusyBlockIds] = useState<ReadonlySet<string>>(new Set());
  const [busyCount, setBusyCount] = useState(0);
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

  /** Run `doc` and record the outcome. The one place a run is actually made. */
  const run = useCallback(
    async (args: {
      runDoc: EditorDocument;
      flowName: string;
      input?: TestInput;
      breakAt?: string;
      breakpointLabel?: string;
      blockId?: string;
    }) => {
      const { runDoc, flowName, input, breakAt, breakpointLabel, blockId } = args;

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
        });
        record({
          id: newId(),
          flowName,
          inputName: input?.name,
          breakpointLabel,
          ...classify(outcome, breakAt !== undefined),
        });
      } catch (err) {
        record({
          id: newId(),
          flowName,
          inputName: input?.name,
          breakpointLabel,
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
    [transport, integrationId, record],
  );

  const remember = useCallback((flowId: string, input?: TestInput) => {
    if (input) lastInputRef.current.set(flowId, input);
  }, []);

  const runFlow = useCallback(
    async (flowId: string, input?: TestInput) => {
      const flow = doc.flows.find((f) => f.id === flowId);
      if (!flow) return;
      remember(flowId, input);
      await run({ runDoc: doc, flowName: flow.name, input });
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
      runFlow,
      runToBlock,
      lastInput: (flowId) => lastInputRef.current.get(flowId),
      clear: () => setResults([]),
    }),
    [results, busyBlockIds, busyCount, runFlow, runToBlock],
  );

  return <FlowRunContext.Provider value={value}>{children}</FlowRunContext.Provider>;
}

/** Flow-run control, or null when no run capability is mounted. */
export function useFlowRun(): FlowRunValue | null {
  return useContext(FlowRunContext);
}
