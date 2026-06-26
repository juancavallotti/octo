"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { Folder as FolderIcon, Plus, Workflow } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { arrayMove } from "@dnd-kit/sortable";
import {
  assignIntegration,
  createFolder,
  deleteFolder,
  deleteIntegration,
  renameFolder,
  reorderFolderIntegrations,
  reorderFolders,
  unassignIntegration,
} from "@/app/model/orchestrator";
import {
  flatten,
  isDescendant,
  type Bucket,
  type DragData,
  type DropData,
  type FlatFolder,
} from "./model";
import { EMPTY, loadData, type Data } from "./managerData";
import FolderTree from "./FolderTree";
import IntegrationList from "./IntegrationList";
import IntegrationDetail from "./IntegrationDetail";
import SecretsManager from "./SecretsManager";
import ViewTabs, { type ManagementView } from "./ViewTabs";

/**
 * The `/integrations` management view: a folder tree (with full CRUD) on the
 * left, the selected bucket's integrations in the middle, and operating details
 * for the selected integration on the right. All mutations go through the BFF
 * client and refresh the view. Folder membership is single-folder, derived by
 * querying each folder's members.
 */
export default function IntegrationsManager({
  initialView = "integrations",
  initialSelectedId = null,
  userMenu,
}: {
  /** Which top-level view to open on (e.g. "secrets" from the dashboard shortcut). */
  initialView?: ManagementView;
  /** Integration to preselect on open (e.g. a dashboard tile's "Manage"). */
  initialSelectedId?: string | null;
  /** Server-rendered account tile, shown in the shared header. */
  userMenu?: React.ReactNode;
} = {}) {
  const { available, ready } = useOrchestrator();
  const [data, setData] = useState<Data>(EMPTY);
  const [view, setView] = useState<ManagementView>(initialView);
  const [bucket, setBucket] = useState<Bucket>("all");
  const [selectedId, setSelectedId] = useState<string | null>(initialSelectedId);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeDrag, setActiveDrag] = useState<DragData | null>(null);

  // A small activation distance keeps a plain click (select) distinct from a drag.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const refresh = useCallback(
    () =>
      loadData().then(setData, (e) => setError((e as Error).message)),
    [],
  );

  useEffect(() => {
    if (available) refresh();
  }, [available, refresh]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const { folders, integrations, membership, order } = data;
  const flat = useMemo(() => flatten(folders), [folders]);

  const shown = useMemo(() => {
    if (bucket === "all") return integrations;
    if (bucket === "unfiled")
      return integrations.filter((i) => !membership.has(i.id));
    const inFolder = integrations.filter(
      (i) => membership.get(i.id) === bucket.folder,
    );
    // Honor the folder's stored order; ids missing from it (e.g. just assigned)
    // sort to the end by their natural list position.
    const pos = new Map((order.get(bucket.folder) ?? []).map((id, i) => [id, i]));
    return [...inFolder].sort(
      (a, b) =>
        (pos.get(a.id) ?? Number.MAX_SAFE_INTEGER) -
        (pos.get(b.id) ?? Number.MAX_SAFE_INTEGER),
    );
  }, [bucket, integrations, membership, order]);

  const unfiledCount = useMemo(
    () => integrations.filter((i) => !membership.has(i.id)).length,
    [integrations, membership],
  );
  const folderCount = (id: string) =>
    integrations.filter((i) => membership.get(i.id) === id).length;

  const selected = integrations.find((i) => i.id === selectedId) ?? null;
  const selectedFolderId = selectedId
    ? (membership.get(selectedId) ?? null)
    : null;

  // A new folder nests under the selected folder, else lives at the root.
  const createParent = typeof bucket === "object" ? bucket.folder : null;

  const createFolderHere = (name: string) =>
    run(() => createFolder(name, createParent));

  const renameFolderTo = (f: FlatFolder, name: string) =>
    run(() => renameFolder(f.id, name, f.parentId));

  const removeFolder = (f: FlatFolder) => {
    if (!confirm(`Delete folder "${f.name}"? Its integrations become unfiled.`))
      return;
    if (typeof bucket === "object" && bucket.folder === f.id) setBucket("all");
    run(() => deleteFolder(f.id));
  };

  const moveSelected = (folderId: string | null) => {
    if (!selectedId) return;
    const current = membership.get(selectedId) ?? null;
    if (folderId === current) return;
    run(async () => {
      if (folderId) await assignIntegration(folderId, selectedId);
      else if (current) await unassignIntegration(current, selectedId);
    });
  };

  const removeSelected = () => {
    if (!selected) return;
    if (!confirm(`Delete integration "${selected.name}"?`)) return;
    const id = selected.id;
    setSelectedId(null);
    run(() => deleteIntegration(id));
  };

  const onDragStart = (e: DragStartEvent) =>
    setActiveDrag((e.active.data.current as DragData | undefined) ?? null);

  // Persist a new order for the folder currently shown, optimistically reflecting
  // it so the list doesn't snap back before the refresh lands.
  const reorderShown = (activeId: string, overId: string) => {
    if (typeof bucket !== "object") return;
    const folderId = bucket.folder;
    const ids = shown.map((i) => i.id);
    const from = ids.indexOf(activeId);
    const to = ids.indexOf(overId);
    if (from === -1 || to === -1 || from === to) return;
    const next = arrayMove(ids, from, to);
    setData((d) => ({ ...d, order: new Map(d.order).set(folderId, next) }));
    run(() => reorderFolderIntegrations(folderId, next));
  };

  // Resolve a drop. Integrations file/unfile (onto a folder/Unfiled) or reorder
  // (onto a peer inside a folder); folders reparent (blocked from landing on
  // themselves or a descendant). No-op moves and unsupported pairs are ignored.
  const onDragEnd = (e: DragEndEvent) => {
    setActiveDrag(null);
    const a = e.active.data.current as DragData | undefined;
    // `over` is a drop zone (DropData) or, for the sortable list, a peer card (DragData).
    const o = e.over?.data.current as DropData | DragData | undefined;
    if (!a || !o) return;

    if (a.kind === "integration") {
      // Dropped on a peer card: reorder within the current folder.
      if (o.kind === "integration") {
        if (o.id !== a.id) reorderShown(a.id, o.id);
        return;
      }
      const current = membership.get(a.id) ?? null;
      if (o.kind === "folder" && o.id !== current) {
        run(() => assignIntegration(o.id, a.id));
      } else if (o.kind === "unfiled" && current) {
        run(() => unassignIntegration(current, a.id));
      }
      return; // dropping on "All" (root) is a no-op for integrations
    }

    // Folder dragged.
    const f = flat.find((x) => x.id === a.id);
    if (!f) return;

    if (o.kind === "folder") {
      if (o.id === a.id) return;
      const target = flat.find((x) => x.id === o.id);
      if (!target) return;
      // Onto a sibling: reorder within the group. Onto a folder in another group:
      // reparent under it (unless that would nest the folder inside itself).
      if ((target.parentId ?? null) === (f.parentId ?? null)) {
        reorderSiblings(f.parentId ?? null, a.id, o.id);
      } else if (!isDescendant(flat, o.id, a.id)) {
        run(() => renameFolder(a.id, f.name, o.id));
      }
      return;
    }

    // Onto the "All" bucket: lift to the top level (no-op if already a root).
    if (o.kind === "root" && (f.parentId ?? null) !== null) {
      run(() => renameFolder(a.id, f.name, null));
    }
  };

  // Persist a new order for the folders sharing a parent. Folders live in the tree
  // (not a flat order map), so a refresh — not an optimistic edit — reflects it;
  // the sortable animation covers the brief gap.
  const reorderSiblings = (
    parentId: string | null,
    activeId: string,
    overId: string,
  ) => {
    const siblings = flat
      .filter((x) => (x.parentId ?? null) === parentId)
      .map((x) => x.id);
    const from = siblings.indexOf(activeId);
    const to = siblings.indexOf(overId);
    if (from === -1 || to === -1 || from === to) return;
    run(() => reorderFolders(parentId, arrayMove(siblings, from, to)));
  };

  // Avoid flashing the "unavailable" message before the probe resolves.
  if (!ready) return null;

  if (!available) {
    return (
      <div className="flex h-full flex-col">
        <AppHeader userMenu={userMenu} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-sm text-zinc-500">
            Integration management is unavailable. Set{" "}
            <code className="rounded bg-black/[0.06] px-1 dark:bg-white/10">
              ORCHESTRATOR_URL
            </code>{" "}
            to enable it.
          </p>
          <Link href="/platform" className="text-sm text-sky-600 hover:underline">
            Back to dashboard
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={userMenu}>
        <ViewTabs view={view} onChange={setView} />
        {view === "integrations" && (
          <Link
            href="/platform/new"
            className="ml-auto inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
          >
            <Plus size={15} />
            New integration
          </Link>
        )}
      </AppHeader>

      {error && (
        <p className="border-b border-red-500/20 bg-red-500/5 px-4 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      {view === "secrets" ? (
        <div className="min-h-0 flex-1">
          <SecretsManager />
        </div>
      ) : (
        <DndContext
          sensors={sensors}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
          onDragCancel={() => setActiveDrag(null)}
        >
          <div className="flex min-h-0 flex-1">
            <FolderTree
              folders={flat}
              bucket={bucket}
              total={integrations.length}
              unfiledCount={unfiledCount}
              folderCount={folderCount}
              nesting={createParent !== null}
              onSelect={setBucket}
              onCreate={createFolderHere}
              onRename={renameFolderTo}
              onDelete={removeFolder}
            />

            <IntegrationList
              integrations={shown}
              selectedId={selectedId}
              onSelect={setSelectedId}
              reorderable={typeof bucket === "object"}
            />

            <div className="min-w-0 flex-1">
              {selected ? (
                <IntegrationDetail
                  integration={selected}
                  folders={flat}
                  folderId={selectedFolderId}
                  busy={busy}
                  onMove={moveSelected}
                  onDelete={removeSelected}
                />
              ) : (
                <div className="flex h-full items-center justify-center px-6 text-center text-sm text-zinc-400">
                  Select an integration to see its details.
                </div>
              )}
            </div>
          </div>

          <DragOverlay>
            {activeDrag ? (
              <div className="flex items-center gap-2 rounded-md border border-black/10 bg-white px-3 py-1.5 text-sm shadow-lg dark:border-white/15 dark:bg-zinc-900">
                {activeDrag.kind === "folder" ? (
                  <FolderIcon size={15} className="text-zinc-400" />
                ) : (
                  <Workflow size={15} className="text-zinc-400" />
                )}
                <span className="max-w-48 truncate">{activeDrag.name}</span>
              </div>
            ) : null}
          </DragOverlay>
        </DndContext>
      )}
    </div>
  );
}
