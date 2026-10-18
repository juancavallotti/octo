import { rename, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import {
  effectiveResourceNames,
  injectDevEnvResource,
  stageResources,
  type ResourceProvider,
} from "./resources";

/**
 * Where a run's files go, and how they get there.
 *
 * Shared by everything that puts a config on disk for a local `octo` to read: the
 * long-running runner, a one-shot `invoke`, and a dolphin test run. Each of the three
 * writes into its own directory under the namespace's, so none of them can clobber
 * another's staged files — but they agree on where the namespace's directory is and on
 * how a config is written, and that agreement lives here.
 */

/** Options carrying the host's resource provider through a run. */
export interface RunResourceOptions {
  /** Resolves the resource files the run's config declares; omitted → no staging. */
  resources?: ResourceProvider;
}

/**
 * The directory a namespace's files live under, beneath OCTO_RUN_DIR (set by `task dev`)
 * or the system temp dir.
 */
export function namespaceDir(ns: string): string {
  return join(process.env.OCTO_RUN_DIR || tmpdir(), ns);
}

/** Atomic write (write sibling temp + rename) so `octo`'s dir watcher sees one event. */
export async function writeConfig(path: string, yaml: string): Promise<void> {
  const tmp = `${path}.tmp-${randomUUID()}`;
  await writeFile(tmp, yaml, "utf8");
  await rename(tmp, path);
}

/**
 * One config file of a project: its name in the project folder and its YAML. The
 * runtime reads only a folder's top level, so the path is a bare file name.
 */
export interface ProjectFile {
  path: string;
  content: string;
}

/**
 * Write a project into `dir` the way `octo` reads a folder: every config file side by
 * side, plus the resources any of them declares. The dev-env resource is declared once,
 * in the first file by name, because env resources merge across the folder and one
 * declaration is all the runtime needs. Returns the absolute paths of the staged
 * resources, for cleanup.
 *
 * A path that is not a bare `.yaml`/`.yml` name, or a test file, is refused: the runtime
 * would skip it, so a run would silently not see the file the caller thought it sent.
 */
export async function stageProject(
  dir: string,
  files: ProjectFile[],
  resources: ResourceProvider | undefined,
): Promise<string[]> {
  if (files.length === 0) throw new Error("a project needs at least one config file");
  const sorted = [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  for (const f of sorted) assertConfigName(f.path);

  for (const [i, f] of sorted.entries()) {
    await writeConfig(join(dir, f.path), i === 0 ? injectDevEnvResource(f.content) : f.content);
  }
  if (!resources) return [];
  const declared = [...new Set(sorted.flatMap((f) => effectiveResourceNames(f.content)))];
  return stageResources(dir, await resources(declared), declared);
}

/** Refuse a project path the runtime would not load as config. */
function assertConfigName(path: string): void {
  if (!/^[^/\\]+\.ya?ml$/i.test(path)) {
    throw new Error(`not a config file name: ${path}`);
  }
  if (/_test\.ya?ml$/i.test(path)) {
    throw new Error(`a test suite is not a config file: ${path}`);
  }
}
