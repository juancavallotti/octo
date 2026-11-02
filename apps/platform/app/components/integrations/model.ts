import type { Folder } from "@/app/model/orchestrator";

/** A folder flattened for indented rendering, keeping its depth and parent. */
export interface FlatFolder {
  id: string;
  name: string;
  parentId: string | null;
  depth: number;
}

/** Depth-first flatten of the folder tree. */
export function flatten(folders: Folder[], depth = 0): FlatFolder[] {
  return folders.flatMap((f) => [
    { id: f.id, name: f.name, parentId: f.parentId, depth },
    ...flatten(f.children ?? [], depth + 1),
  ]);
}

/**
 * Which integrations the tree shows: all of them, or only those with something
 * deployed. Running is derived rather than filed, so it narrows the tree rather
 * than being a place anything can be dragged.
 */
export type Filter = "all" | "running";

/** What a drag source carries (set as the draggable's `data`). */
export type DragData =
  | { kind: "integration"; id: string; name: string }
  | { kind: "folder"; id: string; name: string };

/** What a drop target accepts (set as the droppable's `data`). */
export type DropData =
  | { kind: "folder"; id: string } // file into / reparent under this folder
  | { kind: "root" }; // the top level: unfile an integration, lift a folder

/** Whether `candidateId` lies within `folderId`'s subtree (used to block a folder
 * being dropped onto itself or one of its own descendants). */
export function isDescendant(
  folders: FlatFolder[],
  candidateId: string,
  folderId: string,
): boolean {
  const parentOf = new Map(folders.map((f) => [f.id, f.parentId]));
  let p: string | null | undefined = candidateId;
  while (p) {
    if (p === folderId) return true;
    p = parentOf.get(p);
  }
  return false;
}
