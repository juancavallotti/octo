"use client";

import { useEffect, useRef, useState } from "react";
import { Eye, EyeOff, PackageMinus, PackagePlus, Plus, Trash2 } from "lucide-react";
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
 * The "Dev .env" console tab. It reconciles the document's declared `env:`
 * variables (the same list the Environment launcher edits, kept in sync via
 * `state.document.env`) with the keys actually present in the loaded `.env.dev`,
 * and lists every one — so it reflects the file at full fidelity like a real
 * dotenv, never hiding a key the file carries. Declared vars read bold; keys found
 * only in the file read dimmed and offer a one-click "declare" (mirroring the
 * Resources tab's scope model). name/default/required are still edited in the
 * launcher.
 *
 * Values are shown in the clear by default (this is a dev tool; the eye toggle can
 * mask them) and persisted as the integration's `.env.dev` resource in the host's
 * backend (see state/devEnvStore.ts) — not in the browser, so credentials never
 * live client-side and an MCP run can read them from the store. run-host stages
 * `.env.dev` into the run and the runtime loads it; a variable left blank falls
 * back to its declared default at runtime.
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
  // Values are visible by default — this is a development tool and the panel
  // mirrors a plain .env file, where you expect to read every value at a glance.
  // The toggle stays available to mask them (e.g. when screen-sharing).
  const [reveal, setReveal] = useState(true);
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

  /** Delete a key outright: drop its declaration (if any) and its stored value. */
  function deleteVar(name: string) {
    setEnv(vars.filter((v) => v.name !== name));
    const next = { ...(values ?? {}) };
    delete next[name];
    setValues(next);
    persist(next);
  }

  /** Declare a file-only key by adding it to the document's `env:` — value kept. */
  function declareVar(name: string) {
    if (vars.some((v) => v.name === name)) return;
    setEnv([...vars, { name }]);
  }

  /** Undeclare a tracked var: drop it from `env:` but leave the value in the file
   *  so it stays visible (as an undeclared key) rather than vanishing. */
  function undeclareVar(name: string) {
    setEnv(vars.filter((v) => v.name !== name));
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

  // Reconcile the document's declared `env:` with the keys actually present in the
  // loaded `.env.dev`, so the panel mirrors the file at full fidelity: declared
  // vars first (in document order), then any extra keys found only in the file.
  // Declared rows read bold; file-only rows read italic/gray with a "declare"
  // shortcut — same scope model as the Resources tab, so we never silently drop a
  // key the file already carries.
  const declaredByName = new Map(vars.map((v) => [v.name, v]));
  const rows: Array<{ name: string; envVar?: EnvVar }> = [
    ...vars.map((v) => ({ name: v.name, envVar: v })),
    ...Object.keys(loaded)
      .filter((k) => !declaredByName.has(k))
      .sort()
      .map((name) => ({ name })),
  ];

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
        <span>
          Every value in this integration&apos;s <code>.env.dev</code> resource.
          Declared variables are bold; keys found only in the file are dimmed —
          declare one to track it in the config. Changes apply on the next Run.
        </span>
        {rows.length > 0 && (
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
        {rows.map(({ name, envVar }) => {
          const declared = !!envVar;
          return (
            <div key={name} className="group/row flex items-center gap-1.5">
              <span
                className={`flex w-40 shrink-0 items-center gap-1 truncate font-mono text-xs ${
                  declared
                    ? "font-semibold text-zinc-700 dark:text-zinc-200"
                    : "italic text-zinc-400 dark:text-zinc-500"
                }`}
              >
                <span
                  className="truncate"
                  title={declared ? name : `${name} — not declared in the config`}
                >
                  {name}
                </span>
                {envVar?.required && (
                  <span className="text-red-500" title="Required">
                    *
                  </span>
                )}
              </span>
              <input
                type={reveal ? "text" : "password"}
                value={loaded[name] ?? ""}
                placeholder={envVar?.default ? `default: ${envVar.default}` : "value"}
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => setValue(name, e.target.value)}
                className={INPUT}
              />
              <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover/row:opacity-100">
                {declared ? (
                  <RowAction
                    label={`Remove ${name} from the config (keep its value)`}
                    onClick={() => undeclareVar(name)}
                    icon={<PackageMinus size={14} />}
                  />
                ) : (
                  <RowAction
                    label={`Declare ${name} in the config`}
                    onClick={() => declareVar(name)}
                    icon={<PackagePlus size={14} />}
                  />
                )}
                <RowAction
                  label={`Delete ${name}`}
                  onClick={() => deleteVar(name)}
                  icon={<Trash2 size={14} />}
                  danger
                />
              </div>
            </div>
          );
        })}

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

/** A hover-revealed per-row icon button (declare / undeclare / delete). */
function RowAction({
  label,
  onClick,
  icon,
  danger,
}: {
  label: string;
  onClick: () => void;
  icon: React.ReactNode;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={`rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] dark:hover:bg-white/[0.08] ${
        danger
          ? "hover:text-red-500"
          : "hover:text-zinc-700 dark:hover:text-zinc-200"
      }`}
    >
      {icon}
    </button>
  );
}
