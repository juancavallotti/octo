"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Check, ChevronDown, FilePlus, FolderOpen } from "lucide-react";
import {
  BAR_BUTTON,
  useFileSystem,
  useEditorState,
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
 */
export default function StandaloneFileMenu() {
  const fs = useFileSystem();
  const { state, dispatch } = useEditorState();
  const router = useRouter();
  const current = state.integration.id;
  const [open, setOpen] = useState(false);
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

  return (
    <div ref={ref} className="relative">
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
  );
}
