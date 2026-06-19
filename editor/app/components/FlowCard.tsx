"use client";

import { Plus } from "lucide-react";
import type { FlowDoc } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import SourceCard from "./SourceCard";
import FlowView from "./FlowView";

/**
 * One flow drawn as the schematic in the brief: a dashed container labelled with
 * the flow name, a source node up top, a dashed divider, then the process nodes
 * connected by downward arrows with a drop target between each. Clicking the card
 * makes it the active flow (the click-to-add target).
 */
export default function FlowCard({
  flow,
  active,
}: {
  flow: FlowDoc;
  active: boolean;
}) {
  const { dispatch } = useEditorState();

  return (
    <section
      aria-label={flow.name}
      onClick={() =>
        dispatch({
          type: EditorActionType.SET_ACTIVE_FLOW,
          data: { flowId: flow.id },
        })
      }
      className={[
        "rounded-3xl border-2 border-dashed bg-black/[0.015] dark:bg-white/[0.02] p-5",
        active ? "border-sky-400/70" : "border-zinc-300 dark:border-zinc-700",
      ].join(" ")}
    >
      <h3 className="mb-3 font-mono text-xs text-zinc-500">{flow.name}</h3>
      <div className="flex flex-col items-center">
        {flow.source ? (
          <SourceCard source={flow.source} />
        ) : (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              dispatch({
                type: EditorActionType.ADD_SOURCE,
                data: { flowId: flow.id },
              });
            }}
            className="flex items-center gap-1.5 rounded-full border border-dashed border-zinc-300 px-3 py-1.5 text-sm text-zinc-500 transition-colors hover:border-zinc-400 hover:text-zinc-700 dark:border-zinc-700 dark:hover:border-zinc-500 dark:hover:text-zinc-300"
          >
            <Plus size={14} />
            Add source
          </button>
        )}
        <div className="my-3 w-full border-t border-dashed border-zinc-300 dark:border-zinc-700" />
        <FlowView
          flow={flow}
          ariaLabel="Flow steps"
          emptyHint="Click or drag a component to build this flow"
        />
      </div>
    </section>
  );
}
