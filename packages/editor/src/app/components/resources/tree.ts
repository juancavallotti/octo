import type { ReconciledResource } from "./reconcile";

/**
 * Folders are virtual: the backend stores flat, name-keyed resources, so the tree
 * is derived by splitting each name on "/". buildTree groups the reconciled
 * entries into a nested folder structure; flattenTree renders it to an ordered row
 * list honoring the collapsed set (folders before files, alphabetical per level).
 */

export interface FolderNode {
  name: string;
  /** Full path prefix, e.g. "templates/email" (no trailing slash). */
  path: string;
  folders: FolderNode[];
  files: ReconciledResource[];
}

export type TreeRow =
  | { kind: "folder"; name: string; path: string; depth: number }
  | { kind: "file"; entry: ReconciledResource; label: string; depth: number };

/** The leaf segment of a path-like name ("templates/x.tmpl" → "x.tmpl"). */
export function leafName(name: string): string {
  return name.split("/").pop() || name;
}

export function buildTree(entries: ReconciledResource[]): FolderNode {
  const root: FolderNode = { name: "", path: "", folders: [], files: [] };

  for (const entry of entries) {
    const parts = entry.name.split("/");
    parts.pop(); // drop the leaf; the folders are the remaining segments
    let node = root;
    let prefix = "";
    for (const part of parts) {
      if (part === "") continue;
      prefix = prefix ? `${prefix}/${part}` : part;
      let child = node.folders.find((f) => f.name === part);
      if (!child) {
        child = { name: part, path: prefix, folders: [], files: [] };
        node.folders.push(child);
      }
      node = child;
    }
    // Store the entry unchanged — it keeps its full path for store/declaration ops.
    node.files.push(entry);
  }

  sortNode(root);
  return root;
}

function sortNode(node: FolderNode): void {
  node.folders.sort((a, b) => a.name.localeCompare(b.name));
  node.files.sort((a, b) => leafName(a.name).localeCompare(leafName(b.name)));
  node.folders.forEach(sortNode);
}

export function flattenTree(
  root: FolderNode,
  collapsed: ReadonlySet<string>,
  depth = 0,
): TreeRow[] {
  const rows: TreeRow[] = [];
  for (const folder of root.folders) {
    rows.push({ kind: "folder", name: folder.name, path: folder.path, depth });
    if (!collapsed.has(folder.path)) {
      rows.push(...flattenTree(folder, collapsed, depth + 1));
    }
  }
  for (const file of root.files) {
    rows.push({ kind: "file", entry: file, label: leafName(file.name), depth });
  }
  return rows;
}

/**
 * Ensure a folder node exists for each given path (creating intermediate nodes),
 * so empty "pending" folders — ones the user made but hasn't filed anything into
 * yet — still appear in the tree as drop targets. Idempotent: paths that already
 * exist from a file's name are left as-is. Mutates and returns `root`.
 */
export function ensureFolders(root: FolderNode, paths: Iterable<string>): FolderNode {
  for (const path of paths) {
    let node = root;
    let prefix = "";
    for (const part of path.split("/")) {
      if (part === "") continue;
      prefix = prefix ? `${prefix}/${part}` : part;
      let child = node.folders.find((f) => f.name === part);
      if (!child) {
        child = { name: part, path: prefix, folders: [], files: [] };
        node.folders.push(child);
      }
      node = child;
    }
  }
  sortNode(root);
  return root;
}

/** Every folder path in the tree (used to expand/collapse all). */
export function allFolderPaths(root: FolderNode): string[] {
  const out: string[] = [];
  const walk = (node: FolderNode) => {
    for (const f of node.folders) {
      out.push(f.path);
      walk(f);
    }
  };
  walk(root);
  return out;
}
