"use client";

import { Loader2, Play } from "lucide-react";
import { useEditorState } from "../state/editorState";
import { useRun } from "../run/RunContext";
import { useFlowRun } from "../run/FlowRunContext";
import { planBreakpoint } from "../run/breakpoint";

/**
 * "Run to here": run the flow up to this block and show the message it was carrying,
 * without running the rest of the chain. The runtime has been able to do this since
 * `invoke --break-at`; this is the button that asks for it.
 *
 * It reuses the input the flow was last run with, rather than making the user pick one
 * every time they move the breakpoint — moving a breakpoint is something you do over
 * and over, and re-choosing the input each time would make it a chore.
 *
 * It hides itself when the block cannot be addressed unambiguously (planBreakpoint
 * returns null): offering a run that would break on a *different* block would be worse
 * than offering nothing.
 */
export default function BlockRunButton({ blockId }: { blockId: string }) {
  const { state } = useEditorState();
  const run = useRun();
  const flowRun = useFlowRun();

  if (!run?.available || !flowRun) return null;

  const plan = planBreakpoint(state.document, blockId);
  if (!plan) return null;

  const busy = flowRun.busyBlockIds.has(blockId);
  const blocked = !run.validation.ok;

  return (
    <button
      type="button"
      aria-label="Run to here"
      title={
        blocked
          ? "Fix the document's problems before running"
          : `Run ${plan.flow} up to this block and show the message here`
      }
      disabled={blocked || busy}
      onClick={(e) => {
        e.stopPropagation();
        void flowRun.runToBlock(blockId);
      }}
      // Stays visible while the run is in flight, so the spinner does not vanish the
      // moment the pointer leaves the node it belongs to.
      className={`rounded-full border border-black/10 bg-white p-0.5 text-zinc-400 shadow-sm transition-opacity hover:text-emerald-600 disabled:cursor-not-allowed disabled:hover:text-zinc-400 group-hover:opacity-100 dark:border-white/15 dark:bg-zinc-900 dark:hover:text-emerald-400 ${
        busy ? "opacity-100" : "opacity-0"
      }`}
    >
      {busy ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
    </button>
  );
}
