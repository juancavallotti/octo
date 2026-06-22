"use client";

import { type ReactNode } from "react";
import { GripVertical, X } from "lucide-react";
import { useDraggable } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import type { BlockNode } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import FlowNode from "./FlowNode";

/**
 * The shared draggable wrapper around a node, used by both leaf steps and
 * composites. It renders the FlowNode (icon + label) with a grip (drag-to-move)
 * and a remove button, handles selection, and renders any `children` (a
 * composite's nested sub-flows) below the node.
 */
export default function NodeShell({
  block,
  flowId,
  icon,
  label,
  sublabel,
  children,
}: {
  block: BlockNode;
  flowId: string;
  icon: ReactNode;
  label: string;
  sublabel?: string;
  children?: ReactNode;
}) {
  const { state, dispatch } = useEditorState();
  const { attributes, listeners, setNodeRef, transform, isDragging } =
    useDraggable({ id: block.id, data: { source: "canvas", flowId } });
  const selected = state.selectedBlockId === block.id;
  const style = { transform: CSS.Translate.toString(transform) };

  return (
    <div
      ref={setNodeRef}
      style={style}
      onClick={(e) => {
        e.stopPropagation();
        dispatch({
          type: EditorActionType.SELECT_BLOCK,
          data: { blockId: block.id },
        });
      }}
      className={[
        "flex cursor-pointer flex-col items-center",
        isDragging ? "opacity-50" : "",
      ].join(" ")}
    >
      <FlowNode icon={icon} label={label} sublabel={sublabel} selected={selected}>
        <button
          type="button"
          aria-label="Drag to reorder"
          onClick={(e) => e.stopPropagation()}
          className="absolute -left-5 top-1/2 -translate-y-1/2 cursor-grab touch-none rounded text-zinc-400 opacity-0 transition-opacity hover:text-zinc-600 group-hover:opacity-100 dark:hover:text-zinc-200"
          {...attributes}
          {...listeners}
        >
          <GripVertical size={16} />
        </button>
        <button
          type="button"
          aria-label="Remove step"
          onClick={(e) => {
            e.stopPropagation();
            dispatch({
              type: EditorActionType.REMOVE_BLOCK,
              data: { flowId, blockId: block.id },
            });
          }}
          className="absolute -right-2 -top-2 rounded-full border border-black/10 bg-white p-0.5 text-zinc-400 opacity-0 shadow-sm transition-opacity hover:text-red-500 group-hover:opacity-100 dark:border-white/15 dark:bg-zinc-900"
        >
          <X size={14} />
        </button>
      </FlowNode>
      {children}
    </div>
  );
}
