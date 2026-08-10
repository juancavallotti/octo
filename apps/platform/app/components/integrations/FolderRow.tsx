"use client";

import { useSortable } from "@dnd-kit/sortable";
import { useDroppable } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import {
  ChevronRight,
  Folder as FolderIcon,
  Pencil,
  Trash2,
} from "lucide-react";
import { type DragData, type DropData, type FlatFolder } from "./model";

/**
 * The two row shapes the folder tree renders: a fixed bucket (All / Unfiled) and
 * a folder, which is simultaneously a sortable item, a drop target, and an inline
 * rename field.
 *
 * They live apart from FolderTree because the tree's job is the tree — which rows
 * exist, which are hidden under a collapsed ancestor, what is being dragged — and
 * a row's job is one row. Splitting them also makes FolderRow's prop list explicit
 * at a file boundary rather than implicit halfway down a longer file.
 */

/** A top-level bucket ("All"/"Unfiled") that is also a drop target. */
/** The shared row chrome both a bucket and a folder wear. */
const bucketRow = (active: boolean) =>
  `flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm ${
    active
      ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
      : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
  }`;

export function BucketRow({
  dropId,
  dropData,
  active,
  onClick,
  children,
}: {
  dropId: string;
  dropData: DropData;
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: dropId, data: dropData });
  return (
    <li>
      <button
        ref={setNodeRef}
        type="button"
        onClick={onClick}
        className={`${bucketRow(active)} ${
          isOver ? "ring-1 ring-inset ring-sky-500/60" : ""
        }`}
      >
        {children}
      </button>
    </li>
  );
}

/**
 * One folder row: a drop target (its container) wrapping a chevron toggle and the
 * draggable folder-name button, with hover rename/delete actions. While editing,
 * the name is replaced by an inline input.
 */
export function FolderRow({
  f,
  expandable,
  collapsed,
  selected,
  count,
  editing,
  editName,
  onEditNameChange,
  onSubmitRename,
  onCancelRename,
  onToggle,
  onSelect,
  onStartRename,
  onDelete,
}: {
  f: FlatFolder;
  expandable: boolean;
  collapsed: boolean;
  selected: boolean;
  count: number;
  editing: boolean;
  editName: string;
  onEditNameChange: (v: string) => void;
  onSubmitRename: () => void;
  onCancelRename: () => void;
  onToggle: () => void;
  onSelect: () => void;
  onStartRename: () => void;
  onDelete: () => void;
}) {
  // Indent by depth; the chevron column (1rem) keeps folder icons aligned whether
  // or not a row is expandable.
  const indent = `${0.75 + f.depth * 0.85}rem`;
  // The row is both a sortable item (reorder among siblings) and a drop target
  // (file an integration here, or reparent a folder dragged from another group).
  const data: DragData = { kind: "folder", id: f.id, name: f.name };
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
    isOver,
  } = useSortable({ id: `folder:${f.id}`, data });
  const sortableStyle = { transform: CSS.Transform.toString(transform), transition };

  if (editing) {
    return (
      <li className="relative">
        <input
          autoFocus
          value={editName}
          onChange={(e) => onEditNameChange(e.target.value)}
          onBlur={onSubmitRename}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmitRename();
            if (e.key === "Escape") onCancelRename();
          }}
          style={{ paddingLeft: `calc(${indent} + 1rem)` }}
          className="w-full bg-transparent py-1.5 pr-2 text-sm outline-none ring-1 ring-sky-500/40"
        />
      </li>
    );
  }

  return (
    <li ref={setNodeRef} style={sortableStyle} className="group/row relative">
      <div
        className={`flex w-full items-center pr-14 text-sm ${
          isDragging ? "opacity-40" : ""
        } ${
          selected
            ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
            : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
        } ${isOver ? "ring-1 ring-inset ring-sky-500/60" : ""}`}
        style={{ paddingLeft: indent }}
      >
        {expandable ? (
          <button
            type="button"
            aria-label={collapsed ? `Expand ${f.name}` : `Collapse ${f.name}`}
            aria-expanded={!collapsed}
            onClick={onToggle}
            className="flex h-4 w-4 shrink-0 items-center justify-center text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200"
          >
            <ChevronRight
              size={13}
              className={`transition-transform ${collapsed ? "" : "rotate-90"}`}
            />
          </button>
        ) : (
          <span className="h-4 w-4 shrink-0" aria-hidden />
        )}
        <button
          type="button"
          onClick={onSelect}
          {...attributes}
          {...listeners}
          className="flex min-w-0 flex-1 items-center gap-2 py-1.5 pl-1 text-left"
        >
          <FolderIcon size={15} className="shrink-0 text-zinc-400" />
          <span className="flex-1 truncate">{f.name}</span>
          <span className="text-xs text-zinc-400">{count}</span>
        </button>
      </div>
      <div className="absolute right-1 top-1/2 flex -translate-y-1/2 items-center opacity-0 transition-opacity group-hover/row:opacity-100">
        <button
          type="button"
          aria-label={`Rename ${f.name}`}
          onClick={onStartRename}
          className="rounded p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
        >
          <Pencil size={13} />
        </button>
        <button
          type="button"
          aria-label={`Delete ${f.name}`}
          onClick={onDelete}
          className="rounded p-1 text-zinc-400 hover:text-red-500"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </li>
  );
}
