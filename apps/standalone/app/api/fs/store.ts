/**
 * Local-disk project store: the configured directory (OCTO_FS_DIR, default `./flows`)
 * is one project, and its config files are the top-level `*.yaml` files in it — the
 * same files `octo run --config <dir>` merges into one application. Flat — no folders —
 * and filenames are validated to a single safe segment, so a tampered path can never
 * escape the root.
 *
 * The root is shared with two other modules, and each owns a disjoint slice of it:
 *
 *   store.ts             top-level `*.yaml` that are NOT test files — the config files
 *   testSuiteStore.ts    top-level `*_test.yaml` — the dolphin suites
 *   resourceStore.ts     everything else: subpaths, dotfiles, `.octo/`
 *
 * The test-file exclusion mirrors the runtime, which skips `*_test.yaml` when it loads
 * a directory as a config — that is what lets a suite live beside the flow it tests.
 */

import { mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import YAML from "yaml";
import { noteWritten } from "./watch";

/** A config file: its name in the root and its YAML. */
export interface ConfigFile {
  path: string;
  content: string;
}

/** The project the root holds. */
export interface ProjectDoc {
  id: string;
  name: string;
  files: ConfigFile[];
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

/**
 * A file's name without its extension. Exported so nothing grows a second copy of
 * the rule.
 */
export function nameOf(id: string): string {
  return id.replace(/\.ya?ml$/i, "");
}

/** Whether a filename is one this store would serve: a config file or a suite in the root. */
export function isStoredFile(name: string): boolean {
  return ID_RE.test(name);
}

/**
 * Whether a filename is a dolphin test suite rather than a flow: `orders_test.yaml`
 * tests `orders.yaml`, the way `orders_test.go` tests `orders.go`. The rule is the
 * runtime's own — strip the extension, look for a `_test` suffix — and must stay in
 * step with it.
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
  // A test suite is not this store's to read or write, and the damaging direction is
  // a write: it would overwrite a suite with a flow definition. slugify() maps `_` to
  // `-`, so only an id minted elsewhere reaches this branch.
  if (isTestFile(id)) throw new Error("not a flow: test suites are stored separately");
  const root = path.resolve(fsRoot());
  const full = path.resolve(root, id);
  if (path.dirname(full) !== root) throw new Error("invalid file path");
  return full;
}

/**
 * The project's id: the root folder's name. There is one project per root, so it
 * needs no other identity — and the desktop app's switching folders is switching
 * projects, which is what an id that follows the folder says.
 */
export function projectId(): string {
  return path.basename(path.resolve(fsRoot()));
}

/** The config files in the root, by name. */
export async function listConfigFiles(): Promise<string[]> {
  let entries: string[];
  try {
    entries = await readdir(fsRoot());
  } catch {
    return []; // dir not created yet
  }
  return entries.filter((f) => isStoredFile(f) && !isTestFile(f)).sort();
}

/**
 * Read the project: every config file. Its name is the `service.name` the files
 * declare, falling back to the folder's — the same identity the runtime logs under.
 */
export async function readProject(): Promise<ProjectDoc> {
  const files = await Promise.all(
    (await listConfigFiles()).map(async (p) => ({
      path: p,
      content: await readFile(resolveSafe(p), "utf8"),
    })),
  );
  return { id: projectId(), name: serviceName(files) ?? projectId(), files };
}

/** The first `service.name` the files declare, in the order the runtime merges them. */
function serviceName(files: ConfigFile[]): string | undefined {
  for (const f of files) {
    try {
      const name = (YAML.parse(f.content) as { service?: { name?: unknown } } | null)?.service?.name;
      if (typeof name === "string" && name.trim()) return name;
    } catch {
      // A file that does not parse names nothing; the runtime will say why.
    }
  }
  return undefined;
}

/**
 * Make `files` the project's config files: each is written, and a config file on disk
 * that is not among them is removed. Test suites and resources are not config files and
 * are never touched.
 */
export async function writeProject(files: ConfigFile[]): Promise<ProjectDoc> {
  const targets = files.map((f) => resolveSafe(f.path));
  if (new Set(files.map((f) => f.path)).size !== files.length) {
    throw new Error("a file is given more than once");
  }
  await mkdir(fsRoot(), { recursive: true });
  for (const [i, f] of files.entries()) {
    // Recorded BEFORE the write, so the watcher cannot see the file change before it
    // has been told whose change it was — which would announce the editor's own save
    // back to it as somebody else's.
    noteWritten(f.path, f.content);
    await writeFile(targets[i], f.content, "utf8");
  }
  const keep = new Set(files.map((f) => f.path));
  for (const stale of await listConfigFiles()) {
    if (!keep.has(stale)) await rm(resolveSafe(stale), { force: true });
  }
  return readProject();
}
