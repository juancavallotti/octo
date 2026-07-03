"use server";

/**
 * Server actions backing the editor's Resources tab (standalone). Resources are
 * files on local disk under the flows dir (OCTO_FS_DIR) — the same root the
 * standalone runtime loads and run-host stages. Storage is flat and shared across
 * flows, so a resource is addressed by its path-like name, which doubles as its id.
 */

import type { ActionResult } from "@octo/http";
import {
  deleteResource,
  listResources,
  readResource,
  writeResource,
} from "../api/fs/resourceStore";

/** The editor's StoredResource shape (id/name/kind/content); id === name on disk. */
export interface DiskResource {
  id: string;
  name: string;
  kind: "env" | "template";
  content: string;
}

/** Env-convention names are env resources; everything else is a template. */
function kindFor(name: string): "env" | "template" {
  const base = (name.toLowerCase().split("/").pop() ?? "").trim();
  return base === ".env" || base.startsWith(".env.") || base.endsWith(".env")
    ? "env"
    : "template";
}

function toDisk(name: string, content: string): DiskResource {
  return { id: name, name, kind: kindFor(name), content };
}

export async function listResourcesAction(): Promise<ActionResult<DiskResource[]>> {
  try {
    const rows = await listResources();
    return { ok: true, data: rows.map((r) => toDisk(r.name, r.content)) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

export async function createResourceAction(
  name: string,
  content: string,
): Promise<ActionResult<DiskResource>> {
  try {
    if (!name.trim()) return { ok: false, error: "name is required" };
    if ((await readResource(name)) !== null) {
      return { ok: false, error: "a resource with that name already exists" };
    }
    await writeResource(name, content);
    return { ok: true, data: toDisk(name, content) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/**
 * Update a resource by id (its current name). A new `name` renames it (write the
 * target, remove the old file), preserving current content when none is supplied;
 * `content` alone rewrites in place.
 */
export async function updateResourceAction(
  id: string,
  name: string | null,
  content: string | null,
): Promise<ActionResult<DiskResource>> {
  try {
    const target = name ?? id;
    const text = content ?? (await readResource(id)) ?? "";
    if (target !== id) {
      if ((await readResource(target)) !== null) {
        return { ok: false, error: "a resource with that name already exists" };
      }
      await deleteResource(id);
    }
    await writeResource(target, text);
    return { ok: true, data: toDisk(target, text) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

export async function deleteResourceAction(
  id: string,
): Promise<ActionResult<void>> {
  try {
    await deleteResource(id);
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
