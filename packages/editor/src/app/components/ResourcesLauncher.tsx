"use client";

import { useEffect, useRef, useState } from "react";
import { FileText, Plus, X } from "lucide-react";
import type { Resources, TemplateResource } from "../model/document";
import { useEditorState, EditorActionType } from "../state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const EMPTY: Resources = { env: [], templates: [] };

/**
 * Floating launcher pinned to the top-left of the canvas, beside the environment
 * launcher. The button opens a popover that edits the document's declared
 * resources (the runtime's top-level `resources:`): the `.env`-convention files
 * combined into the environment, and the template files rendered by the
 * `template-resource` block / `templateResource()` CEL function. Loaded and parsed
 * at deployment, so a missing or malformed one fails then, not at message time.
 */
export default function ResourcesLauncher() {
  const { state } = useEditorState();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const resources = state.document.resources ?? EMPTY;
  const count = resources.env.length + resources.templates.length;

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
        aria-label="Resources"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 rounded-full border border-black/10 bg-white/90 px-3 py-1.5 text-sm text-zinc-600 shadow-sm backdrop-blur transition-colors hover:bg-white hover:text-zinc-900 dark:border-white/15 dark:bg-zinc-900/90 dark:text-zinc-300 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <FileText size={16} />
        Resources
        {count > 0 && (
          <span className="rounded-full bg-black/[0.06] px-1.5 text-xs tabular-nums dark:bg-white/10">
            {count}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute left-0 top-full z-20 mt-2 w-96 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <ResourcesEditor />
        </div>
      )}
    </div>
  );
}

/**
 * Inline editor for the document's resources. Holds ordered rows locally so paths
 * can be typed freely, dispatching SET_RESOURCES on every edit. Mounted fresh each
 * time the popover opens, so it always seeds from the latest document.
 */
function ResourcesEditor() {
  const { state, dispatch } = useEditorState();
  const seed = state.document.resources ?? EMPTY;
  const [env, setEnv] = useState<string[]>(() => [...seed.env]);
  const [templates, setTemplates] = useState<TemplateResource[]>(() =>
    seed.templates.map((t) => ({ ...t })),
  );

  function commit(nextEnv: string[], nextTemplates: TemplateResource[]) {
    setEnv(nextEnv);
    setTemplates(nextTemplates);
    const resources: Resources = {
      env: nextEnv.filter((e) => e.trim() !== "").map((e) => e.trim()),
      templates: nextTemplates
        .filter((t) => t.resource.trim() !== "")
        .map((t) => {
          const out: TemplateResource = { resource: t.resource.trim() };
          if (t.as && t.as.trim() !== "") out.as = t.as.trim();
          return out;
        }),
    };
    dispatch({ type: EditorActionType.SET_RESOURCES, data: { resources } });
  }

  const setEnvAt = (i: number, value: string) =>
    commit(
      env.map((e, j) => (j === i ? value : e)),
      templates,
    );
  const setTemplateAt = (i: number, patch: Partial<TemplateResource>) =>
    commit(
      env,
      templates.map((t, j) => (j === i ? { ...t, ...patch } : t)),
    );

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <FileText size={16} className="text-zinc-500" />
        <span className="text-sm font-medium">Resources</span>
      </div>

      <div className="flex max-h-[60vh] flex-col gap-4 overflow-y-auto p-3">
        {/* Env-file resources */}
        <section className="flex flex-col gap-1.5">
          <h4 className="text-xs font-semibold uppercase tracking-wide text-zinc-400 dark:text-zinc-500">
            Env files
          </h4>
          {env.length === 0 ? (
            <p className="text-xs text-zinc-400 dark:text-zinc-500">
              No env files. Add a path (relative to the config) whose values are
              combined into the environment, e.g.{" "}
              <code className="font-mono">.env.dev</code>.
            </p>
          ) : (
            env.map((row, i) => (
              <div key={i} className="flex items-center gap-1.5">
                <input
                  type="text"
                  value={row}
                  placeholder=".env.dev"
                  onChange={(e) => setEnvAt(i, e.target.value)}
                  className={`${INPUT} font-mono`}
                />
                <button
                  type="button"
                  aria-label="Remove env file"
                  onClick={() =>
                    commit(
                      env.filter((_, j) => j !== i),
                      templates,
                    )
                  }
                  className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
                >
                  <X size={14} />
                </button>
              </div>
            ))
          )}
          <button
            type="button"
            onClick={() => commit([...env, ""], templates)}
            className="flex items-center gap-1.5 self-start rounded-md px-1 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
          >
            <Plus size={14} />
            Add env file
          </button>
        </section>

        {/* Template resources */}
        <section className="flex flex-col gap-1.5">
          <h4 className="text-xs font-semibold uppercase tracking-wide text-zinc-400 dark:text-zinc-500">
            Templates
          </h4>
          {templates.length === 0 ? (
            <p className="text-xs text-zinc-400 dark:text-zinc-500">
              No templates. Declare a template file and an optional alias to
              reference it from a <code className="font-mono">template-resource</code>{" "}
              block or <code className="font-mono">templateResource()</code>.
            </p>
          ) : (
            templates.map((row, i) => (
              <div
                key={i}
                className="flex flex-col gap-1.5 rounded-lg border border-black/[0.06] p-2 dark:border-white/[0.06]"
              >
                <div className="flex items-center gap-1.5">
                  <input
                    type="text"
                    value={row.resource}
                    placeholder="templates/welcome.tmpl"
                    onChange={(e) => setTemplateAt(i, { resource: e.target.value })}
                    className={`${INPUT} font-mono`}
                  />
                  <button
                    type="button"
                    aria-label="Remove template"
                    onClick={() =>
                      commit(
                        env,
                        templates.filter((_, j) => j !== i),
                      )
                    }
                    className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
                  >
                    <X size={14} />
                  </button>
                </div>
                <input
                  type="text"
                  value={row.as ?? ""}
                  placeholder="alias (optional)"
                  onChange={(e) => setTemplateAt(i, { as: e.target.value })}
                  className={`${INPUT} font-mono`}
                />
              </div>
            ))
          )}
          <button
            type="button"
            onClick={() => commit(env, [...templates, { resource: "" }])}
            className="flex items-center gap-1.5 self-start rounded-md px-1 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
          >
            <Plus size={14} />
            Add template
          </button>
        </section>
      </div>
    </div>
  );
}
