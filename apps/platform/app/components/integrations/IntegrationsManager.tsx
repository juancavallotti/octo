"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Plus } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import {
  assignIntegration,
  createFolder,
  deleteFolder,
  deleteIntegration,
  renameFolder,
  unassignIntegration,
} from "@/app/model/orchestrator";
import { flatten, type Bucket, type FlatFolder } from "./model";
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

  const { folders, integrations, membership } = data;
  const flat = useMemo(() => flatten(folders), [folders]);

  const shown = useMemo(() => {
    if (bucket === "all") return integrations;
    if (bucket === "unfiled")
      return integrations.filter((i) => !membership.has(i.id));
    return integrations.filter((i) => membership.get(i.id) === bucket.folder);
  }, [bucket, integrations, membership]);

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
      )}
    </div>
  );
}
