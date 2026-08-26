"use client";

import { useCallback, useMemo, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { Folder as FolderIcon, Workflow } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { type Bucket, type DragData, type DropData } from "./model";
import { resolveDrag } from "./dragDrop";
import { useIntegrationTree } from "./useIntegrationTree";
import IntegrationsToolbar from "./IntegrationsToolbar";
import IntegrationsUnavailable from "./IntegrationsUnavailable";
import ManagementNav from "@/app/components/ManagementNav";
import FolderTree from "./FolderTree";
import IntegrationList from "./IntegrationList";
import IntegrationDetail from "./IntegrationDetail";
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
  const { bucket, selectedId } = useMemo(
    () => parsePathname(pathname),
    [pathname],
  );
  const [activeDrag, setActiveDrag] = useState<DragData | null>(null);

  // A small activation distance keeps a plain click (select) distinct from a drag.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

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

  const {
    integrations,
    membership,
    flat,
    shown,
    selected,
    selectedFolderId,
    runningCount,
    unfiledCount,
    folderCount,
    createParent,
    busy,
    error,
    createFolderHere,
    renameFolderTo,
    removeFolder,
    importInput,
    replaceInput,
    onImportFile,
    downloadSelectedBundle,
    replaceSelectedFromBundle,
    copySelected,
    renameSelected,
    removeSelected,
    applyDrag,
  } = useIntegrationTree({
    available,
    bucket,
    selectedId,
    confirm,
    selectBucket,
    selectIntegration,
  });

  const onDragStart = (e: DragStartEvent) =>
    setActiveDrag((e.active.data.current as DragData | undefined) ?? null);

  // Resolve a drop, then carry it out. The rules — which pairs mean something,
  // which are no-ops, and that a folder may not land inside its own subtree —
  // live in dragDrop.ts, where they can be checked without a pointer.
  const onDragEnd = (e: DragEndEvent) => {
    setActiveDrag(null);
    applyDrag(
      resolveDrag(
        e.active.data.current as DragData | undefined,
        e.over?.data.current as DropData | DragData | undefined,
        { flat, membership },
      ),
    );
  };

  // Avoid flashing the "unavailable" message before the probe resolves.
  if (!ready) return null;

  if (!available) {
    return <IntegrationsUnavailable userMenu={userMenu} />;
  }

  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={userMenu}>
        <ManagementNav />
        <IntegrationsToolbar
          importInput={importInput}
          busy={busy}
          onImportFile={onImportFile}
        />
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
            runningCount={runningCount}
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
                replaceInput={replaceInput}
                onDownloadBundle={downloadSelectedBundle}
                onReplaceFromBundle={replaceSelectedFromBundle}
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
