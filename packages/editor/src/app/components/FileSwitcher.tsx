"use client";

import { useEffect, useRef, useState } from "react";
import { FileText, Pencil, Plus, X } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { isConfigFileName } from "../model/project";
import { BAR_BUTTON, BAR_COUNT } from "./barButton";

/**
 * The project's config files, in the document bar. The button names the open file;
 * its popover lists every file — picking one opens it, keeping any unsaved edits in
 * the one left — and offers to add a file, and to rename or remove the open one.
 * Nothing touches the store until Save, which writes the files as a set.
 */
export default function FileSwitcher() {
  const { state, dispatch } = useEditorState();
  const [open, setOpen] = useState(false);
  // The name being typed, and whether it names a new file or the open one's new name.
  const [draft, setDraft] = useState<{ mode: "create" | "rename"; name: string } | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const paths = state.files.map((f) => f.path).sort();

  const close = () => {
    setOpen(false);
    setDraft(null);
  };

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // A typed name without an extension means a YAML file: that is the only kind there is.
  const target = draft ? withExtension(draft.name.trim()) : "";
  const problem = !draft || target === "" ? null : nameProblem(target, paths, draft.mode === "rename" ? state.activePath : null);

  const commit = () => {
    if (!draft || target === "" || problem) return;
    if (draft.mode === "create") {
      dispatch({ type: EditorActionType.CREATE_FILE, data: { path: target } });
    } else if (target !== state.activePath) {
      dispatch({ type: EditorActionType.RENAME_FILE, data: { from: state.activePath, to: target } });
    }
    close();
  };

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label="Files"
        onClick={() => (open ? close() : setOpen(true))}
        className={BAR_BUTTON}
      >
        <FileText size={15} />
        {state.activePath}
        {paths.length > 1 && <span className={BAR_COUNT}>{paths.length}</span>}
      </button>

      {open && (
        <div className="absolute left-0 top-full z-40 mt-2 w-64 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <ul className="max-h-72 overflow-y-auto py-1">
            {paths.map((path) => {
              const active = path === state.activePath;
              return (
                <li key={path} className="group/row relative">
                  <button
                    type="button"
                    onClick={() => {
                      dispatch({ type: EditorActionType.SWITCH_FILE, data: { path } });
                      close();
                    }}
                    className={`flex w-full items-center gap-2.5 px-3 py-2 pr-14 text-left text-sm transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06] ${
                      active ? "bg-sky-500/10 font-medium" : ""
                    }`}
                  >
                    <FileText size={15} className="shrink-0 text-zinc-500" />
                    <span className="truncate">{path}</span>
                  </button>
                  {active && (
                    <span className="absolute right-2 top-1/2 flex -translate-y-1/2 gap-0.5 opacity-0 transition-opacity group-hover/row:opacity-100">
                      <button
                        type="button"
                        aria-label={`Rename ${path}`}
                        onClick={() => setDraft({ mode: "rename", name: path })}
                        className="rounded-full p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
                      >
                        <Pencil size={13} />
                      </button>
                      {paths.length > 1 && (
                        <button
                          type="button"
                          aria-label={`Remove ${path}`}
                          onClick={() => {
                            dispatch({ type: EditorActionType.DELETE_FILE, data: { path } });
                            close();
                          }}
                          className="rounded-full p-1 text-zinc-400 hover:text-red-500"
                        >
                          <X size={14} />
                        </button>
                      )}
                    </span>
                  )}
                </li>
              );
            })}
          </ul>

          <div className="border-t border-black/10 dark:border-white/10">
            {draft ? (
              <form
                className="px-3 py-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  commit();
                }}
              >
                <input
                  autoFocus
                  aria-label={draft.mode === "create" ? "New file name" : "File name"}
                  value={draft.name}
                  placeholder="orders.yaml"
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-sky-500 dark:border-white/15"
                />
                {problem && <p className="mt-1 text-xs text-rose-500">{problem}</p>}
              </form>
            ) : (
              <button
                type="button"
                onClick={() => setDraft({ mode: "create", name: "" })}
                className="flex w-full items-center gap-1.5 px-3 py-2 text-sm text-zinc-500 transition-colors hover:bg-black/[0.04] hover:text-zinc-700 dark:hover:bg-white/[0.06] dark:hover:text-zinc-300"
              >
                <Plus size={14} />
                New file
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function withExtension(name: string): string {
  return name === "" || /\.ya?ml$/i.test(name) ? name : `${name}.yaml`;
}

/** Why a typed name cannot be used, or null when it can. */
function nameProblem(path: string, taken: string[], renaming: string | null): string | null {
  if (path.includes("/") || path.includes("\\")) return "A config file lives at the project root.";
  if (!isConfigFileName(path)) return "A file ending in _test is a test suite, not config.";
  if (path !== renaming && taken.includes(path)) return "A file with that name already exists.";
  return null;
}
