/**
 * Integration resources — the env files and templates a definition loads. Backed
 * by the server actions in `app/actions/resources.ts`; these wrappers unwrap the
 * ActionResult so callers keep a value-or-throw contract.
 *
 * Downloading a resource frozen under a version tag lives in `bundles.ts`, with
 * the other calls whose payload is bytes.
 */

import * as resourceActions from "@/app/actions/resources";
import type { Resource } from "./orchestratorTypes";
import { unwrap } from "./bff";

/** List an integration's resources (env files, templates). */
export async function listResources(
  integrationId: string,
): Promise<Resource[]> {
  return unwrap(await resourceActions.listResources(integrationId));
}

/** Create a resource under an integration. */
export async function createResource(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<Resource> {
  return unwrap(
    await resourceActions.createResource(integrationId, kind, name, content),
  );
}

/** Delete an integration's resource. */
export async function deleteResource(
  integrationId: string,
  id: string,
): Promise<void> {
  return unwrap(await resourceActions.deleteResource(integrationId, id));
}
