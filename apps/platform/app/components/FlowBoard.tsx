"use client";

import { Plus } from "lucide-react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import FlowCard from "./FlowCard";

/**
 * The board holds every flow in the file, stacked vertically. New blocks from
 * the palette land in the active flow; each flow also accepts drops directly. An
 * "Add flow" button appends a fresh flow.
 */
export default function FlowBoard() {
  const { state, dispatch } = useEditorState();

  return (
    <div className="mx-auto flex w-fit min-w-[28rem] max-w-full flex-col gap-4 p-6">
      {state.document.flows.map((flow) => (
        <FlowCard
          key={flow.id}
          flow={flow}
          active={flow.id === state.activeFlowId}
        />
      ))}
      <button
        type="button"
        onClick={() => dispatch({ type: EditorActionType.ADD_FLOW })}
        className="flex items-center justify-center gap-2 rounded-xl border border-dashed border-black/15 dark:border-white/20 px-3 py-3 text-sm text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300 hover:border-black/30 dark:hover:border-white/30 transition-colors"
      >
        <Plus size={16} />
        Add flow
      </button>
    </div>
  );
}
