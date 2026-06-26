import {
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
}

export const EMPTY: Data = {
  folders: [],
  integrations: [],
  membership: new Map(),
  order: new Map(),
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
  const lists = await Promise.all(
    flatten(folders).map((f) =>
      listFolderIntegrations(f.id).then((items) => ({
        folderId: f.id,
        ids: items.map((i) => i.id),
      })),
    ),
  );
  const membership = new Map<string, string>();
  const order = new Map<string, string[]>();
  for (const { folderId, ids } of lists) {
    order.set(folderId, ids);
    for (const id of ids) membership.set(id, folderId);
  }
  return { folders, integrations, membership, order };
}
