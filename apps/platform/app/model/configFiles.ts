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

/** Create "`name`" holding a copy of every config file of the integration `sourceId`. */
export async function duplicateIntegration(sourceId: string, name: string): Promise<Integration> {
  const files = await getConfigFiles(sourceId);
  const created = unwrap(await integrationActions.createIntegration({ name, definition: "" }));
  if (files.length > 0) {
    await replaceConfigFiles(
      created.id,
      files.map(({ path, content }) => ({ path, content })),
    );
  }
  return created;
}
