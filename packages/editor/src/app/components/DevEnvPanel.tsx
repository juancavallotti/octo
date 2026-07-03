"use client";

import { useEffect, useRef, useState } from "react";
import { Eye, EyeOff, Plus, X } from "lucide-react";
import type { EnvVar } from "../model/document";
import { useEditorState, EditorActionType } from "../state/editorState";
import {
  parseDotEnv,
  serializeDotEnv,
  useDevEnvStore,
  type DevEnvMap,
  type DevEnvStore,
} from "../state/devEnvStore";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** Debounce for persisting edits to the backend as the user types. */
const SAVE_DEBOUNCE_MS = 600;

/**
 * The "Dev .env" console tab. Its rows mirror the document's declared `env:`
 * variables — the same list the Environment launcher edits — so the two stay in
 * sync (both read and write `state.document.env`). Here you can add a variable and
 * supply each one's value; name/default/required are still edited in the launcher.
 *
 * Values are masked by default and persisted as the integration's `.env.dev`
 * resource in the host's backend (see state/devEnvStore.ts) — not in the browser,
 * so credentials never live client-side and an MCP run can read them from the
 * store. run-host stages `.env.dev` into the run and the runtime loads it; a
 * variable left blank falls back to its declared default at runtime.
 *
 * Keyed by integration id so switching files remounts with the right values.
 */
export default function DevEnvPanel() {
  const { state } = useEditorState();
  const id = state.integration.id;
  const store = useDevEnvStore();
  return <DevEnvEditor key={id ?? "__draft__"} id={id} store={store} />;
}

function DevEnvEditor({
  id,
  store,
}: {
  id: string | null;
  store: DevEnvStore | null;
}) {
  const { state, dispatch } = useEditorState();
  const vars = state.document.env;
  const editable = !!store && store.canEdit(id);
  // null while the .env.dev content is still loading from the backend.
  const [values, setValues] = useState<DevEnvMap | null>(editable ? null : {});
  const [reveal, setReveal] = useState(false);
  const [newName, setNewName] = useState("");
  const [newValue, setNewValue] = useState("");
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Load the stored values once per integration (the panel remounts on id change).
  useEffect(() => {
    if (!editable || !store) return;
    let cancelled = false;
    store
      .load(id)
      .then((content) => {
        if (!cancelled) setValues(parseDotEnv(content));
      })
      .catch(() => {
        if (!cancelled) setValues({});
      });
    return () => {
      cancelled = true;
    };
  }, [editable, store, id]);

  // Flush any pending debounced save when the panel unmounts.
  useEffect(
    () => () => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
    },
    [],
  );

  /** Persist the given map to the backend, debounced. */
  function persist(next: DevEnvMap) {
    if (!store) return;
    if (saveTimer.current) clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      void store.save(id, serializeDotEnv(next)).catch(() => {});
    }, SAVE_DEBOUNCE_MS);
  }

  const setEnv = (env: EnvVar[]) =>
    dispatch({ type: EditorActionType.SET_ENV, data: { env } });

  function setValue(name: string, value: string) {
    const next = { ...(values ?? {}), [name]: value };
    setValues(next);
    persist(next);
  }

  function removeVar(name: string) {
    setEnv(vars.filter((v) => v.name !== name));
    const next = { ...(values ?? {}) };
    delete next[name];
    setValues(next);
    persist(next);
  }

  function addVar() {
    const name = newName.trim();
    if (name === "" || vars.some((v) => v.name === name)) return;
    setEnv([...vars, { name }]);
    if (newValue !== "") setValue(name, newValue);
    setNewName("");
    setNewValue("");
  }

  // Draft (no id) on a backend that needs a saved integration: nothing to edit yet.
  if (!editable) {
    return (
      <div className="flex flex-1 flex-col overflow-auto px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
        Save the integration first to set its dev environment values.
      </div>
    );
  }

  const loaded = values ?? {};

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
        <span>
          Values for the declared environment variables, stored as this
          integration&apos;s <code>.env.dev</code> resource. Changes apply on the next Run.
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
              value={loaded[v.name] ?? ""}
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
