/**
 * Integrations and everything filed under one: the folder tree that organizes
 * them, the version tags that freeze them, and the env/template resources they
 * load. They travel together because a caller working on one is usually working
 * on the others.
 */

import { call, enc } from "./http";
import type {
  Folder,
  Integration,
  IntegrationInput,
  Resource,
  Snapshot,
  SnapshotResource,
} from "@/app/model/orchestrator";
import type { ActionResult } from "@octo/http";

// --- Integrations ---------------------------------------------------------

export function listIntegrations(): Promise<ActionResult<Integration[]>> {
  return call<Integration[]>("GET", "/integrations");
}

export function getIntegration(
  id: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("GET", `/integrations/${enc(id)}`);
}

export function createIntegration(
  input: IntegrationInput,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("POST", "/integrations", { ...input, actorId });
}

export function updateIntegration(
  id: string,
  input: IntegrationInput,
  actorId?: string,
): Promise<ActionResult<Integration>> {
  return call<Integration>("PUT", `/integrations/${enc(id)}`, {
    ...input,
    actorId,
  });
}

export function deleteIntegration(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/integrations/${enc(id)}`);
}

// --- Folders --------------------------------------------------------------

export function listFolders(): Promise<ActionResult<Folder[]>> {
  return call<Folder[]>("GET", "/folders");
}

export function createFolder(
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return call<Folder>("POST", "/folders", { name, parentId });
}

export function renameFolder(
  id: string,
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return call<Folder>("PUT", `/folders/${enc(id)}`, { name, parentId });
}

export function deleteFolder(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/folders/${enc(id)}`);
}

export function reorderFolders(
  parentId: string | null,
  folderIds: string[],
): Promise<ActionResult<void>> {
  return call<void>("PUT", "/folders/reorder", { parentId, folderIds });
}

export function listFolderIntegrations(
  folderId: string,
): Promise<ActionResult<Integration[]>> {
  return call<Integration[]>("GET", `/folders/${enc(folderId)}/integrations`);
}

export function assignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "PUT",
    `/folders/${enc(folderId)}/integrations/${enc(integrationId)}`,
  );
}

export function unassignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "DELETE",
    `/folders/${enc(folderId)}/integrations/${enc(integrationId)}`,
  );
}

export function reorderFolderIntegrations(
  folderId: string,
  integrationIds: string[],
): Promise<ActionResult<void>> {
  return call<void>("PUT", `/folders/${enc(folderId)}/integration-order`, {
    integrationIds,
  });
}

// --- Snapshots (version tags) ---------------------------------------------

export function listSnapshots(
  integrationId: string,
): Promise<ActionResult<Snapshot[]>> {
  return call<Snapshot[]>("GET", `/integrations/${enc(integrationId)}/snapshots`);
}

export function createSnapshot(
  integrationId: string,
  tag: string,
): Promise<ActionResult<Snapshot>> {
  return call<Snapshot>("POST", `/integrations/${enc(integrationId)}/snapshots`, {
    tag,
  });
}

export function deleteSnapshot(id: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/snapshots/${enc(id)}`);
}

/** The resources frozen alongside a tag's definition (metadata only; content is
 * served separately by the runtime's loader). */
export function listSnapshotResources(
  snapshotId: string,
): Promise<ActionResult<SnapshotResource[]>> {
  return call<SnapshotResource[]>(
    "GET",
    `/snapshots/${enc(snapshotId)}/resources`,
  );
}

// --- Resources ------------------------------------------------------------
// Every route is nested under the owning integration; the resource id alone is
// not a valid address.

export function listResources(
  integrationId: string,
): Promise<ActionResult<Resource[]>> {
  return call<Resource[]>("GET", `/integrations/${enc(integrationId)}/resources`);
}

export function getResource(
  integrationId: string,
  id: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "GET",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
  );
}

export function createResource(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "POST",
    `/integrations/${enc(integrationId)}/resources`,
    { kind, name, content },
  );
}

export function deleteResource(
  integrationId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>(
    "DELETE",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
  );
}

export function updateResource(
  integrationId: string,
  id: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return call<Resource>(
    "PUT",
    `/integrations/${enc(integrationId)}/resources/${enc(id)}`,
    { kind, name, content },
  );
}

/**
 * Create or replace a resource by its (path-like) name. Resource names are
 * addressed by the orchestrator's opaque id, not by name, so this lists the
 * integration's resources, and updates the match by id or creates a new one. Used
 * for name-keyed resources like `.env.dev` that the editor owns without tracking
 * their id.
 */
export async function upsertResourceByName(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  const existing = await listResources(integrationId);
  if (!existing.ok) return existing;
  const match = existing.data.find((r) => r.name === name);
  return match
    ? updateResource(integrationId, match.id, kind, name, content)
    : createResource(integrationId, kind, name, content);
}
