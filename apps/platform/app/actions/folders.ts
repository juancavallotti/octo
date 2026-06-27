"use server";

/**
 * Server actions for folder organization — the BFF replacement for the
 * `/api/folders*` route handlers. Each action authorizes (session for reads,
 * write roles for mutations) and delegates to the orchestrator client lib; the
 * model layer (`app/model/orchestrator.ts`) unwraps the ActionResult.
 */

import type { Folder, Integration } from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listFolders(): Promise<ActionResult<Folder[]>> {
  return withRead(() => client.listFolders());
}

export async function createFolder(
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return withWrite(() => client.createFolder(name, parentId));
}

export async function renameFolder(
  id: string,
  name: string,
  parentId: string | null,
): Promise<ActionResult<Folder>> {
  return withWrite(() => client.renameFolder(id, name, parentId));
}

export async function deleteFolder(id: string): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteFolder(id));
}

export async function reorderFolders(
  parentId: string | null,
  folderIds: string[],
): Promise<ActionResult<void>> {
  return withWrite(() => client.reorderFolders(parentId, folderIds));
}

export async function listFolderIntegrations(
  folderId: string,
): Promise<ActionResult<Integration[]>> {
  return withRead(() => client.listFolderIntegrations(folderId));
}

export async function assignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.assignIntegration(folderId, integrationId));
}

export async function unassignIntegration(
  folderId: string,
  integrationId: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.unassignIntegration(folderId, integrationId));
}

export async function reorderFolderIntegrations(
  folderId: string,
  integrationIds: string[],
): Promise<ActionResult<void>> {
  return withWrite(() =>
    client.reorderFolderIntegrations(folderId, integrationIds),
  );
}
