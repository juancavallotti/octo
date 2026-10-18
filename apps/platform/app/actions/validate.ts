"use server";

/**
 * The server action behind the editor's runtime validation: `octo validate` over a
 * project's files, run as a child of this process like the other one-shots.
 */

import { binaries, validate } from "@octo/run-host";
import type { RuntimeIssue, StoredFile } from "@octo/editor";
import type { ActionResult } from "@octo/http";
import { ensureRunNamespace } from "@/app/run/namespace";
import { withRead } from "./_auth";
import { resourcesFor } from "./_run";

/**
 * Check a saved integration's files with `octo validate`, staged with the resources it
 * owns. A draft is refused rather than checked without them, which would report every
 * template it declares missing; the editor then keeps its own verdict.
 */
export async function runValidate(
  tabId: string,
  files: StoredFile[],
  integrationId?: string,
): Promise<ActionResult<RuntimeIssue[]>> {
  return withRead(async () => {
    if (!integrationId) return { ok: false, error: "an unsaved integration is checked by the editor alone" };
    if (!binaries().available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (!Array.isArray(files) || files.length === 0) {
      return { ok: false, error: "missing `files`" };
    }
    try {
      const r = await validate(await ensureRunNamespace(tabId), files, {
        resources: resourcesFor(integrationId),
      });
      if (!r.ok) return { ok: false, error: r.error ?? "validation did not run" };
      return { ok: true, data: r.issues };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}
