"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Editor from "react-simple-code-editor";
import {
  ChevronDown,
  ChevronRight,
  FilePlus,
  FileText,
  Folder,
  FolderClosed,
  PackageMinus,
  PackagePlus,
  Plus,
  Trash2,
  X,
} from "lucide-react";
import { useEditorState, EditorActionType } from "../../state/editorState";
import type { Resources } from "../../model/document";
import {
  useResourceStore,
  type ResourceStore,
} from "../../providers/ResourceStoreProvider";
import type { StoredResource } from "../../providers/ResourceStoreProvider";
import { reconcileResources, type ReconciledResource } from "./reconcile";
import { highlight, languageLabel } from "./highlight";
import { buildTree, flattenTree, leafName, type TreeRow } from "./tree";
import { declareResource, undeclareResource } from "./declarations";
import { guessKind, scaffoldFor } from "./scaffold";
import { useConfirmDialog } from "./ConfirmDialog";

/**
 * The Resources tab: a file browser (left) and content editor (right) over the
 * integration's resources. It reconciles what the store holds with the document's
 * declared `resources:` — tracked files are editable and manageable, files that
 * exist but aren't declared are shown gray + italic and never mutated except by an
 * explicit "Include in project" (see reconcile.ts). Creating/deleting/including/
 * removing a file also updates the declared set so the YAML tab stays in sync.
 */
