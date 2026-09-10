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
import type { PlatformUser, RoleOption, UserInput } from "./iam";

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

/** Every user, oldest first — which puts whoever set the platform up at the top. */
export function listUsers(): Promise<ActionResult<PlatformUser[]>> {
  return managed<PlatformUser[]>("GET", "/users");
}

/**
 * Add somebody who has never signed in. The subject is the one their identity
 * provider will present, which an administrator reads from the provider's own
 * console — there is no way to discover it, and this platform admits only
 * provisioned users, so an account has to exist before its owner can sign in.
 */
export function createUser(
  subject: string,
  input: UserInput,
): Promise<ActionResult<PlatformUser>> {
  return managed<PlatformUser>("POST", "/users", { subject, ...input });
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
