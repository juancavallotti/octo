"use client";

import { useMemo, useState } from "react";
import { SortableContext } from "@dnd-kit/sortable";
import { FolderPlus, Search } from "lucide-react";
import Loaded from "@/app/components/ui/Loaded";
import type { Loadable } from "@/app/components/ui/loadable";
import { SkeletonRows } from "@/app/components/ui/Skeleton";
import { EMPTY, type Data } from "./managerData";
import type { FlatFolder } from "./model";
import { ancestorsOf, smartKey, treeRows } from "./treeModel";
import { useTreeExpansion } from "./useTreeExpansion";
import { FolderRow, SmartFolderRow, TopLevelDropZone, treeIndent } from "./FolderRow";
import TreeIntegrationRow from "./TreeIntegrationRow";

/**
 * The integrations sidebar: the smart folders — Running apps, All integrations —
 * above the real folders and the integrations filed in them, with the unfiled
 * ones at the top level, and a search over names. It owns only transient UI
 * state — which folders are open, the search, an inline create or rename — and
 * hands every change to the manager.
 *
 * Rows do not shift out of the way while something is dragged over them: a drop
 * means "here, where this row is", shown by the row lighting up, and rows sliding
 * about would move the very target being aimed at.
 */
export default function IntegrationTree({
  state,
  flat,
  selectedId,
  onSelect,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
}: {
  /** The tree as it loads; the counts and rows wait until it is ready. */
  state: Loadable<Data>;
  flat: FlatFolder[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onCreateFolder: (name: string, parentId: string | null) => void;
  onRenameFolder: (folder: FlatFolder, name: string) => void;
  onDeleteFolder: (folder: FlatFolder) => void;
}) {
  // undefined: not creating; null: at the top level; else under that folder.
  const [creatingIn, setCreatingIn] = useState<string | null | undefined>(undefined);
  const [draftName, setDraftName] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [query, setQuery] = useState("");

  const data = state.status === "ready" ? state.data : EMPTY;
  const ready = state.status === "ready";
  const refreshing = ready && state.refreshing;
  const known = selectedId !== null && data.integrations.some((i) => i.id === selectedId);
  const { toggled, toggle } = useTreeExpansion(
    known ? { id: selectedId, ancestors: ancestorsOf(data, flat, selectedId) } : null,
  );
  const rows = useMemo(() => treeRows(data, toggled, query), [data, toggled, query]);

  const startCreate = (parentId: string | null) => {
    setCreatingIn(parentId);
    setDraftName("");
  };
  const submitCreate = () => {
    const name = draftName.trim();
    const parentId = creatingIn ?? null;
    setCreatingIn(undefined);
    setDraftName("");
    if (name) onCreateFolder(name, parentId);
  };
  const submitRename = (f: FlatFolder) => {
    const name = editName.trim();
    setEditingId(null);
    if (name && name !== f.name) onRenameFolder(f, name);
  };

  const parentName =
    creatingIn != null ? flat.find((f) => f.id === creatingIn)?.name : undefined;

  return (
    <aside className="relative flex w-72 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
      {/* A refresh after a change keeps the tree; this bar is all it shows. */}
      {refreshing && (
        <span
          aria-hidden
          className="absolute inset-x-0 top-0 h-0.5 animate-pulse bg-sky-500/60"
        />
      )}
      <div className="flex items-center gap-2 px-3 py-2">
        <label className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md bg-black/[0.04] px-2 py-1 text-sm focus-within:ring-1 focus-within:ring-sky-500/40 dark:bg-white/[0.06]">
          <Search size={13} aria-hidden className="shrink-0 text-zinc-400" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setQuery("");
            }}
            placeholder="Search integrations"
            aria-label="Search integrations"
            className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-zinc-400"
          />
        </label>
        <button
          type="button"
          onClick={() => startCreate(null)}
          title="New folder"
          aria-label="New folder"
          className="rounded p-1 text-zinc-400 hover:bg-black/[0.04] hover:text-zinc-700 dark:hover:bg-white/[0.06]"
        >
          <FolderPlus size={16} />
        </button>
      </div>

      <Loaded state={state} skeleton={<SkeletonRows count={8} />}>
        {() => (
          <ul className="flex min-h-0 flex-1 flex-col overflow-y-auto">
            {creatingIn !== undefined && (
              <li>
                <input
                  autoFocus
                  value={draftName}
                  placeholder={parentName ? `New folder in ${parentName}…` : "New folder…"}
                  onChange={(e) => setDraftName(e.target.value)}
                  onBlur={submitCreate}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitCreate();
                    if (e.key === "Escape") setCreatingIn(undefined);
                  }}
                  style={{ paddingLeft: treeIndent(0) }}
                  className="w-full bg-transparent py-1.5 pr-3 text-sm outline-none ring-1 ring-sky-500/40"
                />
              </li>
            )}

            <SortableContext
              items={rows.flatMap((r) =>
                r.kind === "folder"
                  ? [`folder:${r.folder.id}`]
                  : r.kind === "integration"
                    ? [r.rowId]
                    : [],
              )}
              strategy={() => null}
            >
              {rows.map((r) =>
                r.kind === "smart" ? (
                  <SmartFolderRow
                    key={smartKey(r.id)}
                    id={r.id}
                    label={r.label}
                    title={r.title}
                    expanded={r.expanded}
                    count={r.count}
                    onToggle={() => toggle(smartKey(r.id))}
                  />
                ) : r.kind === "folder" ? (
                  <FolderRow
                    key={r.folder.id}
                    f={r.folder}
                    expandable={r.expandable}
                    expanded={r.expanded}
                    count={r.count}
                    editing={editingId === r.folder.id}
                    editName={editName}
                    onEditNameChange={setEditName}
                    onSubmitRename={() => submitRename(r.folder)}
                    onCancelRename={() => setEditingId(null)}
                    onToggle={() => toggle(r.folder.id)}
                    onStartRename={() => {
                      setEditingId(r.folder.id);
                      setEditName(r.folder.name);
                    }}
                    onNewSubfolder={() => startCreate(r.folder.id)}
                    onDelete={() => onDeleteFolder(r.folder)}
                  />
                ) : (
                  <TreeIntegrationRow
                    key={r.rowId}
                    rowId={r.rowId}
                    integration={r.integration}
                    depth={r.depth}
                    running={r.running}
                    computed={r.computed}
                    selected={selectedId === r.integration.id}
                    onSelect={onSelect}
                  />
                ),
              )}
            </SortableContext>

            {data.integrations.length === 0 ? (
              <li className="px-3 py-2 text-sm text-zinc-400">No integrations yet.</li>
            ) : query.trim() !== "" &&
              !rows.some((r) => r.kind === "integration") ? (
              <li className="px-3 py-2 text-sm text-zinc-400">No integration matches.</li>
            ) : null}
            <TopLevelDropZone />
          </ul>
        )}
      </Loaded>
    </aside>
  );
}
