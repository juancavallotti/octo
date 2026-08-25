import type { Integration } from "@/app/model/orchestrator";
import type { Bucket } from "./model";
import type { Data } from "./managerData";

/**
 * What the middle column shows, derived from the tree and the current bucket.
 *
 * Pure, and apart from the hook that memoizes them, because the ordering rule is
 * the sort of thing that is only ever seen through a rendered list: a folder
 * stores an explicit order, and an integration just assigned to it is not in that
 * order yet. It has to sort to the end rather than to the front, or a drag lands
 * a card somewhere nobody dropped it.
 */

/** The integrations this bucket shows, in the order it shows them. */
export function shownFor(bucket: Bucket, data: Data): Integration[] {
  const { integrations, membership, order, deployed } = data;
  if (bucket === "all") return integrations;
  if (bucket === "running") {
    return integrations.filter((i) => deployed.has(i.id));
  }
  if (bucket === "unfiled") {
    return integrations.filter((i) => !membership.has(i.id));
  }

  const inFolder = integrations.filter(
    (i) => membership.get(i.id) === bucket.folder,
  );
  // Honor the folder's stored order; ids missing from it (e.g. just assigned)
  // sort to the end by their natural list position.
  const pos = new Map((order.get(bucket.folder) ?? []).map((id, i) => [id, i]));
  return [...inFolder].sort(
    (a, b) =>
      (pos.get(a.id) ?? Number.MAX_SAFE_INTEGER) -
      (pos.get(b.id) ?? Number.MAX_SAFE_INTEGER),
  );
}

/**
 * How many integrations have something deployed.
 *
 * Counted over the integration list rather than over the deployed set, so an id
 * left behind by an integration deleted between two reads does not inflate a
 * count of rows the middle column would then not show.
 */
export function runningCountOf(data: Data): number {
  return data.integrations.filter((i) => data.deployed.has(i.id)).length;
}

/** How many integrations sit in no folder at all. */
export function unfiledCountOf(data: Data): number {
  return data.integrations.filter((i) => !data.membership.has(i.id)).length;
}

/** How many integrations sit directly in one folder. */
export function folderCountOf(data: Data, folderId: string): number {
  return data.integrations.filter((i) => data.membership.get(i.id) === folderId)
    .length;
}

/** One folder in the flattened tree the detail pane is given. */
export interface FlatFolder {
  id: string;
  name: string;
  parentId: string | null;
}

/**
 * The path of the folder an integration is filed under ("Parent / Child"), or
 * "No folder" when it is unfiled. Walks parents, so it is the one place that
 * knows a folder's display path is its ancestry rather than its name.
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
