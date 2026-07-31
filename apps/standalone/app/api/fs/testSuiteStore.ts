/**
 * Local-disk store for dolphin test suites: the `*_test.yaml` files sitting directly in
 * the flows root (OCTO_FS_DIR), beside the flows they test. This is the third of the
 * three modules that share that root, and the partition is exact:
 *
 *   store.ts             top-level `*.yaml` that are NOT test files — the flows
 *   testSuiteStore.ts    top-level `*_test.yaml` — this module
 *   resourceStore.ts     everything else: subpaths, dotfiles, `.octo/`
 *
 * These are real, committable files, not editor bookkeeping — that is the whole point of
 * the Testing tab. `octo run` skips them when it loads the directory as a config
 * (runtime/core/runtime/config.go), and `dolphin test <dir>` picks up every one of them,
 * so a suite written here gives the same verdict from a terminal as it does in the tab.
 *
 * Storage is flat and shared across documents, as `.env.dev` and the resources are: the
 * root is one octo config directory. So the integration id is not a partition key here,
 * and `listSuites` returns every suite in the root. That is safe because the runtime
 * already requires flow names to be unique across a merged config, so the flow a suite
 * names identifies it unambiguously within the directory.
 */

import { access, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { flowOfSuite, isSuiteFileName, suiteFileName } from "@octo/editor/runtime";
import { fsRoot } from "./store";

/** A stored suite: the flow it tests, and its YAML. */
export interface SuiteFile {
  flow: string;
  content: string;
}

/**
 * Resolve a suite filename to an absolute path inside the root. Every read and write
 * goes through here: the shared name check blocks separators up front (it accepts only
 * a single safe segment) and the resolved-parent check is the second line of defence.
 */
function resolveSafe(file: string): string {
  if (!isSuiteFileName(file) || file.includes("/")) {
    throw new Error("invalid test suite name");
  }
  const root = path.resolve(fsRoot());
  const full = path.resolve(root, file);
  if (path.dirname(full) !== root) throw new Error("invalid test suite path");
  return full;
}

async function exists(full: string): Promise<boolean> {
  try {
    await access(full);
    return true;
  } catch {
    return false;
  }
}

/** Every suite filename in the root, sorted. */
async function suiteFiles(): Promise<string[]> {
  try {
    return (await readdir(fsRoot())).filter(isSuiteFileName).sort();
  } catch {
    return []; // dir not created yet
  }
}

/** Every suite in the root, keyed by the flow each one declares. */
export async function listSuites(): Promise<SuiteFile[]> {
  const out: SuiteFile[] = [];
  for (const file of await suiteFiles()) {
    let content: string;
    try {
      content = await readFile(path.join(fsRoot(), file), "utf8");
    } catch {
      continue; // vanished between the listing and the read
    }
    out.push({ flow: flowOfSuite(content, file), content });
  }
  return out;
}

/** The file already holding this flow's suite, or null when it has none. */
async function fileForFlow(flow: string): Promise<string | null> {
  for (const file of await suiteFiles()) {
    let content: string;
    try {
      content = await readFile(path.join(fsRoot(), file), "utf8");
    } catch {
      continue;
    }
    if (flowOfSuite(content, file) === flow) return file;
  }
  return null;
}

/**
 * The name to create this flow's suite under. Two distinct flow names can slug to the
 * same stem (`Order Intake` and `order-intake`), so a taken name is de-duplicated the
 * way `store.ts` de-duplicates a flow filename, rather than one suite silently replacing
 * the other's.
 */
async function freeSuiteFile(flow: string): Promise<string> {
  const first = suiteFileName(flow);
  let file = first;
  let n = 2;
  while (await exists(path.join(fsRoot(), file))) {
    file = first.replace(/_test\.yaml$/, `-${n}_test.yaml`);
    n++;
  }
  return file;
}

/**
 * Write a flow's suite, into the file already holding it when there is one. Resolving by
 * declaration rather than by name means a suite stays put across a slug collision, and
 * that a file a human renamed by hand is still the one we update.
 */
export async function writeSuite(flow: string, content: string): Promise<void> {
  await mkdir(fsRoot(), { recursive: true });
  const file = (await fileForFlow(flow)) ?? (await freeSuiteFile(flow));
  await writeFile(resolveSafe(file), content, "utf8");
}

/** Delete a flow's suite; a flow with no suite is the normal starting state. */
export async function deleteSuite(flow: string): Promise<void> {
  const file = await fileForFlow(flow);
  if (file) await rm(resolveSafe(file), { force: true });
}