export default function ResourcesView() {
  const { state, dispatch } = useEditorState();
  const store = useResourceStore();
  const integrationId = state.integration.id;
  const declared = state.document.resources;

  const [stored, setStored] = useState<StoredResource[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [opError, setOpError] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useCollapsedFolders(integrationId);
  const { confirm, dialog } = useConfirmDialog();

  // (Re)load whenever the store or the saved integration id changes — the id
  // flips from null to a value on the first save, which should populate the tab.
  useEffect(() => {
    if (!store) return;
    let cancelled = false;
    setStatus("loading");
    store
      .list()
      .then((rows) => {
        if (cancelled) return;
        setStored(rows);
        setStatus("ready");
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        setStatus("error");
      });
    return () => {
      cancelled = true;
    };
  }, [store, integrationId]);

  const entries = useMemo(
    () => reconcileResources(stored, declared),
    [stored, declared],
  );
  const rows = useMemo(
    () => flattenTree(buildTree(entries), collapsed),
    [entries, collapsed],
  );

  const current = entries.find((e) => e.name === selected) ?? null;

  const setResources = useCallback(
    (resources: Resources) =>
      dispatch({ type: EditorActionType.SET_RESOURCES, data: { resources } }),
    [dispatch],
  );

  const patchStored = (updated: StoredResource) =>
    setStored((rows) => rows.map((r) => (r.id === updated.id ? updated : r)));

  const toggleFolder = (path: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  // --- Mutations. Each keeps the store (content) and the declared set (YAML) in
  // step; out-of-scope files are only ever mutated via Include (declare-only). ---

  const runOp = async (op: () => Promise<void>) => {
    setOpError(null);
    try {
      await op();
    } catch (e) {
      setOpError(e instanceof Error ? e.message : String(e));
    }
  };

  const createFile = (name: string) =>
    runOp(async () => {
      if (!store) return;
      const kind = guessKind(name);
      const created = await store.create({
        name,
        kind,
        content: scaffoldFor(name),
      });
      setStored((rows) => [...rows, created]);
      setResources(declareResource(declared, name, kind));
      setSelected(name);
    });

  const createMissing = (entry: ReconciledResource) =>
    runOp(async () => {
      if (!store) return;
      const created = await store.create({
        name: entry.name,
        kind: entry.kind,
        content: scaffoldFor(entry.name),
      });
      setStored((rows) => [...rows, created]); // already declared
      setSelected(entry.name);
    });

  const deleteFile = (entry: ReconciledResource) =>
    runOp(async () => {
      if (!store || !entry.stored) return;
      const ok = await confirm({
        title: `Delete ${leafName(entry.name)}?`,
        body: "This permanently deletes the resource and removes its declaration from the config.",
        confirmLabel: "Delete",
        danger: true,
      });
      if (!ok) return;
      const id = entry.stored.id;
      await store.remove(id);
      setStored((rows) => rows.filter((r) => r.id !== id));
      setResources(undeclareResource(declared, entry.name));
      if (selected === entry.name) setSelected(null);
    });

  // Include: declare an existing-but-undeclared file (non-destructive).
  const includeFile = (entry: ReconciledResource) =>
    runOp(async () => {
      setResources(declareResource(declared, entry.name, entry.kind));
    });

  // Remove from project: undeclare a tracked file, leaving the file untouched.
  const removeFromProject = (entry: ReconciledResource) =>
    runOp(async () => {
      setResources(undeclareResource(declared, entry.name));
    });

  // The tab is only reachable when a store is provided (ViewModeToggle gates it),
  // but the hook type is nullable — guard so downstream props stay non-null.
  // We don't gate on a saved integration: standalone resources are folder-global
  // and work without one; a platform draft simply lists empty and surfaces a
  // "save first" error from the store if the user tries to create.
  if (!store) return null;
  if (status === "loading") {
    return <Centered>Loading resources…</Centered>;
  }
  if (status === "error") {
    return <Centered>Couldn’t load resources: {error}</Centered>;
  }

  return (
    <div className="flex flex-1 min-h-0">
      <aside className="flex w-64 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
        <Toolbar
          existingNames={entries.map((e) => e.name)}
          onCreate={createFile}
        />
        {opError && (
          <p className="px-3 py-1.5 text-xs text-red-500">{opError}</p>
        )}
        <div className="flex-1 overflow-y-auto">
          {rows.length === 0 ? (
            <p className="px-3 py-4 text-xs text-zinc-400 dark:text-zinc-500">
              No resources yet. Use “New file” to add one.
            </p>
          ) : (
            <ul className="py-1">
              {rows.map((row) =>
                row.kind === "folder" ? (
                  <FolderRow
                    key={`d:${row.path}`}
                    row={row}
                    collapsed={collapsed.has(row.path)}
                    onToggle={() => toggleFolder(row.path)}
                  />
                ) : (
                  <FileRow
                    key={`f:${row.entry.name}`}
                    row={row}
                    active={row.entry.name === selected}
                    onSelect={() => setSelected(row.entry.name)}
                    onDelete={() => deleteFile(row.entry)}
                    onInclude={() => includeFile(row.entry)}
                    onRemove={() => removeFromProject(row.entry)}
                    onCreateMissing={() => createMissing(row.entry)}
                  />
                ),
              )}
            </ul>
          )}
        </div>
      </aside>
      <section className="flex flex-1 min-h-0 flex-col overflow-hidden">
        <ContentPane
          entry={current}
          store={store}
          onSaved={patchStored}
          onCreateMissing={() => current && createMissing(current)}
        />
      </section>
      {dialog}
    </div>
  );
}

/** Collapsed-folder set persisted per integration in localStorage. */
function useCollapsedFolders(
  integrationId: string | null,
): [Set<string>, (fn: (prev: Set<string>) => Set<string>) => void] {
  const key = integrationId
    ? `octo.resources.collapsed.${integrationId}`
    : null;
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    if (!key || typeof window === "undefined") return new Set();
    try {
      const raw = window.localStorage.getItem(key);
      return new Set(raw ? (JSON.parse(raw) as string[]) : []);
    } catch {
      return new Set();
    }
  });
  const update = useCallback(
    (fn: (prev: Set<string>) => Set<string>) =>
      setCollapsed((prev) => {
        const next = fn(prev);
        if (key && typeof window !== "undefined") {
          window.localStorage.setItem(key, JSON.stringify([...next]));
        }
        return next;
      }),
    [key],
  );
  return [collapsed, update];
}

