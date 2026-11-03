import {
  loadIntegrationTree,
  type Folder,
  type Integration,
  type IntegrationTree,
} from "@/app/model/orchestrator";

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

/** Fetch the whole tree in one round trip and index it for the view. */
export async function loadData(): Promise<Data> {
  return toData(await loadIntegrationTree());
}

/**
 * Index the tree as it travels — plain arrays and records — into the lookups the
 * view reads. Each folder's list keeps the backend's stored order, which the
 * folder honors when it shows its integrations.
 */
export function toData(tree: IntegrationTree): Data {
  const membership = new Map<string, string>();
  const order = new Map<string, string[]>();
  for (const [folderId, ids] of Object.entries(tree.members)) {
    order.set(folderId, ids);
    for (const id of ids) membership.set(id, folderId);
  }
  return {
    folders: tree.folders,
    integrations: tree.integrations,
    membership,
    order,
    deployed: new Set(tree.deployed),
  };
}
