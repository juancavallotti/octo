/**
 * Browser-side client for cluster-wide secrets. Backed by server actions in
 * `app/actions/secrets.ts`; these wrappers unwrap the ActionResult so callers keep
 * a value-or-throw contract. Values are write-only: there is no read-value call
 * here by design — only listing names and setting/deleting.
 */

import * as secretActions from "@/app/actions/secrets";
import { unwrap } from "./bff";

/**
 * A cluster-wide secret, as the catalog exposes it. The value is write-only and
 * never returned — only the name and timestamps are.
 */
export interface ClusterSecret {
  name: string;
  /** RFC3339 timestamp of when the secret was first created. */
  createdAt: string;
  /** RFC3339 timestamp of the last time the value was set. */
  lastUpdated: string;
}

/** List the cluster secrets (names + timestamps; values are never returned). */
export async function listSecrets(): Promise<ClusterSecret[]> {
  return unwrap(await secretActions.listSecrets());
}

/** Create or overwrite a secret's value (write-only). */
export async function setSecret(
  name: string,
  value: string,
): Promise<ClusterSecret> {
  return unwrap(await secretActions.setSecret(name, value));
}

/**
 * Delete a secret. The orchestrator refuses (409) when a deployment still
 * references it; pass force to override.
 */
export async function deleteSecret(name: string, force = false): Promise<void> {
  return unwrap(await secretActions.deleteSecret(name, force));
}
