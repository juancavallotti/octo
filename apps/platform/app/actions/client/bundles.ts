/**
 * Bundles: a whole integration as one zip — its definition plus every resource it
 * owns. Export hangs off the thing being exported (an integration or one of its
 * version tags); import creates an integration, replace overwrites the one it
 * addresses. The frozen-resource read lives here too, because it is the other
 * call in this client whose response is bytes rather than JSON.
 */

import { callBytes, callWithBytes, enc } from "./http";
import type { Integration } from "@/app/model/orchestrator";
import type { ActionResult } from "@octo/http";

/**
 * One frozen resource's raw bytes. Kind and name are query parameters rather than
 * path segments because a resource name may itself contain slashes.
 */
export function snapshotResourceContent(
  snapshotId: string,
  kind: string,
  name: string,
): Promise<ActionResult<Uint8Array>> {
  const query = `kind=${enc(kind)}&name=${enc(name)}`;
  return callBytes(
    "GET",
    `/snapshots/${enc(snapshotId)}/resources/content?${query}`,
  );
}

/** The content type a bundle is served as and uploaded as. */
const ARCHIVE_CONTENT_TYPE = "application/zip";

export function exportIntegrationBundle(
  integrationId: string,
): Promise<ActionResult<Uint8Array>> {
  return callBytes("GET", `/integrations/${enc(integrationId)}/bundle`);
}

export function exportSnapshotBundle(
  snapshotId: string,
): Promise<ActionResult<Uint8Array>> {
  return callBytes("GET", `/snapshots/${enc(snapshotId)}/bundle`);
}

/**
 * Import a bundle as a new integration. `name` names an archive that carries no
 * manifest (the caller passes the uploaded filename's stem); a manifest's own name
 * wins over it, and a name already in use is suffixed rather than rejected.
 */
export function importIntegrationBundle(
  archive: Uint8Array,
  name: string,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  const query = new URLSearchParams({ name });
  if (actorId) query.set("actorId", actorId);
  return callWithBytes<Integration>(
    "POST",
    `/integrations/bundle?${query.toString()}`,
    archive,
    ARCHIVE_CONTENT_TYPE,
  );
}

/** Overwrite an integration's definition and resource set from a bundle. */
export function replaceIntegrationBundle(
  integrationId: string,
  archive: Uint8Array,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  const query = actorId ? `?actorId=${enc(actorId)}` : "";
  return callWithBytes<Integration>(
    "PUT",
    `/integrations/${enc(integrationId)}/bundle${query}`,
    archive,
    ARCHIVE_CONTENT_TYPE,
  );
}
