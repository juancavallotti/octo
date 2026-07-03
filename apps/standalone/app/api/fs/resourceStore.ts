/**
 * Local-disk resource store for the standalone app: reads and writes an
 * integration's resources (env files, templates) under the flows directory
 * (OCTO_FS_DIR), the same root the standalone runtime loads them from. Separate
 * from the flow store (`store.ts`), which only handles single-segment `*.yaml`
 * filenames: resources are path-like — `.env.dev`, `templates/welcome.tmpl` — so
 * this permits subpaths and dotfiles while still refusing to escape the root.
 *
 * Standalone resource storage is flat and shared across flows (there is no
 * per-integration partition on local disk), so these are addressed by name alone.
 */

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fsRoot } from "./store";

/**
 * Resolve a path-like resource name to an absolute path inside the flows root.
 * Cleans the name as an absolute path (leading `..` and absolute paths can't climb
 * above root), joins it under root, and asserts containment — throwing on any name
 * that would escape.
 */
function resolveResource(name: string): string {
  const root = path.resolve(fsRoot());
  const full = path.resolve(root, "." + path.normalize("/" + name));
  if (full !== root && !full.startsWith(root + path.sep)) {
    throw new Error("invalid resource name");
  }
  return full;
}

/** Read a resource's content, or null when it doesn't exist. */
export async function readResource(name: string): Promise<string | null> {
  try {
    return await readFile(resolveResource(name), "utf8");
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return null;
    throw err;
  }
}

/** Write a resource's content, creating any parent directories the name implies. */
export async function writeResource(name: string, content: string): Promise<void> {
  const full = resolveResource(name);
  await mkdir(path.dirname(full), { recursive: true });
  await writeFile(full, content, "utf8");
}
