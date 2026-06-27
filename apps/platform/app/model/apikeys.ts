/**
 * Browser-side client for per-user API keys. Backed by server actions in
 * `app/actions/apikeys.ts`; these wrappers unwrap the ActionResult so callers keep
 * a value-or-throw contract. The secret token is returned only once, by
 * createApiKey — list never exposes it, and it is unrecoverable thereafter.
 */

import * as apikeyActions from "@/app/actions/apikeys";
import { unwrap } from "./bff";

/** A per-user API key's non-secret metadata. */
export interface ApiKey {
  id: string;
  name: string;
  /** Recognizable token prefix (e.g. "octo_ab12"), for identifying a key. */
  prefix: string;
  /** Last 4 characters of the token, for identifying a key. */
  last4: string;
  /** RFC3339 timestamp of creation. */
  createdAt: string;
  /** RFC3339 timestamp after which the key no longer authenticates. */
  expiresAt: string;
  /** RFC3339 timestamp of the last successful use, or null if never used. */
  lastUsedAt?: string | null;
}

/** A freshly created key, carrying the one-time plaintext token. */
export interface CreatedApiKey extends ApiKey {
  /** The full secret token. Shown once at creation and never retrievable again. */
  token: string;
}

/**
 * The owner a verified bearer token resolves to. Produced by the orchestrator's
 * verify endpoint and consumed server-side by the `/mcp` bearer gate; not a
 * browser-facing shape.
 */
export interface VerifiedApiKey {
  id: string;
  userId: string;
  name: string;
}

/** List the caller's active API keys (no secret material). */
export async function listApiKeys(): Promise<ApiKey[]> {
  return unwrap(await apikeyActions.listApiKeys());
}

/** Create a key that expires after ttlSeconds; returns the one-time token. */
export async function createApiKey(
  name: string,
  ttlSeconds: number,
): Promise<CreatedApiKey> {
  return unwrap(await apikeyActions.createApiKey(name, ttlSeconds));
}

/** Revoke one of the caller's keys. */
export async function deleteApiKey(id: string): Promise<void> {
  return unwrap(await apikeyActions.deleteApiKey(id));
}
