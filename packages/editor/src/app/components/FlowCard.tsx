"use client";

import { X } from "lucide-react";
import type { FlowDoc } from "../model/document";
import { useEditorState, EditorActionType } from "../state/editorState";
import SourceCard from "./SourceCard";
import SourcePicker from "./SourcePicker";
import FlowView from "./FlowView";
import FlowRunMenu from "./FlowRunMenu";

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
        "group rounded-3xl border-2 border-dashed p-5",
        "bg-white/60 backdrop-blur-md dark:bg-zinc-900/50",
        // `backdrop-blur` makes this card a stacking context, which traps anything inside
        // it *underneath* the card that follows — so a popover opened on one of its nodes
        // is painted over by the next flow. Nothing inside can fix that with a z-index of
        // its own; the card that owns the context has to lift itself, which it does while
        // any popover within it is open (see components/ui/Popover). z-20 and not higher:
        // the canvas toolbar sits at z-30 and must stay above the cards.
        "relative has-[[data-popover-open]]:z-20",
        active ? "border-sky-400/70" : "border-zinc-300 dark:border-zinc-700",
      ].join(" ")}
    >
      <div className="mb-3 flex items-center justify-between">
        {/* Run sits with the flow's name, where the flow is identified — it acts on the
            whole flow, unlike the destructive delete kept over on the right. */}
        <div className="flex items-center gap-1.5">
          <FlowRunMenu flowId={flow.id} />
          <h3 className="font-mono text-xs text-zinc-500">{flow.name}</h3>
        </div>
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
          <SourceCard flowId={flow.id} source={flow.source} />
        ) : (
          <SourcePicker flowId={flow.id} />
        )}
        <div className="my-3 w-full border-t border-dashed border-zinc-300 dark:border-zinc-700" />
        <FlowView
          flow={flow}
          ariaLabel="Flow steps"
          emptyHint="Click or drag a component to build this flow"
        />
        {flow.error && (
          <>
            <div className="my-3 w-full border-t border-dashed border-rose-300/70 dark:border-rose-500/30" />
            <span className="mb-2 self-start font-mono text-[11px] text-rose-500/90 dark:text-rose-400/80">
              on error
            </span>
            <FlowView
              flow={flow.error}
              ariaLabel="Error path"
              emptyHint="Drop a component to handle errors"
            />
          </>
        )}
      </div>
    </section>
  );
}
