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

import { mkdir, readdir, readFile, rename, rm, writeFile } from "node:fs/promises";
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

/** Delete a resource file; a missing file is a no-op. */
export async function deleteResource(name: string): Promise<void> {
  await rm(resolveResource(name), { force: true });
}

/** Rename a resource file, creating the destination's parent dirs as needed. */
export async function renameResource(from: string, to: string): Promise<void> {
  const dest = resolveResource(to);
  await mkdir(path.dirname(dest), { recursive: true });
  await rename(resolveResource(from), dest);
}

/** A resource file on disk: its path-like name and content. */
export interface ResourceEntry {
  name: string;
  content: string;
}

/**
 * List the resource files under the flows root, depth-first. Every top-level
 * single-segment `*.yaml`/`*.yml` is excluded, because those belong to the other two
 * stores sharing this root: a flow document (store.ts) or a dolphin test suite
 * (testSuiteStore.ts). Dot-directories and node_modules are skipped too; dotfiles like
 * `.env.dev` are kept. Names are returned posix-style (with `/`) regardless of platform.
 */
export async function listResources(): Promise<ResourceEntry[]> {
  const root = path.resolve(fsRoot());
  const out: ResourceEntry[] = [];

  const walk = async (dir: string, rel: string): Promise<void> => {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch {
      return; // dir not created yet
    }
    for (const entry of entries) {
      const childRel = rel ? `${rel}/${entry.name}` : entry.name;
      if (entry.isDirectory()) {
        if (entry.name.startsWith(".") || entry.name === "node_modules") continue;
        await walk(path.join(dir, entry.name), childRel);
        continue;
      }
      if (!entry.isFile()) continue;
      // A top-level *.yaml/*.yml is a flow document or a test suite, not a resource.
      if (rel === "" && /\.ya?ml$/i.test(entry.name)) continue;
      const content = (await readResource(childRel)) ?? "";
      out.push({ name: childRel, content });
    }
  };

  await walk(root, "");
  out.sort((a, b) => a.name.localeCompare(b.name));
  return out;
}
