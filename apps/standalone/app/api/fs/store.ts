/**
 * Local-disk flow store for the standalone app: reads and writes `*.yaml` flow
 * definitions in a single configured directory (OCTO_FS_DIR, default `./flows`,
 * mounted as a volume in the Docker image). Flat — no folders — so the editor's
 * folder UI stays hidden. Filenames are validated to a single safe segment so a
 * tampered id can never escape the root.
 *
 * The root is shared with two other modules, and each owns a disjoint slice of it:
 *
 *   store.ts             top-level `*.yaml` that are NOT test files — the flows
 *   testSuiteStore.ts    top-level `*_test.yaml` — the dolphin suites
 *   resourceStore.ts     everything else: subpaths, dotfiles, `.octo/`
 *
 * The test-file exclusion is this module's half of that split, and it mirrors the
 * runtime exactly: `octo` skips `*_test.yaml` when it loads a directory as a config
 * (runtime/core/runtime/config.go), which is what lets a suite live beside the flow it
 * tests. Without the same rule here, a suite would show up in the editor's folder
 * picker as an integration of its own.
 */

import { access, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
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

// A single `*.yaml`/`*.yml` filename living directly in the root: must start with
// an alphanumeric (so no leading-dot dotfiles) and contain only word/dot/dash
// characters — which excludes `/`, `\`, and any other separator, so no id can
// name a parent (`..`) or nested directory. This is the path-traversal guard.
const ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*\.ya?ml$/;

function nameOf(id: string): string {
  return id.replace(/\.ya?ml$/i, "");
}

/**
 * Whether a filename is a dolphin test suite rather than a flow: `orders_test.yaml`
 * tests `orders.yaml`, the way `orders_test.go` tests `orders.go`.
 *
 * The rule is the runtime's — `IsTestFile` in runtime/core/runtime/config.go, which
 * strips the extension and looks for a `_test` suffix. Keep the two in step: the whole
 * arrangement rests on the editor and the runtime agreeing about which files are flows.
 */
export function isTestFile(id: string): boolean {
  return /_test$/i.test(nameOf(id));
}

/**
 * Resolve a user-supplied id to an absolute path inside the store root, rejecting
 * anything that isn't a plain `*.yaml` filename in the root. Every read/write goes
 * through here, so a tampered id (`../../etc/passwd`, an absolute path, a nested
 * dir) can never escape the root: ID_RE blocks separators up front, and the
 * resolved-parent check is a second line of defense.
 */
function resolveSafe(id: string): string {
  if (!ID_RE.test(id)) throw new Error("invalid file name");
  // A test suite is not this store's to read or write. Excluding it from listFlows
  // alone would leave the partition a convention that a hand-made id could step over —
  // and the damaging direction is a write, which would silently overwrite a suite with
  // a flow definition. slugify() already maps `_` to `-`, so no id minted here can land
  // in this branch; it exists for the ones that were not minted here.
  if (isTestFile(id)) throw new Error("not a flow: test suites are stored separately");
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
    .filter((f) => /\.ya?ml$/i.test(f) && !isTestFile(f))
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

/** First unused `<slug>.yaml` (or `<slug>-N.yaml`) filename under the root. */
async function freeId(slug: string): Promise<string> {
  const root = fsRoot();
  let id = `${slug}.yaml`;
  let n = 2;
  while (await exists(path.join(root, id))) {
    id = `${slug}-${n}.yaml`;
    n++;
  }
  return id;
}

/** Create a new flow file from a name, de-duplicating the slug. */
export async function createFlow(
  name: string,
  definition: string,
): Promise<FlowDoc> {
  await mkdir(fsRoot(), { recursive: true });
  return writeFlow(await freeId(slugify(name)), definition);
}

/**
 * Update an existing flow. The filename is the flow's identity, so when the
 * name's slug no longer matches the current file the flow is renamed on disk:
 * the new file is written first, then the old one removed (a slug collision with
 * a different flow is de-duplicated to `-2`, `-3`, …). Returns the (possibly new)
 * record so the editor can adopt the new id.
 */
export async function updateFlow(
  oldId: string,
  name: string,
  definition: string,
): Promise<FlowDoc> {
  const current = resolveSafe(oldId); // validate before touching disk
  const desired = `${slugify(name)}.yaml`;
  if (desired === oldId) return writeFlow(oldId, definition);
  const written = await writeFlow(await freeId(slugify(name)), definition);
  await rm(current, { force: true }); // drop the old file only after the new one lands
  return written;
}
