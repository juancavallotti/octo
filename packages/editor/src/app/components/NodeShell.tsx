"use client";

import { type MouseEvent, type ReactNode } from "react";
import { GripVertical, X } from "lucide-react";
import { useDraggable } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import type { BlockNode } from "../model/document";
import { useEditorState, EditorActionType } from "../state/editorState";
import FlowNode from "./FlowNode";

/**
 * The shared draggable wrapper around a node, used by both leaf steps and
 * composites. It owns selection plus the drag-to-move grip and remove button so
 * that behaviour lives in one place.
 *
 * Leaf steps render as the detached FlowNode pill (icon + label) with `children`
 * (a composite's nested sub-flows) below it. Composites pass `boxed` to instead
 * bake the icon + title into the top-centre of a single bordered box that holds
 * its slots (`children`) directly — a more compact scope shape.
 */
export default function NodeShell({
  block,
  flowId,
  icon,
  label,
  sublabel,
  boxed = false,
  children,
}: {
  block: BlockNode;
  flowId: string;
  icon: ReactNode;
  label: string;
  sublabel?: string;
  boxed?: boolean;
  children?: ReactNode;
}) {
  const { state, dispatch } = useEditorState();
  const { attributes, listeners, setNodeRef, transform, isDragging } =
    useDraggable({ id: block.id, data: { source: "canvas", flowId } });
  const selected = state.selectedBlockId === block.id;
  const style = { transform: CSS.Translate.toString(transform) };

  const select = (e: MouseEvent) => {
    e.stopPropagation();
    dispatch({
      type: EditorActionType.SELECT_BLOCK,
      data: { blockId: block.id },
    });
  };

  const grip = (
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
  );

  const remove = (
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
  );

  if (boxed) {
    const ring = selected
      ? "border-sky-500"
      : "border-zinc-800 dark:border-zinc-300";
    return (
      <div
        ref={setNodeRef}
        style={style}
        onClick={select}
        className={[
          "group relative flex cursor-pointer flex-col rounded-2xl border-2 bg-white px-3 pb-3 pt-2 shadow-sm dark:bg-zinc-900",
          ring,
          isDragging ? "opacity-50" : "",
        ].join(" ")}
      >
        {grip}
        {remove}
        <div className="mb-2 flex items-center justify-center gap-2">
          {icon}
          <span className="whitespace-nowrap text-sm font-semibold leading-none">
            {label}
          </span>
          {sublabel && (
            <span className="whitespace-nowrap rounded-full bg-black/[0.06] px-2 py-0.5 text-xs leading-none text-zinc-500 dark:bg-white/10 dark:text-zinc-400">
              {sublabel}
            </span>
          )}
        </div>
        {children}
      </div>
    );
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      onClick={select}
      className={[
        "flex cursor-pointer flex-col items-center",
        isDragging ? "opacity-50" : "",
      ].join(" ")}
    >
      <FlowNode icon={icon} label={label} sublabel={sublabel} selected={selected}>
        {grip}
        {remove}
      </FlowNode>
      {children}
    </div>
  );
}
