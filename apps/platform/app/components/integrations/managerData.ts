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
}

export const EMPTY: Data = {
  folders: [],
  integrations: [],
  membership: new Map(),
};

/** Fetch the folder tree, all integrations, and derive folder membership. */
export async function loadData(): Promise<Data> {
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
  for (const list of lists)
    for (const [iid, fid] of list) membership.set(iid, fid);
  return { folders, integrations, membership };
}
