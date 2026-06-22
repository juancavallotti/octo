"use client";

import { createElement } from "react";
import { Webhook, X } from "lucide-react";
import type { SourceNode } from "@/app/model/document";
import { getSourceSpec, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import FlowNode from "./FlowNode";

/**
 * The source node at the top of a flow. Clicking it selects the source so its
 * settings open in the panel; a hover-revealed corner X removes it, mirroring the
 * step nodes (NodeShell). The label/icon come from the source's schema spec once
 * it has a connector/type, falling back to a generic node otherwise.
 */
export default function SourceCard({
  flowId,
  source,
}: {
  flowId: string;
  source?: SourceNode;
}) {
  const { state, dispatch } = useEditorState();
  const spec =
    source?.connector && source.type
      ? getSourceSpec(source.connector, source.type)
      : undefined;

  const icon = spec
    ? createElement(resolveIcon(spec.icon ?? ""), {
        size: 20,
        className: "text-zinc-600 dark:text-zinc-300",
      })
    : <Webhook size={20} className="text-zinc-600 dark:text-zinc-300" />;
  const label = spec?.label ?? source?.type ?? "Source";
  const sublabel = spec ? source?.connector : "callable by name";

  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Source: ${label}`}
      onClick={(e) => {
        e.stopPropagation();
        dispatch({ type: EditorActionType.SELECT_SOURCE, data: { flowId } });
      }}
      className="cursor-pointer"
    >
      <FlowNode
        icon={icon}
        label={label}
        sublabel={sublabel}
        selected={state.selectedSourceFlowId === flowId}
      >
        <button
          type="button"
          aria-label="Remove source"
          onClick={(e) => {
            e.stopPropagation();
            dispatch({
              type: EditorActionType.REMOVE_SOURCE,
              data: { flowId },
            });
          }}
          className="absolute -right-2 -top-2 rounded-full border border-black/10 bg-white p-0.5 text-zinc-400 opacity-0 shadow-sm transition-opacity hover:text-red-500 group-hover:opacity-100 dark:border-white/15 dark:bg-zinc-900"
        >
          <X size={14} />
        </button>
      </FlowNode>
    </div>
  );
}
