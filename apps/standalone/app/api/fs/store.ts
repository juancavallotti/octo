/**
 * Local-disk flow store for the standalone app: reads and writes `*.yaml` flow
 * definitions in a single configured directory (OCTO_FS_DIR, default `./flows`,
 * mounted as a volume in the Docker image). Flat — no folders — so the editor's
 * folder UI stays hidden. Filenames are validated to a single safe segment so a
 * tampered id can never escape the root.
 */

import { access, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

export interface FlowDoc {
  id: string;
  name: string;
  definition: string;
}

/** The directory backing the store. */
export function fsRoot(): string {
  return process.env.OCTO_FS_DIR || path.join(process.cwd(), "flows");
}

/** A single `*.yaml`/`*.yml` filename, no path separators. */
const ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*\.ya?ml$/;

function nameOf(id: string): string {
  return id.replace(/\.ya?ml$/i, "");
}

/** Resolve an id to an absolute path inside the root, rejecting anything else. */
function resolveSafe(id: string): string {
  if (!ID_RE.test(id)) throw new Error("invalid file name");
  const root = path.resolve(fsRoot());
  const full = path.resolve(root, id);
  if (path.dirname(full) !== root) throw new Error("invalid file path");
  return full;
}

function slugify(name: string): string {
  return (
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "flow"
  );
}

async function exists(full: string): Promise<boolean> {
  try {
    await access(full);
    return true;
  } catch {
    return false;
  }
}

export async function listFlows(): Promise<{ id: string; name: string }[]> {
  let entries: string[];
  try {
    entries = await readdir(fsRoot());
  } catch {
    return []; // dir not created yet
  }
  return entries
    .filter((f) => /\.ya?ml$/i.test(f))
    .sort()
    .map((id) => ({ id, name: nameOf(id) }));
}

export async function readFlow(id: string): Promise<FlowDoc> {
  const definition = await readFile(resolveSafe(id), "utf8");
  return { id, name: nameOf(id), definition };
}

export async function writeFlow(id: string, definition: string): Promise<FlowDoc> {
  const full = resolveSafe(id);
  await mkdir(path.dirname(full), { recursive: true });
  await writeFile(full, definition, "utf8");
  return { id, name: nameOf(id), definition };
}

/** Create a new flow file from a name, de-duplicating the slug. */
export async function createFlow(
  name: string,
  definition: string,
): Promise<FlowDoc> {
  const root = fsRoot();
  await mkdir(root, { recursive: true });
  const slug = slugify(name);
  let id = `${slug}.yaml`;
  let n = 2;
  while (await exists(path.join(root, id))) {
    id = `${slug}-${n}.yaml`;
    n++;
  }
  return writeFlow(id, definition);
}
