/**
 * Browser-side client for cluster-wide secrets, talking to the editor's BFF proxy
 * routes under `/api/secrets`. Shares the request/envelope helpers with the rest of
 * the orchestrator client. Values are write-only: there is no read-value call here
 * by design — only listing names and setting/deleting.
 */

import { jsonBody, request } from "./bff";

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
export function listSecrets(): Promise<ClusterSecret[]> {
  return request<ClusterSecret[]>("/api/secrets");
}

/** Create or overwrite a secret's value (write-only). */
export function setSecret(name: string, value: string): Promise<ClusterSecret> {
  return request<ClusterSecret>(`/api/secrets/${encodeURIComponent(name)}`, {
    ...jsonBody({ value }),
    method: "PUT",
  });
}

/**
 * Delete a secret. The orchestrator refuses (409) when a deployment still
 * references it; pass force to override.
 */
export function deleteSecret(name: string, force = false): Promise<void> {
  return request<void>(
    `/api/secrets/${encodeURIComponent(name)}${force ? "?force=true" : ""}`,
    { method: "DELETE" },
  );
}
