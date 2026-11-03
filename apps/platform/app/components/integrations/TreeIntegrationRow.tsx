"use client";

import { createElement, useMemo } from "react";
import { useSortable } from "@dnd-kit/sortable";
import type { Integration } from "@/app/model/orchestrator";
import type { DragData } from "./model";
import { iconForIntegration } from "./sourceIcon";
import { treeIndent } from "./FolderRow";

/**
 * One integration in the tree. It is a drag source and, where it is filed, a
 * drop target meaning "where this one is": beside it in its folder, or into its
 * folder, or to the top level. A smart folder's copy is not a drop target: where
 * it sits says nothing about where anything goes. A plain click selects it (a small pointer activation distance keeps
 * clicks and drags distinct).
 */
export default function TreeIntegrationRow({
  rowId,
  integration: i,
  depth,
  running,
  computed,
  selected,
  onSelect,
}: {
  rowId: string;
  integration: Integration;
  depth: number;
  running: boolean;
  /** Listed by a smart folder: draggable out, but no place to drop onto. */
  computed: boolean;
  selected: boolean;
  onSelect: (id: string) => void;
}) {
  const data: DragData = { kind: "integration", id: i.id, name: i.name };
  // The integration's icon: the one it chose, else the one its definition
  // suggests. Memoized because deriving parses the definition, and keyed on the
  // choice as well as the definition — otherwise picking an icon would not show
  // up until something else re-rendered the row.
  const icon = useMemo(
    () => createElement(iconForIntegration(i.icon, i.definition), { size: 15 }),
    [i.icon, i.definition],
  );
  const { attributes, listeners, setNodeRef, isDragging, isOver } = useSortable({
    id: rowId,
    data,
    disabled: { droppable: computed },
  });

  return (
    <li ref={setNodeRef}>
      <button
        type="button"
        onClick={() => onSelect(i.id)}
        {...attributes}
        {...listeners}
        title={`Updated ${new Date(i.lastUpdated).toLocaleString()}`}
        style={{ paddingLeft: `calc(${treeIndent(depth)} + 1rem)` }}
        className={`flex w-full items-center gap-2 py-1.5 pr-3 text-left text-sm ${
          isDragging ? "opacity-40" : ""
        } ${
          selected
            ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
            : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
        } ${isOver ? "ring-1 ring-inset ring-sky-500/60" : ""}`}
      >
        <span
          className={`shrink-0 ${selected ? "text-sky-600 dark:text-sky-400" : "text-zinc-400"}`}
        >
          {icon}
        </span>
        <span className="min-w-0 flex-1 truncate">{i.name}</span>
        {running && (
          <span
            role="img"
            title="Deployed"
            aria-label="Deployed"
            className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500"
          />
        )}
      </button>
    </li>
  );
}
