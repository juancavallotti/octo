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

import { authEnabled } from "@/auth";
import { AuthError, requireSession } from "@/app/auth/guard";
import type { ApiKey, CreatedApiKey } from "@/app/model/apikeys";
import * as client from "./_client";
import type { ActionResult } from "./_client";

// Stable identity for the local (no-SSO) dev session, which has no OIDC subject.
// Bootstrapping it on demand gives `task dev` a real user row to own keys.
const LOCAL_SUBJECT = "local-dev";
const LOCAL_EMAIL = "local@localhost";
const LOCAL_NAME = "Local Dev";

/**
 * Resolve the durable orchestrator user id for the caller. With SSO it comes from
 * the session (bootstrapped at sign-in). In local dev there is no IdP, so a stable
 * sentinel user is bootstrapped on demand and its id used. Throws AuthError when
 * no user can be resolved, which the action wrapper maps to an error result.
 */
async function currentUserId(): Promise<string> {
  if (!authEnabled) {
    const res = await client.bootstrapUser(LOCAL_SUBJECT, LOCAL_EMAIL, LOCAL_NAME);
    if (!res.ok) throw new AuthError(res.error);
    return res.data.id;
  }
  const session = await requireSession();
  const id = session.user.id;
  if (!id) throw new AuthError("user not provisioned");
  return id;
}

/**
 * Resolve the caller's user id and run `fn` scoped to it, mapping an
 * authorization/provisioning failure to an error result so the action never
 * throws across the boundary.
 */
async function withUser<T>(
  fn: (userId: string) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  let userId: string;
  try {
    userId = await currentUserId();
  } catch (err) {
    if (err instanceof AuthError) return { ok: false, error: err.message };
    throw err;
  }
  return fn(userId);
}

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
