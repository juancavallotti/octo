import type { Data } from "./managerData";

/**
 * How many integrations have something deployed. Counted over the integration
 * list rather than the deployed set, so an id left behind by an integration
 * deleted between two reads cannot inflate it.
 */
export function runningCountOf(data: Data): number {
  return data.integrations.filter((i) => data.deployed.has(i.id)).length;
}

/** One folder in the flattened tree the detail pane is given. */
export interface FlatFolder {
  id: string;
  name: string;
  parentId: string | null;
}

/**
 * The path of the folder an integration is filed under ("Parent / Child"), or
 * "No folder" when it is unfiled. Walks parents: a folder's display path is its
 * ancestry rather than its name.
 */
export function folderPathOf(
  folders: FlatFolder[],
  folderId: string | null,
): string {
  if (!folderId) return "No folder";
  const byId = new Map(folders.map((f) => [f.id, f]));
  const parts: string[] = [];
  let cur: FlatFolder | undefined = byId.get(folderId);
  while (cur) {
    parts.unshift(cur.name);
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return parts.join(" / ") || "No folder";
}
