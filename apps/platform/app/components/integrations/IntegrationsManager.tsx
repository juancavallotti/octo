"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { fromDefinitionYaml } from "@octo/editor";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { Folder as FolderIcon, Plus, Upload, Workflow } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { arrayMove } from "@dnd-kit/sortable";
import {
  assignIntegration,
  createFolder,
  createIntegration,
  deleteFolder,
  deleteIntegration,
  renameFolder,
  reorderFolderIntegrations,
  reorderFolders,
  unassignIntegration,
  updateIntegration,
} from "@/app/model/orchestrator";
import {
  flatten,
  type Bucket,
  type DragData,
  type DropData,
  type FlatFolder,
} from "./model";
import { resolveDrag } from "./dragDrop";
import { EMPTY, loadData, type Data } from "./managerData";
import ManagementNav from "@/app/components/ManagementNav";
import FolderTree from "./FolderTree";
import IntegrationList from "./IntegrationList";
import IntegrationDetail from "./IntegrationDetail";
import { nameFromFilename } from "./yamlFile";
import {
  INTEGRATIONS_BASE,
  buildPath,
  parsePathname,
  type ManagerSelection,
} from "./query";

/**
 * The `/integrations` management route: a folder tree (with full CRUD) on the
 * left, the selected bucket's integrations in the middle, and operating details
 * for the selected integration on the right. All mutations go through the BFF
 * client and refresh the view. Folder membership is single-folder, derived by
 * querying each folder's members. Sibling sections (secrets, queues) are their own
 * routes, reached via the shared ManagementNav in the header.
 */
