/**
 * Standalone resource provider for editor/MCP runs. The standalone runtime reads
 * an integration's resources (env files, templates) as plain files; here we resolve
 * the names a run declares directly from the flows directory (OCTO_FS_DIR) so
 * @octo/run-host can stage them into the run's namespace dir. Unlike the flow
 * store (which only handles single-segment `*.yaml` filenames), resources are
 * path-like — `.env.dev`, `templates/welcome.tmpl` — so this has its own resolver
 * that permits subpaths and dotfiles while still refusing to escape the root.
 */

import { readFile } from "node:fs/promises";
import path from "node:path";
import type { ResourceFile, ResourceProvider } from "@octo/run-host";
import { fsRoot } from "../api/fs/store";

/**
 * Resolve a path-like resource name to an absolute path inside the flows root, or
 * null when it would escape. Cleans the name as an absolute path (leading `..` and
 * absolute paths can't climb above root), joins it under root, and asserts
 * containment — mirroring the runtime's fs loader.
 */
function resolveResource(name: string): string | null {
  const root = path.resolve(fsRoot());
  const cleaned = path.normalize("/" + name);
  const full = path.resolve(root, "." + cleaned);
  if (full !== root && !full.startsWith(root + path.sep)) return null;
  return full;
}

/**
 * The standalone {@link ResourceProvider}: reads each declared name from the flows
 * dir, omitting any that are missing (ENOENT) or would escape the root. Resource
 * storage in standalone is flat under OCTO_FS_DIR and shared across flows, so this
 * ignores any integration id.
 */
export const fsResourceProvider: ResourceProvider = async (names) => {
  const files: ResourceFile[] = [];
  for (const name of names) {
    const full = resolveResource(name);
    if (!full) continue;
    try {
      files.push({ name, content: await readFile(full, "utf8") });
    } catch {
      // Missing resource: omit it — the runtime skips missing env resources and
      // reports missing templates itself.
    }
  }
  return files;
};
