/**
 * Browser-side client for the deployment-scoped object store — the user-facing
 * "user" KV namespace that the `object-read`/`object-write`/`object-delete` blocks
 * persist to. Backed by server actions in `app/actions/objects.ts`; these wrappers
 * unwrap the ActionResult so callers keep a value-or-throw contract.
 *
 * Values round-trip with an explicit encoding ("utf8" for readable text, "base64"
 * for binary) so a value survives JSON either way. Writes and deletes use the
 * stored version for optimistic concurrency (0 creates / deletes unconditionally).
 */

import * as objectActions from "@/app/actions/objects";
import { unwrap } from "./bff";

/** Metadata for one stored object, as the listing returns it (no value). */
export interface ObjectEntry {
  key: string;
  version: number;
  /** Stored value size in bytes. */
  size: number;
  /** RFC3339 timestamp of the last write. */
  updatedAt: string;
}

/** A single object's value plus the version to pass back on a conditional write. */
export interface ObjectValue {
  key: string;
  value: string;
  /** How `value` is encoded: "utf8" for text, "base64" for binary. */
  encoding: "utf8" | "base64";
  version: number;
}

/**
 * List the non-secret namespaces a deployment holds data in (the user namespace is
 * always present).
 */
export async function listNamespaces(deploymentId: string): Promise<string[]> {
  return unwrap(await objectActions.listNamespaces(deploymentId));
}

/**
 * List the objects a deployment holds in a namespace (keys + metadata). The
 * namespace defaults to the user-facing one server-side when omitted.
 */
export async function listObjects(
  deploymentId: string,
  namespace?: string,
): Promise<ObjectEntry[]> {
  return unwrap(await objectActions.listObjects(deploymentId, namespace));
}

/** Read a single object's value (and the version to write back). */
export async function getObject(
  deploymentId: string,
  key: string,
  namespace?: string,
): Promise<ObjectValue> {
  return unwrap(await objectActions.getObject(deploymentId, key, namespace));
}

/**
 * Create or overwrite an object. Pass the current version (0 to create); a stale
 * version is rejected by the orchestrator (409). Returns the new version.
 */
export async function setObject(
  deploymentId: string,
  key: string,
  value: string,
  version: number,
  encoding: "utf8" | "base64" = "utf8",
  namespace?: string,
): Promise<number> {
  return unwrap(
    await objectActions.setObject(
      deploymentId,
      key,
      value,
      version,
      encoding,
      namespace,
    ),
  );
}

/** Delete an object. Version 0 deletes unconditionally; a positive value must match. */
export async function deleteObject(
  deploymentId: string,
  key: string,
  version = 0,
  namespace?: string,
): Promise<void> {
  return unwrap(
    await objectActions.deleteObject(deploymentId, key, version, namespace),
  );
}
