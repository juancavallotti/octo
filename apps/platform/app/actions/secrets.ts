"use server";

/**
 * Server actions for cluster-wide secrets — the BFF replacement for the
 * `/api/secrets*` route handlers. Each action authorizes and delegates to the
 * orchestrator client lib; the model unwraps the ActionResult. Values are
 * write-only (there is no read-value action by design).
 */

import type { ClusterSecret } from "@/app/model/secrets";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listSecrets(): Promise<ActionResult<ClusterSecret[]>> {
  return withRead(() => client.listSecrets());
}

export async function setSecret(
  name: string,
  value: string,
): Promise<ActionResult<ClusterSecret>> {
  return withWrite(() => client.setSecret(name, value));
}

export async function deleteSecret(
  name: string,
  force = false,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteSecret(name, force));
}
