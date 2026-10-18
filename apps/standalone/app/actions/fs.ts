"use server";

/**
 * Server actions for the standalone filesystem. They drive the in-process disk store
 * directly (no HTTP, no auth: standalone is local-only). Each returns an ActionResult;
 * the editor's FileSystemCapability (localDiskFileSystem) unwraps it.
 */

import type { ActionResult } from "@octo/http";
import * as store from "../api/fs/store";
import type { ConfigFile, ProjectDoc } from "../api/fs/store";

/** The one project this app serves, as a listing shows it. */
export async function listProjects(): Promise<ActionResult<{ id: string; name: string }[]>> {
  try {
    const { id, name } = await store.readProject();
    return { ok: true, data: [{ id, name }] };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Read the project: every config file in the folder. */
export async function loadProject(): Promise<ActionResult<ProjectDoc>> {
  try {
    return { ok: true, data: await store.readProject() };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Make `files` the project's config files, removing any it no longer has. */
export async function saveProject(files: ConfigFile[]): Promise<ActionResult<ProjectDoc>> {
  if (!Array.isArray(files) || files.length === 0) {
    return { ok: false, error: "a project needs at least one config file" };
  }
  try {
    return { ok: true, data: await store.writeProject(files) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
