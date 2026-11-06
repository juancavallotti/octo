"use server";

/**
 * Server actions for per-user API keys. The action is the trust boundary: it
 * resolves the caller's durable user id from the session (never from client
 * input) and delegates to the iam client, which scopes every operation to that
 * user. Each action returns an ActionResult; the model unwraps it.
 *
 * Keys are user-owned, so authorization is "any authenticated caller manages their
 * own" — there is no write-role gate (that is for cluster-admin operations).
 */

import type { ApiKey, CreatedApiKey } from "@/app/model/apikeys";
import type { ActionResult } from "@octo/http";
import * as client from "./client/apikeys";
import { withUser } from "./_auth";
import { callerEnvironment } from "@/app/auth/callerEnvironment";

export async function listApiKeys(): Promise<ActionResult<ApiKey[]>> {
  return withUser((userId) => client.listApiKeys(userId));
}

export async function createApiKey(
  name: string,
  roles: string[],
  expiresAt: string,
): Promise<ActionResult<CreatedApiKey>> {
  // Bound to the environment the caller is working in, which is also where the
  // roles they were offered are held: a key acts there and nowhere else.
  const environment = await callerEnvironment();
  return withUser((userId) => client.createApiKey(userId, name, roles, expiresAt, environment));
}

export async function deleteApiKey(id: string): Promise<ActionResult<void>> {
  return withUser((userId) => client.deleteApiKey(userId, id));
}
