"use server";

/**
 * Server actions for integration bundles: a whole integration — its definition
 * and every resource it owns — as one zip. The BFF layer over the orchestrator's
 * bundle routes, following the same shape as the rest: authorize, delegate to the
 * client lib, let the model layer unwrap the ActionResult.
 *
 * The archives cross this boundary base64-encoded. A server action's arguments and
 * result are serialized by React, so a binary document travels as text; the model
 * layer does the decoding, and nothing above it sees the encoding.
 */

import type { Integration } from "@/app/model/orchestrator";
import { fromBase64, toBase64 } from "@/app/model/base64";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

/** Download an integration's working copy as a bundle. */
export async function exportBundle(
  integrationId: string,
): Promise<ActionResult<string>> {
  return withRead(async () =>
    encoded(await client.exportIntegrationBundle(integrationId)),
  );
}

/** Download a version tag's frozen definition and resources as a bundle. */
export async function exportSnapshotBundle(
  snapshotId: string,
): Promise<ActionResult<string>> {
  return withRead(async () =>
    encoded(await client.exportSnapshotBundle(snapshotId)),
  );
}

/**
 * Import a bundle as a new integration. `name` names an archive that carries no
 * manifest; the caller passes the uploaded filename's stem.
 */
export async function importBundle(
  archive: string,
  name: string,
): Promise<ActionResult<Integration>> {
  return withWrite((session) =>
    client.importIntegrationBundle(fromBase64(archive), name, session.user.id),
  );
}

/** Overwrite an integration's definition and resource set from a bundle. */
export async function replaceBundle(
  integrationId: string,
  archive: string,
): Promise<ActionResult<Integration>> {
  return withWrite((session) =>
    client.replaceIntegrationBundle(
      integrationId,
      fromBase64(archive),
      session.user.id,
    ),
  );
}

/** Re-wrap a byte result as a base64 one, leaving a failure untouched. */
function encoded(result: ActionResult<Uint8Array>): ActionResult<string> {
  return result.ok ? { ok: true, data: toBase64(result.data) } : result;
}
