/**
 * The folder tree that organizes integrations, and which folder holds which.
 * Membership is single-folder and readable only per folder.
 */

import { call, enc } from "./http";
import type { Folder, Integration } from "@/app/model/orchestrator";
import type { ActionResult } from "@octo/http";

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

/** Every folder id in a tree, depth-first. */
function folderIds(folders: Folder[]): string[] {
  return folders.flatMap((f) => [f.id, ...folderIds(f.children ?? [])]);
}

/**
 * Which integrations each folder holds, in its stored order, keyed by folder id.
 * The membership is only readable per folder, so this reads them all at once.
 */
export async function listFolderMembers(
  folders: Folder[],
): Promise<ActionResult<Record<string, string[]>>> {
  const ids = folderIds(folders);
  const lists = await Promise.all(ids.map((id) => listFolderIntegrations(id)));
  const members: Record<string, string[]> = {};
  for (const [i, list] of lists.entries()) {
    if (!list.ok) return list;
    members[ids[i]] = list.data.map((it) => it.id);
  }
  return { ok: true, data: members };
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
