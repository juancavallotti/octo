"use server";

/**
 * Server actions backing the editor's Dev .env panel (standalone). Dev-env values
 * live as the `.env.dev` resource on local disk (OCTO_FS_DIR) — the same file the
 * standalone runtime loads and run-host stages — rather than in the browser.
 * Standalone resource storage is flat and shared across flows, so there is one
 * `.env.dev`; the integration id the editor passes is ignored.
 */

import type { ActionResult } from "@octo/http";
import { DEV_ENV_RESOURCE } from "@octo/run-host";
import { readResource, writeResource } from "../api/fs/resourceStore";

/** Read the shared `.env.dev` content (empty string when unset). */
export async function loadDevEnv(): Promise<ActionResult<string>> {
  try {
    return { ok: true, data: (await readResource(DEV_ENV_RESOURCE)) ?? "" };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Persist the shared `.env.dev` content. */
export async function saveDevEnv(content: string): Promise<ActionResult<void>> {
  if (typeof content !== "string") {
    return { ok: false, error: "invalid content" };
  }
  try {
    await writeResource(DEV_ENV_RESOURCE, content);
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
