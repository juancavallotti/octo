import { rename, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import type { ResourceProvider } from "./resources";

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