/** Toolbar with an inline "New file" input. */
function Toolbar({
  existingNames,
  onCreate,
}: {
  existingNames: string[];
  onCreate: (name: string) => void;
}) {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (creating) inputRef.current?.focus();
  }, [creating]);

  const trimmed = name.trim();
  const duplicate = trimmed !== "" && existingNames.includes(trimmed);
  const valid = trimmed !== "" && !duplicate && !trimmed.endsWith("/");

  const submit = () => {
    if (!valid) return;
    onCreate(trimmed);
    setName("");
    setCreating(false);
  };

  return (
    <div className="border-b border-black/10 dark:border-white/10">
      <div className="flex items-center justify-between px-3 py-2">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-400 dark:text-zinc-500">
          Resources
        </span>
        <button
          type="button"
          aria-label="New file"
          title="New file"
          onClick={() => setCreating((c) => !c)}
          className="rounded p-1 text-zinc-500 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
        >
          <Plus size={15} />
        </button>
      </div>
      {creating && (
        <div className="px-3 pb-2">
          <div className="flex items-center gap-1.5">
            <input
              ref={inputRef}
              type="text"
              value={name}
              placeholder="templates/welcome.tmpl"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") submit();
                if (e.key === "Escape") {
                  setName("");
                  setCreating(false);
                }
              }}
              className="w-full rounded-md border border-black/10 bg-transparent px-2 py-1 font-mono text-xs outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
            />
            <button
              type="button"
              aria-label="Cancel"
              onClick={() => {
                setName("");
                setCreating(false);
              }}
              className="shrink-0 rounded p-1 text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300"
            >
              <X size={13} />
            </button>
          </div>
          {duplicate && (
            <p className="mt-1 text-[11px] text-red-500">
              A resource with that name already exists.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function FolderRow({
  row,
  collapsed,
  onToggle,
}: {
  row: Extract<TreeRow, { kind: "folder" }>;
  collapsed: boolean;
  onToggle: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        style={{ paddingLeft: `${0.5 + row.depth * 0.85}rem` }}
        className="flex w-full items-center gap-1.5 py-1 pr-2 text-left text-sm text-zinc-600 transition-colors hover:bg-black/[0.04] dark:text-zinc-300 dark:hover:bg-white/[0.06]"
      >
        {collapsed ? (
          <ChevronRight size={13} className="shrink-0 opacity-70" />
        ) : (
          <ChevronDown size={13} className="shrink-0 opacity-70" />
        )}
        <Folder size={14} className="shrink-0 opacity-70" />
        <span className="truncate">{row.name}</span>
      </button>
    </li>
  );
}

function FileRow({
  row,
  active,
  onSelect,
  onDelete,
  onInclude,
  onRemove,
  onCreateMissing,
}: {
  row: Extract<TreeRow, { kind: "file" }>;
  active: boolean;
  onSelect: () => void;
  onDelete: () => void;
  onInclude: () => void;
  onRemove: () => void;
  onCreateMissing: () => void;
}) {
  const { entry, label, depth } = row;
  const outOfScope = entry.scope === "out-of-scope";
  const missing = entry.scope === "declared-missing";
  return (
    <li className="group/row relative">
      <button
        type="button"
        onClick={onSelect}
        title={outOfScope ? `${entry.name} — not in project` : entry.name}
        style={{ paddingLeft: `${0.5 + depth * 0.85 + 0.9}rem` }}
        className={`flex w-full items-center gap-2 py-1.5 pr-14 text-left text-sm transition-colors ${
          active
            ? "bg-black/[0.06] dark:bg-white/10"
            : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
        } ${
          outOfScope
            ? "italic text-zinc-400 dark:text-zinc-500"
            : missing
              ? "text-amber-600 dark:text-amber-400"
              : "text-zinc-700 dark:text-zinc-200"
        }`}
      >
        <FileText size={14} className="shrink-0 opacity-70" />
        <span className="truncate">{label}</span>
      </button>
      <div className="absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center gap-0.5 opacity-0 transition-opacity group-hover/row:opacity-100">
        {entry.scope === "tracked" && (
          <>
            <RowAction
              label="Remove from project (keep file)"
              onClick={onRemove}
              icon={<PackageMinus size={13} />}
            />
            <RowAction
              label="Delete"
              onClick={onDelete}
              icon={<Trash2 size={13} />}
              danger
            />
          </>
        )}
        {outOfScope && (
          <RowAction
            label="Include in project"
            onClick={onInclude}
            icon={<PackagePlus size={13} />}
          />
        )}
        {missing && (
          <RowAction
            label="Create file"
            onClick={onCreateMissing}
            icon={<FilePlus size={13} />}
          />
        )}
      </div>
    </li>
  );
}

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

function ContentPane({
  entry,
  store,
  onSaved,
  onCreateMissing,
}: {
  entry: ReconciledResource | null;
  store: ResourceStore;
  onSaved: (updated: StoredResource) => void;
  onCreateMissing: () => void;
}) {
  if (!entry) {
    return (
      <Centered>
        <FolderClosed size={20} className="mb-2 opacity-50" />
        Select a resource to view its contents.
      </Centered>
    );
  }

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <span className="font-mono text-sm">{entry.name}</span>
        <ScopeBadge entry={entry} />
        <span className="ml-auto text-[11px] text-zinc-400 dark:text-zinc-500">
          {languageLabel(entry.name)}
        </span>
      </header>
      {entry.scope === "declared-missing" ? (
        <Centered>
          Declared in the config but no content stored yet.
          <button
            type="button"
            onClick={onCreateMissing}
            className="mt-3 flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-sky-500"
          >
            <FilePlus size={14} />
            Create file
          </button>
        </Centered>
      ) : entry.scope === "tracked" && entry.stored ? (
        // Fresh buffer per file: key on id so switching files resets state.
        <ResourceEditor
          key={entry.stored.id}
          resource={entry.stored}
          store={store}
          onSaved={onSaved}
        />
      ) : (
        // Out-of-scope: shown read-only, never edited (see reconcile.ts).
        <div className="flex-1 overflow-auto">
          <pre
            className="octo-code m-0 p-3 font-mono text-xs leading-relaxed text-zinc-800 dark:text-zinc-200"
            dangerouslySetInnerHTML={{
              __html: highlight(entry.stored?.content ?? "", entry.name),
            }}
          />
        </div>
      )}
    </>
  );
}

type SaveState = "idle" | "dirty" | "saving" | "saved" | "error";

/**
 * Editable, syntax-highlighted content editor for a tracked resource. Holds a
 * local buffer and debounce-autosaves through the store, mirroring the Dev .env
 * panel's persist-as-you-type behavior. Only ever mounted for tracked files.
 */
function ResourceEditor({
  resource,
  store,
  onSaved,
}: {
  resource: StoredResource;
  store: ResourceStore;
  onSaved: (updated: StoredResource) => void;
}) {
  const [value, setValue] = useState(resource.content);
  const [saveState, setSaveState] = useState<SaveState>("idle");
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Debounced autosave. The initial render matches the stored content, so it
  // stays idle until the user actually types.
  useEffect(() => {
    if (value === resource.content) return;
    setSaveState("dirty");
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      setSaveState("saving");
      store
        .update(resource.id, { content: value })
        .then((updated) => {
          onSaved(updated);
          setSaveState("saved");
        })
        .catch((e) => {
          setError(e instanceof Error ? e.message : String(e));
          setSaveState("error");
        });
    }, 600);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
    // onSaved/store are stable for the editor's lifetime; resource.content is the
    // save baseline and never changes while this instance is mounted (keyed by id).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex-1 overflow-auto">
        <Editor
          value={value}
          onValueChange={setValue}
          highlight={(code) => highlight(code, resource.name)}
          padding={12}
          textareaClassName="octo-code-textarea"
          preClassName="octo-code"
          className="octo-code min-h-full"
          // Set the mono font via style, not a class: react-simple-code-editor
          // copies these onto both the textarea and the highlight <pre> so their
          // glyph metrics line up exactly. An "inherit" here would fall back to the
          // app's sans font — it must read as code.
          style={{
            fontFamily:
              "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
            fontSize: 12,
            lineHeight: 1.6,
            minHeight: "100%",
          }}
        />
      </div>
      <SaveStatus state={saveState} error={error} />
    </div>
  );
}

function SaveStatus({ state, error }: { state: SaveState; error: string | null }) {
  const label =
    state === "saving"
      ? "Saving…"
      : state === "saved"
        ? "Saved"
        : state === "dirty"
          ? "Unsaved changes"
          : state === "error"
            ? `Save failed: ${error}`
            : "";
  return (
    <div
      className={`flex h-6 items-center border-t border-black/10 px-3 text-[11px] dark:border-white/10 ${
        state === "error"
          ? "text-red-500"
          : "text-zinc-400 dark:text-zinc-500"
      }`}
    >
      {label}
    </div>
  );
}

function ScopeBadge({ entry }: { entry: ReconciledResource }) {
  if (entry.scope === "out-of-scope") {
    return (
      <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] italic text-zinc-500 dark:bg-white/10">
        not in project
      </span>
    );
  }
  if (entry.scope === "declared-missing") {
    return (
      <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-600 dark:text-amber-400">
        missing
      </span>
    );
  }
  return (
    <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-white/10">
      {entry.kind}
    </span>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center p-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
      {children}
    </div>
  );
}
