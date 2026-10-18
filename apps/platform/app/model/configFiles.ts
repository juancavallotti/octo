/**
 * Browser-side calls for an integration's config files, backed by the server actions in
 * `app/actions/integrations.ts`. Re-exported from `./orchestrator`, the single import
 * for the orchestrator's surface.
 */

import * as integrationActions from "@/app/actions/integrations";
import { unwrap } from "./bff";
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
    await integrationActions.deleteIntegration(created.id);
    throw err;
  }
  return created;
}

/** Create "`name`" holding a copy of every config file of the integration `sourceId`. */
export async function duplicateIntegration(sourceId: string, name: string): Promise<Integration> {
  const files = await getConfigFiles(sourceId);
  return createIntegrationWithFiles(
    name,
    files.map(({ path, content }) => ({ path, content })),
  );
}
