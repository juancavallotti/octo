"use client";

import { useCallback, useMemo, useState } from "react";
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
import { flatten, type FlatFolder } from "./model";
import type { DragOutcome } from "./dragDrop";
import { EMPTY, loadData } from "./managerData";
import { useLoadable } from "@/app/components/ui/loadable";
import { runningCountOf } from "./managerViews";
import { folderIntegrations } from "./treeModel";
import { useIntegrationActions } from "./useIntegrationActions";

/**
 * The integration tree and everything you can do to it: the folders and
 * integrations themselves, the counts derived from them, and the mutations — create, import, duplicate, rename, delete, reorder.
 *
 * Every mutation goes through `run`, so all of them refresh on success and
 * surface their failure in the same inline banner. `renameSelected` is the one
 * exception, and returns a boolean: the inline editor has to stay open on a name
 * conflict rather than close as though it had worked.
 */
export function useIntegrationTree({
  available,
  selectedId,
  confirm,
  selectIntegration,
}: {
  /** The orchestrator probe resolved and it is there; nothing loads until it has. */
  available: boolean;
  selectedId: string | null;
  confirm: (opts: {
    title: string;
    body?: string;
    confirmLabel?: string;
    danger?: boolean;
  }) => Promise<boolean>;
  selectIntegration: (id: string | null) => void;
}) {
  // Nothing loads until the orchestrator probe says it is there.
  const tree = useLoadable(available ? "tree" : null, loadData);
  const { reload: refresh, update } = tree;
  const data = tree.state.status === "ready" ? tree.state.data : EMPTY;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

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

  const runningCount = useMemo(() => runningCountOf(data), [data]);

  const selected = integrations.find((i) => i.id === selectedId) ?? null;
  const selectedFolderId = selectedId
    ? (membership.get(selectedId) ?? null)
    : null;

  const {
    importInput,
    replaceInput,
    onImportFile,
    downloadSelectedBundle,
    replaceSelectedFromBundle,
    copySelected,
    renameSelected,
    setSelectedIcon,
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

  const createFolderIn = (name: string, parentId: string | null) =>
    run(() => createFolder(name, parentId));

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
    run(() => deleteFolder(f.id));
  };

  // Persist a new order for an integration's folder, optimistically reflecting it
  // so the tree doesn't snap back before the refresh lands.
  const reorderInFolder = (activeId: string, overId: string) => {
    const folderId = membership.get(activeId);
    if (!folderId) return;
    const ids = folderIntegrations(data, folderId).map((i) => i.id);
    const from = ids.indexOf(activeId);
    const to = ids.indexOf(overId);
    if (from === -1 || to === -1 || from === to) return;
    const next = arrayMove(ids, from, to);
    update((d) => ({ ...d, order: new Map(d.order).set(folderId, next) }));
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
        reorderInFolder(outcome.id, outcome.beforeId);
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
    /** The tree as loading, ready or failed; `data` is its contents once ready. */
    state: tree.state,
    data,
    flat,
    selected,
    selectedFolderId,
    runningCount,
    // Status. A failed refresh keeps the tree and reports through `error` too.
    busy,
    error: error ?? (tree.state.status === "ready" ? tree.state.error : null),
    setError,
    // Mutations.
    createFolderIn,
    renameFolderTo,
    removeFolder,
    importInput,
    replaceInput,
    onImportFile,
    downloadSelectedBundle,
    replaceSelectedFromBundle,
    copySelected,
    renameSelected,
    setSelectedIcon,
    removeSelected,
    applyDrag,
  };
}
