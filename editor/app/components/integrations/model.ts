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

/** Which bucket of integrations the middle column shows. */
export type Bucket = "all" | "unfiled" | { folder: string };

/** Whether `bucket` is the given folder. */
export function isFolderBucket(bucket: Bucket, id: string): boolean {
  return typeof bucket === "object" && bucket.folder === id;
}
