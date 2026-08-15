import {
  listDeployments,
  listFolderIntegrations,
  listFolders,
  listIntegrations,
  type Folder,
  type Integration,
} from "@/app/model/orchestrator";
import { flatten } from "./model";

/** The data behind the management view, fetched together so it can be refreshed atomically. */
export interface Data {
  folders: Folder[];
  integrations: Integration[];
  /** integrationId -> folderId, for every filed integration. */
  membership: Map<string, string>;
  /** folderId -> its integration ids in stored order (the backend's order). */
  order: Map<string, string[]>;
  /** The integrations that have at least one deployment, filed or not. */
  deployed: Set<string>;
}

export const EMPTY: Data = {
  folders: [],
  integrations: [],
  membership: new Map(),
  order: new Map(),
  deployed: new Set(),
};

/**
 * Fetch the folder tree, all integrations, and derive folder membership and the
 * per-folder ordering. The orchestrator returns each folder's integrations already
 * in stored order, which we keep so the middle column can honor it.
 */
export async function loadData(): Promise<Data> {
  const [folders, integrations] = await Promise.all([
    listFolders(),
    listIntegrations(),
  ]);
  const [lists, deployed] = await Promise.all([
    Promise.all(
      flatten(folders).map((f) =>
        listFolderIntegrations(f.id).then((items) => ({
          folderId: f.id,
          ids: items.map((i) => i.id),
        })),
      ),
    ),
    loadDeployed(integrations),
  ]);
  const membership = new Map<string, string>();
  const order = new Map<string, string[]>();
  for (const { folderId, ids } of lists) {
    order.set(folderId, ids);
    for (const id of ids) membership.set(id, folderId);
  }
  return { folders, integrations, membership, order, deployed };
}

/**
 * Which integrations have something deployed. Deployments are listed per
 * integration, so one unreachable integration contributes nothing rather than
 * failing the load — the folder tree is still usable without this, and blanking
 * it over a deployment read would be a poor trade.
 */
async function loadDeployed(integrations: Integration[]): Promise<Set<string>> {
  const counts = await Promise.all(
    integrations.map((i) =>
      listDeployments(i.id).then(
        (ds) => (ds.length > 0 ? i.id : null),
        () => null,
      ),
    ),
  );
  return new Set(counts.filter((id): id is string => id !== null));
}
