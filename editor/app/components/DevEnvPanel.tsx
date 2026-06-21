"use client";

import { useState } from "react";
import { Eye, EyeOff, Plus, X } from "lucide-react";
import type { EnvVar } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import { loadDevEnv, saveDevEnv, type DevEnvMap } from "@/app/state/devEnv";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * The "Dev .env" console tab. Its rows mirror the document's declared `env:`
 * variables — the same list the Environment launcher edits — so the two stay in
 * sync (both read and write `state.document.env`). Here you can add a variable and
 * supply each one's value; name/default/required are still edited in the launcher.
 *
 * Values are masked by default and persisted in localStorage scoped by the open
 * integration's id (see state/devEnv.ts) — they never touch the document, the
 * rendered YAML, or any server file. They are injected into the runner's
 * environment at run time and discarded; a variable left blank falls back to its
 * declared default at runtime.
 *
 * Keyed by integration id so switching files remounts with the right value bucket
 * (initialized lazily, no setState-in-effect).
 */
export default function DevEnvPanel() {
  const { state } = useEditorState();
  const id = state.integration.id;
  return <DevEnvEditor key={id ?? "__draft__"} id={id} />;
}

function DevEnvEditor({ id }: { id: string | null }) {
  const { state, dispatch } = useEditorState();
  const vars = state.document.env;
  const [values, setValues] = useState<DevEnvMap>(() => loadDevEnv(id));
  const [reveal, setReveal] = useState(false);
  const [newName, setNewName] = useState("");
  const [newValue, setNewValue] = useState("");

  const setEnv = (env: EnvVar[]) =>
    dispatch({ type: EditorActionType.SET_ENV, data: { env } });

  function setValue(name: string, value: string) {
    const next = { ...values, [name]: value };
    setValues(next);
    saveDevEnv(id, next);
  }

  function removeVar(name: string) {
    setEnv(vars.filter((v) => v.name !== name));
    const next = { ...values };
    delete next[name];
    setValues(next);
    saveDevEnv(id, next);
  }

  function addVar() {
    const name = newName.trim();
    if (name === "" || vars.some((v) => v.name === name)) return;
    setEnv([...vars, { name }]);
    if (newValue !== "") setValue(name, newValue);
    setNewName("");
    setNewValue("");
  }

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
        <span>
          Values for the declared environment variables, injected into the runner at
          start — never written to the config. Changes apply on the next Run.
        </span>
        {vars.length > 0 && (
          <button
            type="button"
            onClick={() => setReveal((r) => !r)}
            aria-label={reveal ? "Hide values" : "Show values"}
            title={reveal ? "Hide values" : "Show values"}
            className="ml-auto flex shrink-0 items-center gap-1 rounded p-1 hover:bg-black/5 dark:hover:bg-white/10"
          >
            {reveal ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
          </button>
        )}
      </div>

      <div className="flex flex-col gap-1.5 px-3 pb-2">
        {vars.map((v) => (
          <div key={v.name} className="flex items-center gap-1.5">
            <span className="flex w-40 shrink-0 items-center gap-1 truncate font-mono text-xs text-zinc-600 dark:text-zinc-300">
              <span className="truncate" title={v.name}>
                {v.name}
              </span>
              {v.required && (
                <span className="text-red-500" title="Required">
                  *
                </span>
              )}
            </span>
            <input
              type={reveal ? "text" : "password"}
              value={values[v.name] ?? ""}
              placeholder={v.default ? `default: ${v.default}` : "value"}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setValue(v.name, e.target.value)}
              className={INPUT}
            />
            <button
              type="button"
              aria-label={`Remove ${v.name}`}
              onClick={() => removeVar(v.name)}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
        ))}

        {/* Add a new variable (kept in sync with the document's declared env:). */}
        <div className="flex items-center gap-1.5">
          <input
            type="text"
            value={newName}
            placeholder="NAME"
            aria-label="New variable name"
            spellCheck={false}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") addVar();
            }}
            className={`${INPUT} w-40 shrink-0 font-mono`}
          />
          <input
            type={reveal ? "text" : "password"}
            value={newValue}
            placeholder="value"
            aria-label="New variable value"
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => setNewValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") addVar();
            }}
            className={INPUT}
          />
          <button
            type="button"
            aria-label="Add variable"
            onClick={addVar}
            disabled={newName.trim() === ""}
            className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-zinc-700 disabled:opacity-40 dark:hover:text-zinc-300"
          >
            <Plus size={14} />
          </button>
        </div>
      </div>
    </div>
  );
}
