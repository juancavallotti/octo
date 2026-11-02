import type { Folder, Integration } from "@/app/model/orchestrator";
import type { Data } from "./managerData";
import type { FlatFolder, Filter } from "./model";

/**
 * The integrations tree as rows: folders in their stored order, each followed —
 * when expanded — by its subfolders and then its integrations, with the
 * integrations that sit in no folder at the top level after the folders.
 *
 * Built as data rather than in the component so the rules can be checked without
 * a pointer: which folders survive the Running filter, what a collapsed folder
 * hides, and the order inside a folder.
 */

export type TreeRow =
  | {
      kind: "folder";
      folder: FlatFolder;
      /** Whether it has anything to show beneath it, under the current filter. */
      expandable: boolean;
      expanded: boolean;
      /** The integrations directly in it, under the current filter. */
      count: number;
    }
  | {
      kind: "integration";
      integration: Integration;
      depth: number;
      running: boolean;
    };

/**
 * A folder's integrations in the order it stores. One just assigned to it is not
 * in that order yet: it sorts to the end rather than the front, or a drag lands it
 * somewhere nobody dropped it.
 */
export function folderIntegrations(data: Data, folderId: string): Integration[] {
  const inFolder = data.integrations.filter(
    (i) => data.membership.get(i.id) === folderId,
  );
  const pos = new Map((data.order.get(folderId) ?? []).map((id, i) => [id, i]));
  return [...inFolder].sort(
    (a, b) =>
      (pos.get(a.id) ?? Number.MAX_SAFE_INTEGER) -
      (pos.get(b.id) ?? Number.MAX_SAFE_INTEGER),
  );
}

export function treeRows(
  data: Data,
  filter: Filter,
  collapsed: ReadonlySet<string>,
): TreeRow[] {
  const shows = (i: Integration) => filter === "all" || data.deployed.has(i.id);
  const leaf = (i: Integration, depth: number): TreeRow => ({
    kind: "integration",
    integration: i,
    depth,
    running: data.deployed.has(i.id),
  });

  // Returns the rows of the folders worth showing at this level. Under Running a
  // folder is worth showing only if something deployed sits somewhere beneath it.
  const walk = (folders: Folder[], depth: number): TreeRow[] =>
    folders.flatMap((f) => {
      const below = walk(f.children ?? [], depth + 1);
      const own = folderIntegrations(data, f.id).filter(shows);
      if (filter === "running" && below.length === 0 && own.length === 0) return [];

      const expanded = !collapsed.has(f.id);
      const row: TreeRow = {
        kind: "folder",
        folder: { id: f.id, name: f.name, parentId: f.parentId, depth },
        expandable: below.length > 0 || own.length > 0,
        expanded,
        count: own.length,
      };
      return expanded
        ? [row, ...below, ...own.map((i) => leaf(i, depth + 1))]
        : [row];
    });

  const unfiled = data.integrations.filter(
    (i) => !data.membership.has(i.id) && shows(i),
  );
  return [...walk(data.folders, 0), ...unfiled.map((i) => leaf(i, 0))];
}

/** The folders an integration sits inside, innermost first; empty when unfiled. */
export function ancestorsOf(
  data: Data,
  flat: FlatFolder[],
  integrationId: string,
): string[] {
  const parentOf = new Map(flat.map((f) => [f.id, f.parentId]));
  const out: string[] = [];
  let at = data.membership.get(integrationId) ?? null;
  while (at) {
    out.push(at);
    at = parentOf.get(at) ?? null;
  }
  return out;
}
