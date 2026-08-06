"use server";

/**
 * Server actions for per-user API keys. The action is the trust boundary: it
 * resolves the caller's durable user id from the session (never from client
 * input) and delegates to the orchestrator client, which scopes every operation
 * to that user. Each action returns an ActionResult; the model unwraps it.
 *
 * Keys are user-owned, so authorization is "any authenticated caller manages their
 * own" — there is no write-role gate (that is for cluster-admin operations).
 */

import type { ApiKey, CreatedApiKey } from "@/app/model/apikeys";
import * as client from "./_client";
import type { ActionResult } from "./_client";
import { withUser } from "./_auth";

export async function listApiKeys(): Promise<ActionResult<ApiKey[]>> {
  return withUser((userId) => client.listApiKeys(userId));
}

export async function createApiKey(
  name: string,
  ttlSeconds: number,
): Promise<ActionResult<CreatedApiKey>> {
  return withUser((userId) => client.createApiKey(userId, name, ttlSeconds));
}

export async function deleteApiKey(id: string): Promise<ActionResult<void>> {
  return withUser((userId) => client.deleteApiKey(userId, id));
}
