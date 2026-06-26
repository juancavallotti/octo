"use client";

import { useEffect, useMemo, useState } from "react";
import { useDroppable } from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ChevronRight,
  FolderPlus,
  Folder as FolderIcon,
  Inbox,
  Layers,
  Pencil,
  Trash2,
} from "lucide-react";
import {
  type Bucket,
  type DragData,
  type DropData,
  type FlatFolder,
  isFolderBucket,
} from "./model";

/** localStorage key holding the ids of collapsed folders (so new folders default open). */
const COLLAPSED_KEY = "octo.folderTree.collapsed";

/**
 * The folder tree sidebar of the management view: the "All"/"Unfiled" buckets
 * plus the folder tree with inline create/rename and delete. It owns only the
 * transient inline-edit and collapse UI state; folder mutations are delegated to
 * the manager via callbacks. Folders and the buckets are drop targets (an
 * integration dragged here is filed/unfiled; a folder dragged here is reparented),
 * and each folder row is itself a drag source.
 */
interface Props {
  folders: FlatFolder[];
  bucket: Bucket;
  total: number;
  unfiledCount: number;
  folderCount: (id: string) => number;
  /** True when a new folder would nest under the selected folder. */
  nesting: boolean;
  onSelect: (bucket: Bucket) => void;
  onCreate: (name: string) => void;
  onRename: (folder: FlatFolder, name: string) => void;
  onDelete: (folder: FlatFolder) => void;
}

const bucketRow = (active: boolean) =>
  `flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm ${
    active
      ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
      : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
  }`;

export default function FolderTree({
  folders,
  bucket,
  total,
  unfiledCount,
  folderCount,
  nesting,
  onSelect,
  onCreate,
  onRename,
  onDelete,
}: Props) {
  const [creating, setCreating] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  // Collapsed folder ids. Stored (not "expanded") so folders created later default
  // open. Hydrated from localStorage after mount to avoid an SSR mismatch.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  useEffect(() => {
    try {
      const raw = localStorage.getItem(COLLAPSED_KEY);
      // Loading persisted UI state on mount is intentional: reading localStorage
      // during render would mismatch the server-rendered (empty) markup, so it has
      // to happen in an effect after hydration.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (raw) setCollapsed(new Set(JSON.parse(raw) as string[]));
    } catch {
      // ignore malformed/blocked storage
    }
  }, []);

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      try {
        localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]));
      } catch {
        // ignore blocked storage
      }
      return next;
    });

  // Which folders have children (so only those show a chevron), and the parent of
  // each folder, used to hide rows whose ancestor is collapsed.
  const { hasChildren, parentOf } = useMemo(() => {
    const hasChildren = new Set<string>();
    const parentOf = new Map<string, string | null>();
    for (const f of folders) {
      parentOf.set(f.id, f.parentId);
      if (f.parentId) hasChildren.add(f.parentId);
    }
    return { hasChildren, parentOf };
  }, [folders]);

  // A row is hidden when any ancestor is collapsed.
  const isVisible = (f: FlatFolder): boolean => {
    let p = f.parentId;
    while (p) {
      if (collapsed.has(p)) return false;
      p = parentOf.get(p) ?? null;
    }
    return true;
  };
  const visible = folders.filter(isVisible);

  const submitCreate = () => {
    const name = draftName.trim();
    setCreating(false);
    setDraftName("");
    if (name) onCreate(name);
  };

  const submitRename = (f: FlatFolder) => {
    const name = editName.trim();
    setEditingId(null);
    if (name && name !== f.name) onRename(f, name);
  };

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
      <div className="flex items-center justify-between px-3 py-2">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-400">
          Folders
        </span>
        <button
          type="button"
          onClick={() => {
            setCreating(true);
            setDraftName("");
          }}
          title="New folder"
          className="rounded p-1 text-zinc-400 hover:bg-black/[0.04] hover:text-zinc-700 dark:hover:bg-white/[0.06]"
        >
          <FolderPlus size={16} />
        </button>
      </div>

      <ul className="min-h-0 flex-1 overflow-y-auto pb-2">
        <BucketRow
          dropId="bucket:root"
          dropData={{ kind: "root" }}
          active={bucket === "all"}
          onClick={() => onSelect("all")}
        >
          <Layers size={15} className="text-zinc-400" />
          <span className="flex-1">All integrations</span>
          <span className="text-xs text-zinc-400">{total}</span>
        </BucketRow>
        <BucketRow
          dropId="bucket:unfiled"
          dropData={{ kind: "unfiled" }}
          active={bucket === "unfiled"}
          onClick={() => onSelect("unfiled")}
        >
          <Inbox size={15} className="text-zinc-400" />
          <span className="flex-1">Unfiled</span>
          <span className="text-xs text-zinc-400">{unfiledCount}</span>
        </BucketRow>

        <SortableContext
          items={visible.map((f) => `folder:${f.id}`)}
          strategy={verticalListSortingStrategy}
        >
          {visible.map((f) => (
            <FolderRow
              key={f.id}
              f={f}
              expandable={hasChildren.has(f.id)}
              collapsed={collapsed.has(f.id)}
              selected={isFolderBucket(bucket, f.id)}
              count={folderCount(f.id)}
              editing={editingId === f.id}
              editName={editName}
              onEditNameChange={setEditName}
              onSubmitRename={() => submitRename(f)}
              onCancelRename={() => setEditingId(null)}
              onToggle={() => toggle(f.id)}
              onSelect={() => onSelect({ folder: f.id })}
              onStartRename={() => {
                setEditingId(f.id);
                setEditName(f.name);
              }}
              onDelete={() => onDelete(f)}
            />
          ))}
        </SortableContext>

        {creating && (
          <li>
            <input
              autoFocus
              value={draftName}
              placeholder={nesting ? "New subfolder…" : "New folder…"}
              onChange={(e) => setDraftName(e.target.value)}
              onBlur={submitCreate}
              onKeyDown={(e) => {
                if (e.key === "Enter") submitCreate();
                if (e.key === "Escape") {
                  setCreating(false);
                  setDraftName("");
                }
              }}
              className="w-full bg-transparent px-3 py-1.5 text-sm outline-none ring-1 ring-sky-500/40"
            />
          </li>
        )}
      </ul>
    </aside>
  );
}

/** A top-level bucket ("All"/"Unfiled") that is also a drop target. */
function BucketRow({
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
function FolderRow({
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
