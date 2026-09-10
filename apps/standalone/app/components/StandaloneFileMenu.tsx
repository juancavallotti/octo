"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Check, ChevronDown, FilePlus, FolderOpen, Pencil } from "lucide-react";
import {
  BAR_BUTTON,
  useFileSystem,
  useEditorState,
  useSave,
  EditorActionType,
  type StoredDocument,
} from "@octo/editor";

/**
 * Open/new menu for the standalone editor. Lists the `*.yaml` flows in the local
 * store (via the filesystem capability) and links to `/?file=<id>` to open one.
 * The currently-open file comes from editor state (the id the Save button records
 * after a save), not the URL, so a freshly-saved flow shows up immediately; before
 * the first save there is no id, and the trigger says so.
 * "New flow" clears the editor to a blank document. Renders nothing without a
 * filesystem capability.
 *
 * It sits at the right of the document bar (EditorRoot's `files` slot), so it
 * wears the bar's own trigger look and its menu hangs off the right edge.
 *
 * The pencil renames the open file. The name being edited is the document's title
 * — the standalone store derives the filename from its slug — so a rename here is
 * an edit plus a save, and the store moves the file and hands back the new id. It
 * lives beside the filename because that is the thing being renamed; the header
 * used to carry a title field that renamed the file as a side effect of typing in
 * it, which is a surprising way to move a file on someone's disk.
 */
export default function StandaloneFileMenu() {
  const fs = useFileSystem();
  const { state, dispatch } = useEditorState();
  const save = useSave();
  const router = useRouter();
  const current = state.integration.id;
  const [open, setOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  // What the name was when the rename started, so Escape can put it back. Edits go
  // straight into editor state (as the header field did), which is what keeps the
  // save controller from reading a stale name when Enter asks it to write.
  const before = useRef(state.integration.name);
  const [files, setFiles] = useState<StoredDocument[]>([]);
  const ref = useRef<HTMLDivElement>(null);

  // Refresh the list whenever the menu opens, so a just-saved file shows up.
  useEffect(() => {
    if (!open || !fs?.list) return;
    fs.list()
      .then(setFiles)
      .catch(() => {});
  }, [open, fs]);

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

  if (!fs) return null;

  // Reset the canvas to a fresh, unsaved flow and drop the ?file= query (without
  // a reload — the editor is already mounted).
  const newFlow = () => {
    dispatch({ type: EditorActionType.NEW_INTEGRATION });
    router.replace("/");
    setOpen(false);
  };

  const startRename = () => {
    before.current = state.integration.name;
    setOpen(false);
    setRenaming(true);
  };

  const rename = (name: string) =>
    dispatch({ type: EditorActionType.SET_INTEGRATION_TITLE, data: { name } });

  // Committing is just a save: the name is already in editor state, and the store
  // renames the file when the slug it derives no longer matches. Forced, because
  // naming a flow that has not been drawn yet is still a request for a file — the
  // save controller's "nothing worth persisting" rule is about the Save button, not
  // about this.
  const commitRename = () => {
    setRenaming(false);
    if (!state.integration.name.trim()) rename(before.current);
    else void save?.save({ force: true });
  };

  return (
    <div ref={ref} className="flex items-center">
      {/* Renaming replaces the switcher rather than sitting beside it: the field
          holds the same name the switcher shows, and two of them at once reads as
          two different files. */}
      {renaming ? (
        <input
          type="text"
          autoFocus
          aria-label="File name"
          value={state.integration.name}
          placeholder="untitled-file"
          onChange={(e) => rename(e.target.value)}
          onBlur={commitRename}
          onFocus={(e) => e.currentTarget.select()}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
            if (e.key === "Escape") {
              rename(before.current);
              setRenaming(false);
            }
          }}
          className="w-48 rounded-md border border-black/20 bg-transparent px-2 py-1 text-[13px] outline-none dark:border-white/25"
        />
      ) : (
        <div className="relative">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className={BAR_BUTTON}
          >
            <FolderOpen size={15} className="shrink-0 text-zinc-400" />
            {/* An unsaved draft has no id, and "Open" named the menu rather than what
            is in the editor. The bar's job here is to say which file you are
            looking at, and a draft is a file that does not have a name yet. */}
            <span
              className={`max-w-[12rem] truncate ${current ? "" : "text-zinc-400 dark:text-zinc-500"}`}
            >
              {current ?? "untitled-file"}
            </span>
            <ChevronDown size={14} className="shrink-0 text-zinc-400" />
          </button>

          {open && (
            <div className="absolute right-0 top-full z-50 mt-2 w-64 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
              <button
                type="button"
                onClick={newFlow}
                className="flex w-full items-center gap-2 border-b border-black/5 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:border-white/5 dark:hover:bg-white/[0.06]"
              >
                <FilePlus size={16} className="shrink-0 text-zinc-400" />
                <span className="flex-1">New flow</span>
              </button>
              <ul className="max-h-72 overflow-y-auto py-1">
                {files.length === 0 && (
                  <li className="px-3 py-2 text-xs text-zinc-400">
                    No saved flows yet
                  </li>
                )}
                {files.map((f) => (
                  <li key={f.id}>
                    <Link
                      href={`/?file=${encodeURIComponent(f.id)}`}
                      onClick={() => setOpen(false)}
                      className="flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                    >
                      <span className="flex-1 truncate">{f.name}</span>
                      {current === f.id && (
                        <Check size={15} className="text-sky-500" />
                      )}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      <button
        type="button"
        aria-label="Rename file"
        onClick={startRename}
        className={`${BAR_BUTTON} px-1`}
      >
        <Pencil size={13} className="text-zinc-400" />
      </button>
    </div>
  );
}
