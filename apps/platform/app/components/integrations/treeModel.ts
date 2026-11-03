import type { Folder, Integration } from "@/app/model/orchestrator";
import type { Data } from "./managerData";
import type { FlatFolder } from "./model";

/**
 * The integrations tree as rows. Smart folders come first: their contents are
 * computed rather than filed — every deployed integration, every integration —
 * so they list each one flat, wherever it is filed. Then the real folders in
 * their stored order, each followed — when expanded — by its subfolders and then
 * its integrations, and last the integrations that sit in no folder.
 *
 * Built as data rather than in the component so the rules can be checked without
 * a pointer: what a smart folder holds, what a collapsed folder hides, and the
 * order inside a folder.
 */

/** The smart folders, in the order the tree shows them. */
export const SMART_FOLDERS = [
  { id: "running", label: "Running apps", title: "Every integration with a deployment" },
  { id: "all", label: "All integrations", title: "Every integration, wherever it is filed" },
] as const;

export type SmartFolderId = (typeof SMART_FOLDERS)[number]["id"];

/** The key a smart folder's open/closed state is remembered under, beside folder ids. */
export const smartKey = (id: SmartFolderId) => `smart:${id}`;

export type TreeRow =
  | {
      kind: "smart";
      id: SmartFolderId;
      label: string;
      title: string;
      expanded: boolean;
      count: number;
    }
  | {
      kind: "folder";
      folder: FlatFolder;
      /** Whether it has anything to show beneath it. */
      expandable: boolean;
      expanded: boolean;
      /** The integrations directly in it. */
      count: number;
    }
  | {
      kind: "integration";
      /** Unique per row: an integration also listed in a smart folder has two. */
      rowId: string;
      integration: Integration;
      depth: number;
      running: boolean;
      /** Listed by a smart folder: a copy of a row filed elsewhere, so it can be
       * dragged out but means no place to drop onto. */
      computed: boolean;
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
  collapsed: ReadonlySet<string>,
  /** A search over integration names; empty shows everything. */
  query = "",
): TreeRow[] {
  // While searching, only matches are listed, and every folder holding one is
  // open whatever it was left as — a match hidden in a closed folder is no match.
  const needle = query.trim().toLowerCase();
  const searching = needle !== "";
  const matches = (i: Integration) => !searching || i.name.toLowerCase().includes(needle);

  const leaf = (i: Integration, depth: number, parent: string, computed = false): TreeRow => ({
    kind: "integration",
    rowId: computed ? `${parent}:integration:${i.id}` : `integration:${i.id}`,
    integration: i,
    depth,
    running: data.deployed.has(i.id),
    computed,
  });

  const smart = SMART_FOLDERS.flatMap((f): TreeRow[] => {
    const holds = data.integrations.filter(
      (i) => (f.id === "all" || data.deployed.has(i.id)) && matches(i),
    );
    const expanded = !collapsed.has(smartKey(f.id));
    const row: TreeRow = { kind: "smart", ...f, expanded, count: holds.length };
    return expanded
      ? [row, ...holds.map((i) => leaf(i, 1, smartKey(f.id), true))]
      : [row];
  });

  const walk = (folders: Folder[], depth: number): TreeRow[] =>
    folders.flatMap((f) => {
      const below = walk(f.children ?? [], depth + 1);
      const own = folderIntegrations(data, f.id).filter(matches);
      if (searching && below.length === 0 && own.length === 0) return [];
      const expanded = searching || !collapsed.has(f.id);
      const row: TreeRow = {
        kind: "folder",
        folder: { id: f.id, name: f.name, parentId: f.parentId, depth },
        expandable: (f.children ?? []).length > 0 || own.length > 0,
        expanded,
        count: own.length,
      };
      return expanded
        ? [row, ...below, ...own.map((i) => leaf(i, depth + 1, f.id))]
        : [row];
    });

  const unfiled = data.integrations.filter(
    (i) => !data.membership.has(i.id) && matches(i),
  );
  return [...smart, ...walk(data.folders, 0), ...unfiled.map((i) => leaf(i, 0, "root"))];
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
