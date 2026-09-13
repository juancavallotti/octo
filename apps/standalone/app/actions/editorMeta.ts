"use server";

/**
 * Server actions backing the editor's meta file. `.octo/editor-meta.json` holds
 * design-time bookkeeping — today the saved test inputs a flow can be run with — under
 * the flows directory (OCTO_FS_DIR), beside the flows it describes.
 *
 * It is undeclared: no config references it, so it is never staged into a run, and
 * `listResources()` skips dot-directories, so it never shows up as a resource. Storage
 * is flat, so one file describes the whole directory and the integration id is ignored.
 */

import type { ActionResult } from "@octo/http";
import { readResource, writeResource } from "../api/fs/resourceStore";

/**
 * The meta resource name, mirroring the editor's EDITOR_META_RESOURCE. Inlined rather
 * than imported: the editor's barrel is a tree of "use client" components, and pulling
 * it into a server action drags React client code across the boundary.
 */
const EDITOR_META_RESOURCE = ".octo/editor-meta.json";

/** Read the meta file (empty string when it doesn't exist yet). */
export async function loadEditorMeta(): Promise<ActionResult<string>> {
  try {
    return { ok: true, data: (await readResource(EDITOR_META_RESOURCE)) ?? "" };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Persist the meta file, creating `.octo/` when it is not there. */
export async function saveEditorMeta(content: string): Promise<ActionResult<void>> {
  if (typeof content !== "string") {
    return { ok: false, error: "invalid content" };
  }
  try {
    await writeResource(EDITOR_META_RESOURCE, content);
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
