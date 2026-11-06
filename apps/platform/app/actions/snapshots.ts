"use server";

/**
 * Server actions for version tags (snapshots) — the BFF replacement for the
 * snapshot route handlers. Each action authorizes and delegates to the
 * orchestrator client lib; the model layer unwraps the ActionResult. The
 * orchestrator refuses to delete a deployed tag (#65); that 409 message flows back
 * through the result and surfaces in the UI.
 */

import type {
  Promotion,
  Snapshot,
  SnapshotDetail,
  SnapshotResource,
} from "@/app/model/orchestrator";
import { toBase64 } from "@/app/model/base64";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listSnapshots(
  integrationId: string,
): Promise<ActionResult<Snapshot[]>> {
  return withRead(() => client.listSnapshots(integrationId));
}

export async function getSnapshot(
  id: string,
): Promise<ActionResult<SnapshotDetail>> {
  return withRead(() => client.getSnapshot(id));
}

export async function createSnapshot(
  integrationId: string,
  tag: string,
): Promise<ActionResult<Snapshot>> {
  return withWrite(() => client.createSnapshot(integrationId, tag));
}

/**
 * Promote a version into another environment. Read access is all this boundary
 * asks for: the orchestrator judges the write by the caller's roles in the
 * target, which only it can see from the token.
 */
export async function promoteSnapshot(
  id: string,
  environment: string,
): Promise<ActionResult<Promotion>> {
  return withRead(() => client.promoteSnapshot(id, environment));
}

export async function deleteSnapshot(id: string): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteSnapshot(id));
}

export async function listSnapshotResources(
  snapshotId: string,
): Promise<ActionResult<SnapshotResource[]>> {
  return withRead(() => client.listSnapshotResources(snapshotId));
}

/**
 * One frozen resource's raw bytes, base64-encoded for the trip across the action
 * boundary (see `bundles.ts` for why bytes travel as text). The model layer
 * decodes; nothing above it sees the encoding.
 */
export async function snapshotResourceContent(
  snapshotId: string,
  kind: string,
  name: string,
): Promise<ActionResult<string>> {
  return withRead(async () => {
    const res = await client.snapshotResourceContent(snapshotId, kind, name);
    return res.ok ? { ok: true, data: toBase64(res.data) } : res;
  });
}
