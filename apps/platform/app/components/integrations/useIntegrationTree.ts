"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { arrayMove } from "@dnd-kit/sortable";
import {
  assignIntegration,
  createFolder,
  deleteFolder,
  renameFolder,
  reorderFolderIntegrations,
  reorderFolders,
  unassignIntegration,
} from "@/app/model/orchestrator";
import { flatten, type Bucket, type FlatFolder } from "./model";
import type { DragOutcome } from "./dragDrop";
import { EMPTY, loadData, type Data } from "./managerData";
import { folderCountOf, shownFor, unfiledCountOf } from "./managerViews";
import { useIntegrationActions } from "./useIntegrationActions";

/**
 * The integration tree and everything you can do to it: the folders and
 * integrations themselves, the views derived from them for the current bucket,
 * and the mutations — create, import, duplicate, rename, delete, reorder.
 *
 * The manager keeps what is left: where the selection lives (the URL), the
 * drag-and-drop wiring, and the rendering. That split is the reason this is a
 * hook rather than a second component — the two halves share a great deal of
 * state and almost no concerns.
 *
 * Every mutation goes through `run`, so all of them refresh on success and
 * surface their failure in the same inline banner. `renameSelected` is the one
 * exception, and returns a boolean: the inline editor has to stay open on a name
 * conflict rather than close as though it had worked.
 */
export function useIntegrationTree({
  available,
  bucket,
  selectedId,
  confirm,
  selectBucket,
  selectIntegration,
}: {
  /** The orchestrator probe resolved and it is there; nothing loads until it has. */
  available: boolean;
  bucket: Bucket;
  selectedId: string | null;
  confirm: (opts: {
    title: string;
    body?: string;
    confirmLabel?: string;
    danger?: boolean;
  }) => Promise<boolean>;
  selectBucket: (b: Bucket) => void;
  selectIntegration: (id: string | null) => void;
}) {
  const [data, setData] = useState<Data>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const refresh = useCallback(
    () => loadData().then(setData, (e) => setError((e as Error).message)),
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

  const { folders, integrations, membership } = data;
  const flat = useMemo(() => flatten(folders), [folders]);

  const shown = useMemo(() => shownFor(bucket, data), [bucket, data]);
  const unfiledCount = useMemo(() => unfiledCountOf(data), [data]);
  const folderCount = (id: string) => folderCountOf(data, id);

  const selected = integrations.find((i) => i.id === selectedId) ?? null;
  const selectedFolderId = selectedId
    ? (membership.get(selectedId) ?? null)
    : null;

  // A new folder nests under the selected folder, else lives at the root.
  const createParent = typeof bucket === "object" ? bucket.folder : null;

  const {
    importInput,
    onImportFile,
    copySelected,
    renameSelected,
    removeSelected,
  } = useIntegrationActions({
    selected,
    run,
    refresh,
    confirm,
    selectIntegration,
    setBusy,
    setError,
  });

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



  /** Carry out a resolved drop. Paired with resolveDrag, which decides it. */
  const applyDrag = (outcome: DragOutcome) => {
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

  return {
    // The tree, and the views of it this bucket needs.
    folders,
    integrations,
    membership,
    flat,
    shown,
    selected,
    selectedFolderId,
    unfiledCount,
    folderCount,
    /** A new folder would nest here — the selected folder, or the root. */
    createParent,
    // Status.
    busy,
    error,
    setError,
    // Mutations.
    createFolderHere,
    renameFolderTo,
    removeFolder,
    importInput,
    onImportFile,
    copySelected,
    renameSelected,
    removeSelected,
    applyDrag,
  };
}
