"use client";

import { useMemo, useState } from "react";
import { SortableContext } from "@dnd-kit/sortable";
import { FolderPlus } from "lucide-react";
import type { Data } from "./managerData";
import type { FlatFolder, Filter } from "./model";
import { ancestorsOf, treeRows } from "./treeModel";
import { useTreeExpansion } from "./useTreeExpansion";
import { FolderRow, TopLevelDropZone, treeIndent } from "./FolderRow";
import TreeIntegrationRow from "./TreeIntegrationRow";

/**
 * The integrations sidebar: folders and the integrations filed in them as one
 * tree, with the unfiled ones at the top level, narrowed to what is deployed by
 * the Running filter. It owns only transient UI state — which folders are open,
 * an inline create or rename — and hands every change to the manager.
 *
 * Rows do not shift out of the way while something is dragged over them: a drop
 * means "here, where this row is", shown by the row lighting up, and rows sliding
 * about would move the very target being aimed at.
 */
export default function IntegrationTree({
  data,
  flat,
  filter,
  runningCount,
  selectedId,
  onFilter,
  onSelect,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
}: {
  data: Data;
  flat: FlatFolder[];
  filter: Filter;
  runningCount: number;
  selectedId: string | null;
  onFilter: (filter: Filter) => void;
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

  const known = selectedId !== null && data.integrations.some((i) => i.id === selectedId);
  const { collapsed, toggle } = useTreeExpansion(
    known ? { id: selectedId, ancestors: ancestorsOf(data, flat, selectedId) } : null,
  );
  const rows = useMemo(() => treeRows(data, filter, collapsed), [data, filter, collapsed]);

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
    <aside className="flex w-72 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
      <div className="flex items-center gap-2 px-3 py-2">
        <FilterToggle
          filter={filter}
          total={data.integrations.length}
          running={runningCount}
          onFilter={onFilter}
        />
        <button
          type="button"
          onClick={() => startCreate(null)}
          title="New folder"
          aria-label="New folder"
          className="ml-auto rounded p-1 text-zinc-400 hover:bg-black/[0.04] hover:text-zinc-700 dark:hover:bg-white/[0.06]"
        >
          <FolderPlus size={16} />
        </button>
      </div>

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
          items={rows.map((r) =>
            r.kind === "folder" ? `folder:${r.folder.id}` : `integration:${r.integration.id}`,
          )}
          strategy={() => null}
        >
          {rows.map((r) =>
            r.kind === "folder" ? (
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
                key={r.integration.id}
                integration={r.integration}
                depth={r.depth}
                running={r.running}
                selected={selectedId === r.integration.id}
                onSelect={onSelect}
              />
            ),
          )}
        </SortableContext>

        {rows.length === 0 && (
          <li className="px-3 py-2 text-sm text-zinc-400">
            {filter === "running" ? "Nothing is deployed." : "No integrations yet."}
          </li>
        )}
        <TopLevelDropZone />
      </ul>
    </aside>
  );
}

/** All / Running, each with how many it shows. */
function FilterToggle({
  filter,
  total,
  running,
  onFilter,
}: {
  filter: Filter;
  total: number;
  running: number;
  onFilter: (filter: Filter) => void;
}) {
  const option = (value: Filter, label: string, count: number) => (
    <button
      type="button"
      aria-pressed={filter === value}
      onClick={() => onFilter(value)}
      className={`flex items-center gap-1.5 rounded px-2 py-0.5 text-xs font-medium ${
        filter === value
          ? "bg-white text-zinc-800 shadow-sm dark:bg-zinc-700 dark:text-zinc-100"
          : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
      }`}
    >
      {label}
      <span className="tabular-nums text-zinc-400">{count}</span>
    </button>
  );
  return (
    <div className="flex items-center rounded-md bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
      {option("all", "All", total)}
      {option("running", "Running", running)}
    </div>
  );
}
