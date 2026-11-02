"use client";

import { useDndContext, useDroppable } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import {
  ChevronRight,
  Folder as FolderIcon,
  FolderOpen,
  FolderPlus,
  Pencil,
  Trash2,
} from "lucide-react";
import { type DragData, type FlatFolder } from "./model";

/**
 * The folder rows of the integrations tree, and the top-level drop zone beneath
 * them.
 */

/** How far a row at `depth` is indented, shared so folders and integrations line up. */
export function treeIndent(depth: number): string {
  return `${0.75 + depth * 0.85}rem`;
}

/**
 * One folder: a sortable item (reorder among siblings), a drop target (file an
 * integration here, or reparent a folder dragged from another group) and, while
 * renaming, an inline field. Clicking it opens or closes it.
 */
export function FolderRow({
  f,
  expandable,
  expanded,
  count,
  editing,
  editName,
  onEditNameChange,
  onSubmitRename,
  onCancelRename,
  onToggle,
  onStartRename,
  onNewSubfolder,
  onDelete,
}: {
  f: FlatFolder;
  expandable: boolean;
  expanded: boolean;
  count: number;
  editing: boolean;
  editName: string;
  onEditNameChange: (v: string) => void;
  onSubmitRename: () => void;
  onCancelRename: () => void;
  onToggle: () => void;
  onStartRename: () => void;
  onNewSubfolder: () => void;
  onDelete: () => void;
}) {
  const indent = treeIndent(f.depth);
  const data: DragData = { kind: "folder", id: f.id, name: f.name };
  const { attributes, listeners, setNodeRef, isDragging, isOver } = useSortable({
    id: `folder:${f.id}`,
    data,
  });

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

  const Icon = expanded && expandable ? FolderOpen : FolderIcon;
  return (
    <li ref={setNodeRef} className="group/row relative">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expandable ? expanded : undefined}
        {...attributes}
        {...listeners}
        style={{ paddingLeft: indent }}
        className={`flex w-full items-center gap-1 py-1.5 pr-20 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06] ${
          isDragging ? "opacity-40" : ""
        } ${isOver ? "ring-1 ring-inset ring-sky-500/60" : ""}`}
      >
        {expandable ? (
          <ChevronRight
            size={13}
            aria-hidden
            className={`shrink-0 text-zinc-400 transition-transform ${expanded ? "rotate-90" : ""}`}
          />
        ) : (
          <span className="h-[13px] w-[13px] shrink-0" aria-hidden />
        )}
        <Icon size={15} className="ml-0.5 shrink-0 text-zinc-400" />
        <span className="ml-1 min-w-0 flex-1 truncate font-medium">{f.name}</span>
        <span className="text-xs text-zinc-400">{count}</span>
      </button>
      <div className="absolute right-1 top-1/2 flex -translate-y-1/2 items-center opacity-0 transition-opacity group-hover/row:opacity-100 focus-within:opacity-100">
        <RowAction label={`New folder in ${f.name}`} onClick={onNewSubfolder}>
          <FolderPlus size={13} />
        </RowAction>
        <RowAction label={`Rename ${f.name}`} onClick={onStartRename}>
          <Pencil size={13} />
        </RowAction>
        <RowAction label={`Delete ${f.name}`} onClick={onDelete} danger>
          <Trash2 size={13} />
        </RowAction>
      </div>
    </li>
  );
}

function RowAction({
  label,
  onClick,
  danger,
  children,
}: {
  label: string;
  onClick: () => void;
  danger?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={`rounded p-1 text-zinc-400 ${
        danger ? "hover:text-red-500" : "hover:text-zinc-700 dark:hover:text-zinc-200"
      }`}
    >
      {children}
    </button>
  );
}

/**
 * The top level as a place to drop: an integration dropped here leaves its folder,
 * a folder dropped here moves to the top. It fills the space under the rows, and
 * says what it does only while something is being dragged.
 */
export function TopLevelDropZone() {
  const { active } = useDndContext();
  const { setNodeRef, isOver } = useDroppable({ id: "tree:root", data: { kind: "root" } });
  return (
    <li
      ref={setNodeRef}
      className={`flex min-h-12 flex-1 items-start px-3 py-2 text-xs ${
        isOver ? "bg-sky-500/5 text-sky-600 ring-1 ring-inset ring-sky-500/60" : "text-zinc-400"
      }`}
    >
      {active ? "Drop here to move to the top level" : null}
    </li>
  );
}
