"use client";

import { useEffect, useMemo, useState } from "react";
import {
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { FolderPlus, Inbox, Layers, Rocket } from "lucide-react";
import { type Bucket, type FlatFolder, isFolderBucket } from "./model";
import { BucketRow, FolderRow, ViewRow } from "./FolderRow";

/** localStorage key holding the ids of collapsed folders (so new folders default open). */
const COLLAPSED_KEY = "octo.folderTree.collapsed";

/**
 * The folder tree sidebar of the management view: the "All"/"Running apps"/
 * "Unfiled" buckets plus the folder tree with inline create/rename and delete. It
 * owns only the transient inline-edit and collapse UI state; folder mutations are
 * delegated to the manager via callbacks. Folders and the filing buckets are drop
 * targets (an integration dragged here is filed/unfiled; a folder dragged here is
 * reparented), and each folder row is itself a drag source. "Running apps" is
 * derived from what is deployed, so it is a view and not a target.
 */
interface Props {
  folders: FlatFolder[];
  bucket: Bucket;
  total: number;
  runningCount: number;
  unfiledCount: number;
  folderCount: (id: string) => number;
  /** True when a new folder would nest under the selected folder. */
  nesting: boolean;
  onSelect: (bucket: Bucket) => void;
  onCreate: (name: string) => void;
  onRename: (folder: FlatFolder, name: string) => void;
  onDelete: (folder: FlatFolder) => void;
}

export default function FolderTree({
  folders,
  bucket,
  total,
  runningCount,
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
        <ViewRow
          active={bucket === "running"}
          onClick={() => onSelect("running")}
        >
          <Rocket size={15} className="text-zinc-400" />
          <span className="flex-1" title="Every integration with a deployment">
            Running apps
          </span>
          <span className="text-xs text-zinc-400">{runningCount}</span>
        </ViewRow>
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
