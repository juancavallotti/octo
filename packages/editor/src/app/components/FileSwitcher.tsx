"use client";

import { useEffect, useRef, useState } from "react";
import { Cable, FileText, Pencil, Plus, Trash2, Variable, type LucideIcon } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { projectFiles } from "../state/projectHandlers";
import { isConfigFileName } from "../model/project";
import { resourceCount, type EditorDocument } from "../model/document";
import { BAR_BUTTON, BAR_COUNT } from "./barButton";

/**
 * The project's config files, in the document bar. The button names the open file;
 * its popover lists every file — picking one opens it, keeping any unsaved edits in
 * the one left — and offers to add a file, and to rename or remove any, in its row.
 * The pencil beside the button renames the open file in the bar itself.
 * Each row counts the connections, env vars and resources its file declares, so the
 * file that owns a piece of config can be found without opening them one by one.
 * Nothing touches the store until Save, which writes the files as a set.
 */
export default function FileSwitcher() {
  const { state, dispatch } = useEditorState();
  const [open, setOpen] = useState(false);
  // The name being typed: a new file's, or the new name of the file at `from`.
  const [draft, setDraft] = useState<Draft | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const files = projectFiles(state);
  const paths = files.map((f) => f.path);

  const close = () => {
    setOpen(false);
    setDraft(null);
  };

  // A typed name without an extension means a YAML file: that is the only kind there is.
  const target = draft ? withExtension(draft.name.trim()) : "";
  const problem = !draft || target === "" ? null : nameProblem(target, paths, draft.mode === "rename" ? draft.from : null);

  /**
   * Apply a rename in progress and end it, as leaving its field would; a name that
   * cannot be used stays in the field instead. Anything that takes the field away — a
   * click elsewhere, a host's "new file" — settles it first. A ref, so the listeners
   * below, bound once per opening, see this render's draft.
   */
  const settleRename = useRef(() => {});
  useEffect(() => {
    settleRename.current = () => {
      if (draft?.mode !== "rename" || problem) return;
      if (target !== "" && target !== draft.from) {
        dispatch({ type: EditorActionType.RENAME_FILE, data: { from: draft.from, to: target } });
      }
      setDraft(null);
    };
  });

  // A "new file" asked for from outside (a host's menu) opens the menu on the name.
  useEffect(() => {
    if (state.pendingNew?.kind !== "file") return;
    settleRename.current();
    setOpen(true);
    setDraft({ mode: "create", name: "" });
    dispatch({ type: EditorActionType.CONSUME_NEW_ITEM });
  }, [state.pendingNew, dispatch]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current?.contains(e.target as Node)) return;
      settleRename.current();
      close();
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

  const commit = () => {
    if (!draft || target === "" || problem) return;
    if (draft.mode === "create") {
      dispatch({ type: EditorActionType.CREATE_FILE, data: { path: target } });
      close();
      return;
    }
    // A rename leaves the menu open on the renamed row, like the rest of the list.
    if (target !== draft.from) {
      dispatch({ type: EditorActionType.RENAME_FILE, data: { from: draft.from, to: target } });
    }
    setDraft(null);
  };

  const nameInput = (label: string, floatingProblem = false) =>
    draft && (
      <NameInput
        label={label}
        draft={draft}
        problem={problem}
        floatingProblem={floatingProblem}
        onChange={(name) => setDraft({ ...draft, name })}
        onCommit={commit}
        onCancel={() => setDraft(null)}
      />
    );

  return (
    <div ref={ref} className="relative flex items-center">
      {/* A rename begun from the bar edits the name where it is shown; the menu stays shut. */}
      {!open && draft?.mode === "rename" ? (
        <div className="flex items-center gap-1.5 px-2 text-[13px]">
          <FileText size={15} className="shrink-0 text-zinc-600 dark:text-zinc-300" />
          <div className="w-44">{nameInput("File name", true)}</div>
        </div>
      ) : (
        <>
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
          {/* While the menu is open its rows carry their own pencils. */}
          {!open && (
            <button
              type="button"
              aria-label={`Rename ${state.activePath}`}
              title="Rename"
              onClick={() => setDraft({ mode: "rename", from: state.activePath, name: state.activePath })}
              className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
            >
              <Pencil size={13} />
            </button>
          )}
        </>
      )}

      {open && (
        <div
          // While a row is being renamed, a press elsewhere in the menu keeps the focus
          // in the field rather than blurring it, and the click then settles the rename
          // before its own handler runs. Committing on blur instead would re-sort the
          // rows between press and release, and the release would land on another row.
          onMouseDown={(e) => {
            if (draft?.mode === "rename" && !(e.target instanceof HTMLInputElement)) e.preventDefault();
          }}
          onClickCapture={(e) => {
            if (!(e.target instanceof HTMLInputElement)) settleRename.current();
          }}
          className="absolute left-0 top-full z-40 mt-2 w-80 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <ul className="max-h-72 overflow-y-auto py-1">
            {files.map(({ path, document }) => {
              const active = path === state.activePath;
              const counts = declared(document);
              if (draft?.mode === "rename" && draft.from === path) {
                return (
                  <li key={path} className="flex gap-2.5 px-3 py-1.5">
                    <FileText size={15} className="mt-1.5 shrink-0 text-zinc-500" />
                    <div className="min-w-0 flex-1">{nameInput("File name")}</div>
                  </li>
                );
              }
              return (
                <li key={path} className="relative">
                  <button
                    type="button"
                    // The counts spelled out, as the tooltip and the row's description.
                    title={counts.length > 0 ? counts.map((c) => c.label).join(" · ") : undefined}
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
                    {counts.length > 0 && (
                      // The row's name stays the path: the counts are a glance.
                      <span
                        aria-hidden
                        className="ml-auto flex shrink-0 items-center gap-2 text-[11px] tabular-nums text-zinc-400"
                      >
                        {counts.map(({ icon: Icon, count, label }) => (
                          <span key={label} className="flex items-center gap-0.5">
                            <Icon size={12} />
                            {count}
                          </span>
                        ))}
                      </span>
                    )}
                  </button>
                  <span className="absolute right-2 top-1/2 flex -translate-y-1/2 gap-0.5">
                    <button
                      type="button"
                      aria-label={`Rename ${path}`}
                      onClick={() => setDraft({ mode: "rename", from: path, name: path })}
                      className="rounded-full p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
                    >
                      <Pencil size={13} />
                    </button>
                    {/* The last file stays: a project of no files is not one. */}
                    {paths.length > 1 && (
                      <button
                        type="button"
                        aria-label={`Remove ${path}`}
                        // Removing another file leaves the open one open, and the menu
                        // with it, so several can go in one visit.
                        onClick={() => {
                          dispatch({ type: EditorActionType.DELETE_FILE, data: { path } });
                          if (active) close();
                        }}
                        className="rounded-full p-1 text-zinc-400 hover:text-red-500"
                      >
                        <Trash2 size={13} />
                      </button>
                    )}
                  </span>
                </li>
              );
            })}
          </ul>

          <div className="border-t border-black/10 dark:border-white/10">
            {draft?.mode === "create" ? (
              <div className="px-3 py-2">{nameInput("New file name")}</div>
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

type Draft = { mode: "create"; name: string } | { mode: "rename"; from: string; name: string };

/**
 * The field a name is typed into. Enter commits it and Escape abandons it — only it,
 * not the menu around it. A rename also commits when the field loses focus, as a
 * rename in place does anywhere else; a bad or empty name abandons instead of sticking.
 */
function NameInput({
  label,
  draft,
  problem,
  floatingProblem,
  onChange,
  onCommit,
  onCancel,
}: {
  label: string;
  draft: Draft;
  problem: string | null;
  /** Show the problem over what follows, where there is no room to push it down. */
  floatingProblem: boolean;
  onChange: (name: string) => void;
  onCommit: () => void;
  onCancel: () => void;
}) {
  return (
    <form
      className="relative"
      onSubmit={(e) => {
        e.preventDefault();
        onCommit();
      }}
    >
      <input
        autoFocus
        aria-label={label}
        value={draft.name}
        placeholder="orders.yaml"
        // The name without its extension is what a rename is usually about.
        onFocus={(e) => e.currentTarget.setSelectionRange(0, draft.name.replace(/\.ya?ml$/i, "").length)}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key !== "Escape") return;
          // A rename's Escape abandons the rename, not the menu around it, which closes
          // on an Escape that reaches the document. A new file's closes both, as it did.
          if (draft.mode === "rename") e.nativeEvent.stopPropagation();
          onCancel();
        }}
        onBlur={() => {
          if (draft.mode !== "rename") return;
          if (problem || draft.name.trim() === "") onCancel();
          else onCommit();
        }}
        className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-sky-500 dark:border-white/15"
      />
      {problem && (
        <p
          className={
            floatingProblem
              ? "absolute left-0 top-full z-40 mt-1 whitespace-nowrap rounded-md border border-black/10 bg-white px-2 py-1 text-xs text-rose-500 shadow-lg dark:border-white/10 dark:bg-zinc-900"
              : "mt-1 text-xs text-rose-500"
          }
        >
          {problem}
        </p>
      )}
    </form>
  );
}

interface Count {
  icon: LucideIcon;
  count: number;
  label: string;
}

/**
 * What a file declares, with the icons the document bar's launchers use for each.
 * Kinds the file declares none of are left out.
 */
function declared(doc: EditorDocument): Count[] {
  const resources = resourceCount(doc);
  const ret: Count[] = [
    { icon: Cable, count: doc.connectors.length, label: plural(doc.connectors.length, "connection") },
    { icon: Variable, count: doc.env.length, label: plural(doc.env.length, "env var") },
    { icon: FileText, count: resources, label: plural(resources, "resource") },
  ];
  return ret.filter((c) => c.count > 0);
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
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
