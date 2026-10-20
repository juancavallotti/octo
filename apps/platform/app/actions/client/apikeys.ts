/**
 * The typed operations for a person's own API keys, which iam holds. Nested under
 * their owner, and iam refuses any owner but the caller, so the id in the path is
 * only ever the session's own. createApiKey is the only call that returns the
 * plaintext token.
 */

import type { ActionResult } from "@octo/http";
import type { ApiKey, CreatedApiKey } from "@/app/model/apikeys";
import { managed } from "./iamAdmin";

export function listApiKeys(userId: string): Promise<ActionResult<ApiKey[]>> {
  return managed<ApiKey[]>("GET", keysPath(userId));
}

export function createApiKey(
  userId: string,
  name: string,
  ttlSeconds: number,
): Promise<ActionResult<CreatedApiKey>> {
  return managed<CreatedApiKey>("POST", keysPath(userId), { name, ttlSeconds });
}

export function deleteApiKey(
  userId: string,
  id: string,
): Promise<ActionResult<void>> {
  return managed<void>("DELETE", `${keysPath(userId)}/${encodeURIComponent(id)}`);
}

function keysPath(userId: string): string {
  return `/users/${encodeURIComponent(userId)}/apikeys`;
}
