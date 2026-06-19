"use client";

import { X } from "lucide-react";
import type { FlowDoc } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import SourceCard from "./SourceCard";
import SourcePicker from "./SourcePicker";
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
        "group rounded-3xl border-2 border-dashed bg-black/[0.015] dark:bg-white/[0.02] p-5",
        active ? "border-sky-400/70" : "border-zinc-300 dark:border-zinc-700",
      ].join(" ")}
    >
      <div className="mb-3 flex items-center justify-between">
        <h3 className="font-mono text-xs text-zinc-500">{flow.name}</h3>
        <button
          type="button"
          aria-label="Delete flow"
          onClick={(e) => {
            e.stopPropagation();
            dispatch({
              type: EditorActionType.REMOVE_FLOW,
              data: { flowId: flow.id },
            });
          }}
          className="rounded-full p-0.5 text-zinc-400 opacity-0 transition-opacity hover:text-red-500 group-hover:opacity-100"
        >
          <X size={14} />
        </button>
      </div>
      <div className="flex flex-col items-center">
        {flow.source ? (
          <SourceCard source={flow.source} />
        ) : (
          <SourcePicker flowId={flow.id} />
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
