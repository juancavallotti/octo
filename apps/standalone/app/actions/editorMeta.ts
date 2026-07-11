"use server";

/**
 * Server actions backing the editor's meta file (standalone). `.octo/editor-meta.json`
 * holds design-time bookkeeping — today the saved test inputs a flow can be run with —
 * and lives under the flows directory (OCTO_FS_DIR), beside the flows it describes.
 *
 * It is deliberately *undeclared*: no config references it, so run-host never stages it
 * into a run, and `listResources()` skips dot-directories, so it never shows up in the
 * Resources view. Standalone storage is flat and shared across flows, so one file
 * describes the whole directory and the integration id the editor passes is ignored.
 */

import type { ActionResult } from "@octo/http";
import { EDITOR_META_RESOURCE } from "@octo/editor";
import { readResource, writeResource } from "../api/fs/resourceStore";

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
