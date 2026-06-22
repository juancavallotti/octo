"use client";

import { useState } from "react";
import {
  FolderPlus,
  Folder as FolderIcon,
  Inbox,
  Layers,
  Pencil,
  Trash2,
} from "lucide-react";
import { type Bucket, type FlatFolder, isFolderBucket } from "./model";

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

        {folders.map((f) => (
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
                style={{ paddingLeft: `${0.75 + f.depth * 0.85}rem` }}
                className="w-full bg-transparent py-1.5 pr-2 text-sm outline-none ring-1 ring-sky-500/40"
              />
            ) : (
              <button
                type="button"
                onClick={() => onSelect({ folder: f.id })}
                style={{ paddingLeft: `${0.75 + f.depth * 0.85}rem` }}
                className={`flex w-full items-center gap-2 py-1.5 pr-14 text-left text-sm ${
                  isFolderBucket(bucket, f.id)
                    ? "bg-sky-500/10 text-sky-700 dark:text-sky-300"
                    : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                }`}
              >
                <FolderIcon size={15} className="shrink-0 text-zinc-400" />
                <span className="flex-1 truncate">{f.name}</span>
                <span className="text-xs text-zinc-400">{folderCount(f.id)}</span>
              </button>
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
        ))}

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
