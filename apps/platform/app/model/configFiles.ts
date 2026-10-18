/**
 * Browser-side calls for an integration's config files, backed by the server actions in
 * `app/actions/integrations.ts`. Re-exported from `./orchestrator`, the single import
 * for the orchestrator's surface.
 */

import * as integrationActions from "@/app/actions/integrations";
import { EDITOR_META_RESOURCE } from "@/app/run/editorMetaStore";
import { unwrap } from "./bff";
import { createResource, listResources } from "./resources";
import type { ConfigFile, Integration } from "./orchestratorTypes";

/** Every config file of an integration, with its content. */
export async function getConfigFiles(id: string): Promise<ConfigFile[]> {
  return unwrap(await integrationActions.readConfigFiles(id));
}

/** Make `files` the integration's whole set of config files. */
export async function replaceConfigFiles(
  id: string,
  files: { path: string; content: string }[],
): Promise<void> {
  unwrap(await integrationActions.replaceConfigFiles(id, files));
}

/**
 * Delete the integration `id` a failed create left half-made, and rethrow `cause`. A
 * delete that fails too says so: the leftover holds the name, so every retry would
 * otherwise be refused with no hint why.
 */
async function discardPartial(id: string, name: string, cause: unknown): Promise<never> {
  const removed = await integrationActions.deleteIntegration(id).catch((err: unknown) => ({
    ok: false as const,
    error: err instanceof Error ? err.message : String(err),
  }));
  if (!removed.ok) {
    const reason = cause instanceof Error ? cause.message : String(cause);
    throw new Error(`${reason}; the partial copy "${name}" could not be removed: ${removed.error}`);
  }
  throw cause;
}

/**
 * Create the integration "`name`" holding `files`. Creating it and writing its files are
 * two calls, so a set the orchestrator refuses deletes the integration again rather than
 * leaving an empty one behind.
 */
export async function createIntegrationWithFiles(
  name: string,
  files: { path: string; content: string }[],
): Promise<Integration> {
  const created = unwrap(await integrationActions.createIntegration({ name, definition: "" }));
  if (files.length === 0) return created;
  try {
    await replaceConfigFiles(created.id, files);
  } catch (err) {
    return discardPartial(created.id, name, err);
  }
  return created;
}

/**
 * Create "`name`" holding a copy of the integration `sourceId`: every config file, and
 * every resource — the templates and env files its config loads and the test suites
 * that check it. A copy missing a template its config names would not run.
 *
 * The editor's metadata file is left behind: it is keyed by the integration's id, so
 * the copy's editor would never read what the source's held. A resource that fails to
 * copy deletes the copy, as a refused config does, rather than leaving half of one.
 */
export async function duplicateIntegration(sourceId: string, name: string): Promise<Integration> {
  const [files, resources] = await Promise.all([
    getConfigFiles(sourceId),
    listResources(sourceId),
  ]);
  const created = await createIntegrationWithFiles(
    name,
    files.map(({ path, content }) => ({ path, content })),
  );
  try {
    for (const r of resources) {
      if (r.name === EDITOR_META_RESOURCE) continue;
      await createResource(created.id, r.kind, r.name, r.content);
    }
  } catch (err) {
    return discardPartial(created.id, name, err);
  }
  return created;
}