export default function IntegrationsManager({
  userMenu,
}: {
  /** Server-rendered account tile, shown in the shared header. */
  userMenu?: React.ReactNode;
} = {}) {
  const confirm = useConfirm();
  const { available, ready } = useOrchestrator();
  const router = useRouter();
  const pathname = usePathname();

  // The URL is the source of truth for the selection, so it's bookmarkable and
  // navigated client-side: the bucket + integration are derived from the path, and
  // selecting something is a router navigation (below). The manager lives in the
  // route's layout, so these navigations don't remount it — only the path updates.
  const [data, setData] = useState<Data>(EMPTY);
  const { bucket, selectedId } = useMemo(
    () => parsePathname(pathname),
    [pathname],
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeDrag, setActiveDrag] = useState<DragData | null>(null);

  // A small activation distance keeps a plain click (select) distinct from a drag.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const refresh = useCallback(
    () => loadData().then(setData, (e) => setError((e as Error).message)),
    [],
  );

  useEffect(() => {
    if (available) refresh();
  }, [available, refresh]);

  // Navigate to a selection (client-side; updates the path the view derives from).
  const go = useCallback(
    (sel: ManagerSelection) =>
      router.push(`${INTEGRATIONS_BASE}${buildPath(sel)}`, { scroll: false }),
    [router],
  );
  const selectBucket = useCallback(
    (b: Bucket) => go({ bucket: b, selectedId }),
    [go, selectedId],
  );
  const selectIntegration = useCallback(
    (id: string | null) => go({ bucket, selectedId: id }),
    [go, bucket],
  );

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
    const pos = new Map(
      (order.get(bucket.folder) ?? []).map((id, i) => [id, i]),
    );
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

  const removeFolder = async (f: FlatFolder) => {
    const ok = await confirm({
      title: `Delete folder "${f.name}"?`,
      body: "Its integrations become unfiled.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    if (typeof bucket === "object" && bucket.folder === f.id) selectBucket("all");
    run(() => deleteFolder(f.id));
  };

  // Hidden file input backing the "Import" button. Importing a .yaml always
  // creates a new integration (name from the filename); the file's contents are
  // the runtime definition, validated before the create so malformed YAML fails
  // fast with an inline error instead of a broken record.
  const importInput = useRef<HTMLInputElement>(null);

  const onImportFile = async (file: File) => {
    setError(null);
    const text = await file.text();
    try {
      fromDefinitionYaml(text);
    } catch (e) {
      setError(`Invalid integration YAML: ${(e as Error).message}`);
      return;
    }
    run(async () => {
      const created = await createIntegration({
        name: nameFromFilename(file.name),
        definition: text,
      });
      selectIntegration(created.id);
    });
  };

  // Duplicate the selected integration into a fresh "Copy of …" record, then
  // select the copy. Its definition is already loaded in the list, so no fetch.
  const copySelected = () => {
    if (!selected) return;
    run(async () => {
      const created = await createIntegration({
        name: `Copy of ${selected.name}`,
        definition: selected.definition,
      });
      selectIntegration(created.id);
    });
  };

  // Rename the selected integration (its name is effectively its filename),
  // preserving the definition. The updated name lands via the refresh. Returns
  // whether the rename succeeded so the inline editor can stay open on conflict
  // (e.g. a duplicate name); failures still surface in the inline error banner.
  const renameSelected = async (name: string): Promise<boolean> => {
    if (!selected) return false;
    setBusy(true);
    setError(null);
    try {
      await updateIntegration(selected.id, {
        name,
        definition: selected.definition,
      });
      await refresh();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const removeSelected = async () => {
    if (!selected) return;
    const ok = await confirm({
      title: `Delete integration "${selected.name}"?`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    const id = selected.id;
    selectIntegration(null);
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

  // Resolve a drop, then carry it out. The rules — which pairs mean something,
  // which are no-ops, and that a folder may not land inside its own subtree —
  // live in dragDrop.ts, where they can be checked without a pointer.
  const onDragEnd = (e: DragEndEvent) => {
    setActiveDrag(null);
    const outcome = resolveDrag(
      e.active.data.current as DragData | undefined,
      e.over?.data.current as DropData | DragData | undefined,
      { flat, membership },
    );
    switch (outcome.kind) {
      case "reorder-integration":
        reorderShown(outcome.id, outcome.beforeId);
        break;
      case "file-integration":
        run(() => assignIntegration(outcome.folderId, outcome.integrationId));
        break;
      case "unfile-integration":
        run(() => unassignIntegration(outcome.folderId, outcome.integrationId));
        break;
      case "reorder-folder":
        reorderSiblings(outcome.parentId, outcome.id, outcome.beforeId);
        break;
      case "reparent-folder":
        run(() => renameFolder(outcome.id, outcome.name, outcome.parentId));
        break;
      case "none":
        break;
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
        <AppHeader userMenu={userMenu}>
          <ManagementNav />
        </AppHeader>
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-sm text-zinc-500">
            Integration management is unavailable. Set{" "}
            <code className="rounded bg-black/[0.06] px-1 dark:bg-white/10">
              ORCHESTRATOR_URL
            </code>{" "}
            to enable it.
          </p>
          <Link
            href="/platform"
            className="text-sm text-sky-600 hover:underline"
          >
            Back to dashboard
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={userMenu}>
        <ManagementNav />
        <div className="ml-auto flex items-center gap-2">
          <input
            ref={importInput}
            type="file"
            accept=".yaml,.yml"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              // Reset first so re-selecting the same file fires onChange again.
              e.target.value = "";
              if (file) onImportFile(file);
            }}
          />
          <button
            type="button"
            onClick={() => importInput.current?.click()}
            disabled={busy}
            className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 disabled:opacity-50 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
          >
            <Upload size={15} />
            Import
          </button>
          <Link
            href="/platform/new"
            className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
          >
            <Plus size={15} />
            New integration
          </Link>
        </div>
      </AppHeader>

      {error && (
        <p className="border-b border-red-500/20 bg-red-500/5 px-4 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

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
            onSelect={selectBucket}
            onCreate={createFolderHere}
            onRename={renameFolderTo}
            onDelete={removeFolder}
          />

          <IntegrationList
            integrations={shown}
            selectedId={selectedId}
            onSelect={selectIntegration}
            reorderable={typeof bucket === "object"}
          />

          <div className="min-w-0 flex-1">
            {selected ? (
              <IntegrationDetail
                key={selected.id}
                integration={selected}
                folders={flat}
                folderId={selectedFolderId}
                busy={busy}
                onDelete={removeSelected}
                onCopy={copySelected}
                onRename={renameSelected}
              />
            ) : (
              <div className="flex h-full items-center justify-center px-6 text-center text-sm text-zinc-400">
                Select an integration to see its details.
              </div>
            )}
          </div>
        </div>

        {/* No drop animation: a successful move/reorder lands the item in its new
              place via the refresh, so animating the overlay back to its origin
              would read as a (misleading) snap-back. */}
        <DragOverlay dropAnimation={null}>
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
    </div>
  );
}
