"use client";

import { useEffect, useRef, useState } from "react";
import { Plus, Variable, X } from "lucide-react";
import type { EnvVar } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Floating launcher pinned to the top-left of the canvas, beside the connections
 * launcher. The button opens a popover that edits the document's declared
 * environment variables (the runtime's top-level `env:`). Variables are referenced
 * from settings as `${NAME}`; the runtime resolves them at startup, falling back to
 * each variable's default.
 */
export default function EnvLauncher() {
  const { state } = useEditorState();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const count = state.document.env.length;

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

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label="Environment variables"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-full border border-black/10 bg-white/90 px-3 py-1.5 text-sm text-zinc-600 shadow-sm backdrop-blur transition-colors hover:bg-white hover:text-zinc-900 dark:border-white/15 dark:bg-zinc-900/90 dark:text-zinc-300 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <Variable size={16} />
        Environment
        {count > 0 && (
          <span className="rounded-full bg-black/[0.06] px-1.5 text-xs tabular-nums dark:bg-white/10">
            {count}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute left-0 top-full z-20 mt-2 w-80 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <EnvEditor />
        </div>
      )}
    </div>
  );
}

/**
 * Inline editor for the document's environment variables, modeled on
 * StringMapEditor: it holds ordered rows locally so names can be typed freely, and
 * dispatches SET_ENV (rows with a non-empty name) on every edit. Mounted fresh each
 * time the popover opens, so it always seeds from the latest document.
 */
function EnvEditor() {
  const { state, dispatch } = useEditorState();
  const [rows, setRows] = useState<EnvVar[]>(() =>
    state.document.env.map((v) => ({ ...v })),
  );

  function commit(next: EnvVar[]) {
    setRows(next);
    const env = next
      .filter((v) => v.name.trim() !== "")
      .map((v) => {
        const out: EnvVar = { name: v.name.trim() };
        if (v.default !== undefined && v.default !== "") out.default = v.default;
        if (v.required) out.required = true;
        return out;
      });
    dispatch({ type: EditorActionType.SET_ENV, data: { env } });
  }

  function update(i: number, patch: Partial<EnvVar>) {
    commit(rows.map((row, j) => (j === i ? { ...row, ...patch } : row)));
  }

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <Variable size={16} className="text-zinc-500" />
        <span className="text-sm font-medium">Environment variables</span>
      </div>

      <div className="flex max-h-[60vh] flex-col gap-1.5 overflow-y-auto p-3">
        {rows.length === 0 ? (
          <p className="text-xs text-zinc-400 dark:text-zinc-500">
            No variables yet. Reference them from settings as{" "}
            <code className="font-mono">${"{NAME}"}</code>.
          </p>
        ) : (
          rows.map((row, i) => (
            <div
              key={i}
              className="flex flex-col gap-1.5 rounded-lg border border-black/[0.06] p-2 dark:border-white/[0.06]"
            >
              <div className="flex items-center gap-1.5">
                <input
                  type="text"
                  value={row.name}
                  placeholder="NAME"
                  onChange={(e) => update(i, { name: e.target.value })}
                  className={`${INPUT} font-mono`}
                />
                <input
                  type="text"
                  value={row.default ?? ""}
                  placeholder="default"
                  onChange={(e) => update(i, { default: e.target.value })}
                  className={INPUT}
                />
                <button
                  type="button"
                  aria-label="Remove variable"
                  onClick={() => commit(rows.filter((_, j) => j !== i))}
                  className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
                >
                  <X size={14} />
                </button>
              </div>
              <label className="flex items-center gap-1.5 text-xs text-zinc-500">
                <input
                  type="checkbox"
                  checked={row.required ?? false}
                  onChange={(e) => update(i, { required: e.target.checked })}
                  className="accent-sky-500"
                />
                Required
              </label>
            </div>
          ))
        )}
      </div>

      <div className="border-t border-black/10 p-2 dark:border-white/10">
        <button
          type="button"
          onClick={() => commit([...rows, { name: "" }])}
          className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
        >
          <Plus size={14} />
          Add variable
        </button>
      </div>
    </div>
  );
}
