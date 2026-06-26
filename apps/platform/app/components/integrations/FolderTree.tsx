"use client";

import { useEffect, useMemo, useState } from "react";
import {
  ChevronRight,
  FolderPlus,
  Folder as FolderIcon,
  Inbox,
  Layers,
  Pencil,
  Trash2,
} from "lucide-react";
import { type Bucket, type FlatFolder, isFolderBucket } from "./model";

/** localStorage key holding the ids of collapsed folders (so new folders default open). */
const COLLAPSED_KEY = "octo.folderTree.collapsed";

/**
 * The folder tree sidebar of the management view: the "All"/"Unfiled" buckets
 * plus the folder tree with inline create/rename and delete. It owns only the
 * transient inline-edit UI state; folder mutations are delegated to the manager
 * via callbacks.
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
        <li>
          <button
            type="button"
            onClick={() => onSelect("all")}
            className={bucketRow(bucket === "all")}
          >
            <Layers size={15} className="text-zinc-400" />
            <span className="flex-1">All integrations</span>
            <span className="text-xs text-zinc-400">{total}</span>
          </button>
        </li>
        <li>
          <button
            type="button"
            onClick={() => onSelect("unfiled")}
            className={bucketRow(bucket === "unfiled")}
          >
            <Inbox size={15} className="text-zinc-400" />
            <span className="flex-1">Unfiled</span>
            <span className="text-xs text-zinc-400">{unfiledCount}</span>
          </button>
        </li>

        {folders.filter(isVisible).map((f) => {
          const expandable = hasChildren.has(f.id);
          const isCollapsed = collapsed.has(f.id);
          // Indent by depth; the chevron column (1rem) keeps folder icons aligned
          // whether or not a row is expandable.
          const indent = `${0.75 + f.depth * 0.85}rem`;
          return (
            <li key={f.id} className="group/row relative">
              {editingId === f.id ? (
                <input
                  autoFocus
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  onBlur={() => submitRename(f)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitRename(f);
                    if (e.key === "Escape") setEditingId(null);
                  }}
                  style={{ paddingLeft: `calc(${indent} + 1rem)` }}
                  className="w-full bg-transparent py-1.5 pr-2 text-sm outline-none ring-1 ring-sky-500/40"
                />
              ) : (
                <div
                  className={`flex w-full items-center pr-14 text-sm ${
                    isFolderBucket(bucket, f.id)
                      ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
                      : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                  }`}
                  style={{ paddingLeft: indent }}
                >
                  {expandable ? (
                    <button
                      type="button"
                      aria-label={isCollapsed ? `Expand ${f.name}` : `Collapse ${f.name}`}
                      aria-expanded={!isCollapsed}
                      onClick={() => toggle(f.id)}
                      className="flex h-4 w-4 shrink-0 items-center justify-center text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200"
                    >
                      <ChevronRight
                        size={13}
                        className={`transition-transform ${isCollapsed ? "" : "rotate-90"}`}
                      />
                    </button>
                  ) : (
                    <span className="h-4 w-4 shrink-0" aria-hidden />
                  )}
                  <button
                    type="button"
                    onClick={() => onSelect({ folder: f.id })}
                    className="flex min-w-0 flex-1 items-center gap-2 py-1.5 pl-1 text-left"
                  >
                    <FolderIcon size={15} className="shrink-0 text-zinc-400" />
                    <span className="flex-1 truncate">{f.name}</span>
                    <span className="text-xs text-zinc-400">{folderCount(f.id)}</span>
                  </button>
                </div>
              )}
              <div className="absolute right-1 top-1/2 flex -translate-y-1/2 items-center opacity-0 transition-opacity group-hover/row:opacity-100">
                <button
                  type="button"
                  aria-label={`Rename ${f.name}`}
                  onClick={() => {
                    setEditingId(f.id);
                    setEditName(f.name);
                  }}
                  className="rounded p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
                >
                  <Pencil size={13} />
                </button>
                <button
                  type="button"
                  aria-label={`Delete ${f.name}`}
                  onClick={() => onDelete(f)}
                  className="rounded p-1 text-zinc-400 hover:text-red-500"
                >
                  <Trash2 size={13} />
                </button>
              </div>
            </li>
          );
        })}

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
