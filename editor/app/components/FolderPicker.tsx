"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Folder as FolderIcon, FolderOpen } from "lucide-react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import {
  assignIntegration,
  listFolders,
  unassignIntegration,
  type Folder,
} from "@/app/model/orchestrator";

/**
 * Google-docs-style folder picker for the current integration. The trigger shows
 * the current folder (or "No folder"); the popover lists the folder tree and
 * "No folder". Picking applies immediately when the integration is already saved
 * (single-membership move/remove on the orchestrator); for an unsaved draft it
 * just records the choice, which Save applies on first create. Popover behavior
 * (click-outside, Escape) mirrors ConnectionsLauncher.
 */

interface FlatFolder {
  id: string;
  name: string;
  depth: number;
}

/** Depth-first flatten of the folder tree for indented rendering. */
function flatten(folders: Folder[], depth = 0): FlatFolder[] {
  return folders.flatMap((f) => [
    { id: f.id, name: f.name, depth },
    ...flatten(f.children ?? [], depth + 1),
  ]);
}

export default function FolderPicker() {
  const { state, dispatch } = useEditorState();
  const { id: integrationId, folderId } = state.integration;

  const [open, setOpen] = useState(false);
  const [folders, setFolders] = useState<FlatFolder[]>([]);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  // Load the tree once available so the trigger can name the current folder.
  useEffect(() => {
    let cancelled = false;
    listFolders()
      .then((tree) => {
        if (!cancelled) setFolders(flatten(tree));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const currentName =
    folders.find((f) => f.id === folderId)?.name ?? "No folder";

  const pick = async (next: string | null) => {
    setOpen(false);
    setError(null);
    try {
      // Apply to the server only when the integration already exists; otherwise
      // Save will assign the chosen folder when it first creates the row.
      if (integrationId) {
        if (next) await assignIntegration(next, integrationId);
        else if (folderId) await unassignIntegration(folderId, integrationId);
      }
      dispatch({
        type: EditorActionType.SET_INTEGRATION_FOLDER,
        data: { folderId: next },
      });
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label="Folder"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm text-zinc-500 transition-colors hover:border-black/10 hover:text-zinc-800 dark:text-zinc-400 dark:hover:border-white/15 dark:hover:text-zinc-100"
      >
        {folderId ? <FolderIcon size={15} /> : <FolderOpen size={15} />}
        <span className="max-w-[10rem] truncate">{currentName}</span>
      </button>

      {open && (
        <div className="absolute left-0 top-full z-50 mt-2 w-60 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <ul className="max-h-72 overflow-y-auto py-1">
            <li>
              <button
                type="button"
                onClick={() => pick(null)}
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
              >
                <FolderOpen size={16} className="shrink-0 text-zinc-400" />
                <span className="flex-1">No folder</span>
                {!folderId && <Check size={15} className="text-sky-500" />}
              </button>
            </li>
            {folders.map((f) => (
              <li key={f.id}>
                <button
                  type="button"
                  onClick={() => pick(f.id)}
                  style={{ paddingLeft: `${0.75 + f.depth * 0.9}rem` }}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                >
                  <FolderIcon size={16} className="shrink-0 text-zinc-400" />
                  <span className="flex-1 truncate">{f.name}</span>
                  {folderId === f.id && (
                    <Check size={15} className="text-sky-500" />
                  )}
                </button>
              </li>
            ))}
          </ul>
          {error && (
            <p className="border-t border-black/10 px-3 py-2 text-xs text-red-500 dark:border-white/10">
              {error}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
