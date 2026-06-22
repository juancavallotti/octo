"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { ArrowLeft, Plus } from "lucide-react";
import {
  assignIntegration,
  createFolder,
  deleteFolder,
  deleteIntegration,
  listFolderIntegrations,
  listFolders,
  listIntegrations,
  renameFolder,
  unassignIntegration,
  type Folder,
  type Integration,
} from "@/app/model/orchestrator";
import { flatten, type Bucket, type FlatFolder } from "./model";
import FolderTree from "./FolderTree";
import IntegrationList from "./IntegrationList";
import IntegrationDetail from "./IntegrationDetail";
import SecretsManager from "./SecretsManager";
import ViewTabs, { type ManagementView } from "./ViewTabs";

/** The data behind the management view, fetched together so it can be refreshed atomically. */
interface Data {
  folders: Folder[];
  integrations: Integration[];
  /** integrationId -> folderId, for every filed integration. */
  membership: Map<string, string>;
}

const EMPTY: Data = { folders: [], integrations: [], membership: new Map() };

/** Fetch the folder tree, all integrations, and derive folder membership. */
async function loadData(): Promise<Data> {
  const [folders, integrations] = await Promise.all([
    listFolders(),
    listIntegrations(),
  ]);
  const lists = await Promise.all(
    flatten(folders).map((f) =>
      listFolderIntegrations(f.id).then((items) =>
        items.map((i) => [i.id, f.id] as const),
      ),
    ),
  );
  const membership = new Map<string, string>();
  for (const list of lists) for (const [iid, fid] of list) membership.set(iid, fid);
  return { folders, integrations, membership };
}

/**
 * The `/integrations` management view: a folder tree (with full CRUD) on the
 * left, the selected bucket's integrations in the middle, and operating details
 * for the selected integration on the right. All mutations go through the BFF
 * client and refresh the view. Folder membership is single-folder, derived by
 * querying each folder's members.
 */
export default function IntegrationsManager() {
  const [data, setData] = useState<Data>(EMPTY);
  const [view, setView] = useState<ManagementView>("integrations");
  const [bucket, setBucket] = useState<Bucket>("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(
    () =>
      loadData().then(setData, (e) => setError((e as Error).message)),
    [],
  );

  useEffect(() => {
    refresh();
  }, [refresh]);

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

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-black/10 px-4 h-12 shrink-0 dark:border-white/10">
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-zinc-600 transition-colors hover:bg-black/[0.04] dark:text-zinc-300 dark:hover:bg-white/[0.06]"
        >
          <ArrowLeft size={16} />
          Editor
        </Link>
        <ViewTabs view={view} onChange={setView} />
        {view === "integrations" && (
          <Link
            href="/"
            className="ml-auto inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
          >
            <Plus size={15} />
            New integration
          </Link>
        )}
      </header>

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
