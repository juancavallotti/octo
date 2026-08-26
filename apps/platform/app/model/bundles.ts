/**
 * Bundles: an integration and every resource it owns as one zip archive, plus the
 * one other call whose payload is bytes — a frozen resource's contents.
 *
 * Split out of `orchestrator.ts` because these are the calls that carry an
 * encoding hop: an archive crosses the server-action boundary base64-encoded (a
 * server action's payload is serialized by React, so binary travels as text), and
 * that encoding stops here. Callers deal in bytes.
 */

import * as bundleActions from "@/app/actions/bundles";
import * as snapshotActions from "@/app/actions/snapshots";
import type { Integration } from "./orchestratorTypes";
import { fromBase64, toBase64 } from "./base64";
import { unwrap } from "./bff";

/** The raw bytes of one resource frozen under a version tag. */
export async function snapshotResourceContent(
  snapshotId: string,
  kind: string,
  name: string,
): Promise<Uint8Array> {
  return fromBase64(
    unwrap(
      await snapshotActions.snapshotResourceContent(snapshotId, kind, name),
    ),
  );
}

// --- Bundles --------------------------------------------------------------
// Backed by server actions in `app/actions/bundles.ts`. A bundle crosses the
// action boundary base64-encoded; the encoding stops here, and callers deal in
// bytes.

/** Download an integration's working copy as a bundle archive. */
export async function exportBundle(integrationId: string): Promise<Uint8Array> {
  return fromBase64(unwrap(await bundleActions.exportBundle(integrationId)));
}

/** Download a version tag's frozen contents as a bundle archive. */
export async function exportSnapshotBundle(
  snapshotId: string,
): Promise<Uint8Array> {
  return fromBase64(
    unwrap(await bundleActions.exportSnapshotBundle(snapshotId)),
  );
}

/**
 * Import a bundle as a new integration. `name` names an archive that carries no
 * manifest — the caller passes the uploaded filename's stem.
 */
export async function importBundle(
  archive: Uint8Array,
  name: string,
): Promise<Integration> {
  return unwrap(await bundleActions.importBundle(toBase64(archive), name));
}

/**
 * Overwrite an integration's definition and resource set from a bundle, keeping
 * its id, name, folder, version tags and deployments.
 */
export async function replaceBundle(
  integrationId: string,
  archive: Uint8Array,
): Promise<Integration> {
  return unwrap(
    await bundleActions.replaceBundle(integrationId, toBase64(archive)),
  );
}
