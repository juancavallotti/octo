"use server";

/**
 * Server actions for version tags (snapshots) — the BFF replacement for the
 * snapshot route handlers. Each action authorizes and delegates to the
 * orchestrator client lib; the model layer unwraps the ActionResult. The
 * orchestrator refuses to delete a deployed tag (#65); that 409 message flows back
 * through the result and surfaces in the UI.
 */

import type { Snapshot } from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listSnapshots(
  integrationId: string,
): Promise<ActionResult<Snapshot[]>> {
  return withRead(() => client.listSnapshots(integrationId));
}

export async function createSnapshot(
  integrationId: string,
  tag: string,
): Promise<ActionResult<Snapshot>> {
  return withWrite(() => client.createSnapshot(integrationId, tag));
}

export async function deleteSnapshot(id: string): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteSnapshot(id));
}
