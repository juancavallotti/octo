/**
 * Health, users and API keys: the calls that answer "is the orchestrator there,
 * and who is asking". Grouped apart from the resource CRUD because they are what
 * the app needs before any of it.
 */

import { baseUrl, call, enc } from "./http";
import { requestOk } from "@octo/http";
import type { User } from "@/app/model/orchestrator";
import type { ApiKey, CreatedApiKey, VerifiedApiKey } from "@/app/model/apikeys";
import type { ActionResult } from "@octo/http";

// --- Health ---------------------------------------------------------------

/**
 * Whether the orchestrator is configured and answers its health check. Uses a
 * body-less probe (the `/healthz` response is plain text "ok", not JSON), so it
 * doesn't go through the JSON client.
 */
export function checkHealth(): Promise<boolean> {
  const base = baseUrl();
  if (!base) return Promise.resolve(false);
  return requestOk("GET", `${base}/healthz`);
}

// --- Users ----------------------------------------------------------------

/**
 * Provision (or refresh) the user identified by the OIDC subject, returning the
 * row with its durable id. Called from the auth layer on sign-in; idempotent, so
 * later logins just sync email/name.
 */
export function bootstrapUser(
  subject: string,
  email: string,
  name: string,
): Promise<ActionResult<User>> {
  return call<User>("POST", "/users/bootstrap", { subject, email, name });
}

// --- API keys -------------------------------------------------------------
// Per-user bearer tokens, nested under their owner so the owner id never travels
// in a header. createApiKey is the only call that returns the plaintext token.

export function listApiKeys(userId: string): Promise<ActionResult<ApiKey[]>> {
  return call<ApiKey[]>("GET", `/users/${enc(userId)}/apikeys`);
}

export function createApiKey(
  userId: string,
  name: string,
  ttlSeconds: number,
): Promise<ActionResult<CreatedApiKey>> {
  return call<CreatedApiKey>("POST", `/users/${enc(userId)}/apikeys`, {
    name,
    ttlSeconds,
  });
}

export function deleteApiKey(
  userId: string,
  id: string,
): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/users/${enc(userId)}/apikeys/${enc(id)}`);
}

/** Resolve a presented bearer token to its owner, or an error result if it is
 * unknown, revoked, or expired. Used by the `/mcp` route to authenticate. */
export function verifyApiKey(
  token: string,
): Promise<ActionResult<VerifiedApiKey>> {
  return call<VerifiedApiKey>("POST", "/apikeys/verify", { token });
}
