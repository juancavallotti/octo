/**
 * The typed operations for administering people, as opposed to authenticating
 * them.
 *
 * Apart from `iam.ts` because the two need different things. The exchange runs
 * on the sign-in path and in the MCP endpoint, and has to stay free of anything
 * that assumes a session — these calls are the opposite: every one of them
 * presents the caller's own token, read from the session cookie.
 */

import { requestJson, type ActionResult } from "@octo/http";
import { currentOctoToken } from "@/app/auth/sessionCookie";
import { iamBaseUrl, iamUnconfigured } from "../_iam";
import type {
  PlatformUser,
  RoleOption,
  UserInput,
  UserPage,
  UserQuery,
} from "./iam";

//
// Everything below is behind iam's own administrator check, and every call
// presents the caller's platform token so that iam can make it. The token is
// read from the session cookie here rather than passed down from the action,
// which keeps it out of the signature of every operation — and out of anything
// that might one day log one.

/** The roles that can be granted, from iam's catalogue rather than from usage. */
export function listRoles(): Promise<ActionResult<RoleOption[]>> {
  return managed<RoleOption[]>("GET", "/roles");
}

/**
 * One page of the directory, oldest first — which puts whoever set the platform
 * up at the top. Filtering and paging are iam's, not this layer's: a filter
 * applied after paging would return short pages of an unknown total.
 */
export function listUsers(query: UserQuery = {}): Promise<ActionResult<UserPage>> {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.role) params.set("role", query.role);
  if (query.limit != null) params.set("limit", String(query.limit));
  if (query.cursor) params.set("cursor", query.cursor);
  const qs = params.toString();
  return managed<UserPage>("GET", qs ? `/users?${qs}` : "/users");
}

/**
 * Add somebody who has never signed in, by address.
 *
 * That is all it takes, because an address is all an administrator knows about a
 * colleague who has never been here. The OIDC subject is written by that
 * person's first sign-in, which claims this row.
 */
export function createUser(input: UserInput): Promise<ActionResult<PlatformUser>> {
  return managed<PlatformUser>("POST", "/users", input);
}

/** Correct a user's profile. The subject is not among the fields: it keys the row. */
export function updateUser(
  id: string,
  input: UserInput,
): Promise<ActionResult<PlatformUser>> {
  return managed<PlatformUser>("PUT", `/users/${encodeURIComponent(id)}`, input);
}

/** Remove a user. iam refuses to remove the last administrator. */
export function deleteUser(id: string): Promise<ActionResult<void>> {
  return managed<void>("DELETE", `/users/${encodeURIComponent(id)}`);
}

/** Give a user a role, and answer with the whole set they now hold. */
export function grantRole(id: string, role: string): Promise<ActionResult<PlatformUser>> {
  return roleCall("PUT", id, role);
}

/** Take a role away, and answer with the whole set they now hold. */
export function revokeRole(id: string, role: string): Promise<ActionResult<PlatformUser>> {
  return roleCall("DELETE", id, role);
}

/** Grants are addressed through the user they belong to, never on their own. */
function roleCall(
  method: string,
  id: string,
  role: string,
): Promise<ActionResult<PlatformUser>> {
  return managed<PlatformUser>(
    method,
    `/users/${encodeURIComponent(id)}/roles/${encodeURIComponent(role)}`,
  );
}

/** One administered request, carrying the caller's own token. */
async function managed<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ActionResult<T>> {
  const base = iamBaseUrl();
  if (!base) return iamUnconfigured<T>("user administration");

  const token = await currentOctoToken();
  if (!token) {
    // The same wording iam would answer with, decided here so a caller with no
    // usable session is told the truth rather than shown a failed request.
    return { ok: false, error: "not authorized", status: 401 };
  }
  return requestJson<T>(method, `${base}${path}`, body, {
    headers: { Authorization: `Bearer ${token}` },
  });
}
