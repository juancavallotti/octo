/**
 * The typed operations against iam.
 *
 * Two of them, and they are the same exchange from either end: one trades the
 * identity provider's token for a platform token, the other trades an expiring
 * platform token for a fresh one. Both answer with the same shape, so the
 * lifecycle in app/auth/octoToken.ts has one thing to parse.
 *
 * It builds its own URLs rather than going through `client/http.ts`, whose
 * `call()` is bound to ORCHESTRATOR_URL. Same layering, different service.
 */

import { requestJson, type ActionResult } from "@octo/http";
import { iamBaseUrl, iamUnconfigured } from "../_iam";

/** One grantable role, as iam's own catalogue describes it. */
export interface RoleOption {
  role: string;
  description: string;
}

/** The profile fields an administrator may set on a user. */
export interface UserInput {
  email: string;
  name: string;
}

/** A user as iam describes them. `roles` is always present, empty rather than null. */
export interface PlatformUser {
  id: string;
  email: string;
  name: string;
  roles: string[];
  createdAt: string;
  /** Null for somebody provisioned who has never signed in. */
  lastLoginAt: string | null;
}

/** One page of the directory, with the cursor for the next or none on the last. */
export interface UserPage {
  items: PlatformUser[];
  nextCursor?: string;
}

/** Which page of the directory to read, and which people to look for. */
export interface UserQuery {
  /** Substring of a name or an address; empty matches everybody. */
  q?: string;
  /** Narrow to the people holding this role; empty matches everybody. */
  role?: string;
  limit?: number;
  /** The `nextCursor` of the page before, or absent for the first. */
  cursor?: string;
}

/**
 * A minted platform token. The expiry is given explicitly so a caller can
 * schedule its re-mint without decoding the token it was handed.
 */
export interface PlatformToken {
  token: string;
  expiresAt: string;
  user: PlatformUser;
}

/**
 * Trade the identity provider's id token for a platform one. This is what
 * provisions the user row on a first sign-in, so it is the only call that has to
 * happen before anything else can.
 */
export function exchangeIdToken(idToken: string): Promise<ActionResult<PlatformToken>> {
  return post("/auth", idToken);
}

/**
 * Trade a platform token for a fresh one, picking up any role change since it was
 * minted. iam accepts a token that expired recently; past that window it answers
 * 401 and the session is over.
 */
export function refreshOctoToken(octoToken: string): Promise<ActionResult<PlatformToken>> {
  return post("/auth/refresh", octoToken);
}

/**
 * Both endpoints take the same shape: the credential as a bearer, no body at all.
 * Internal, like the orchestrator client's `call()` — the public API above names
 * what is happening, not how.
 */
function post(path: string, bearer: string): Promise<ActionResult<PlatformToken>> {
  const base = iamBaseUrl();
  if (!base) return Promise.resolve(iamUnconfigured<PlatformToken>("sign-in"));
  return requestJson<PlatformToken>("POST", `${base}${path}`, undefined, {
    headers: { Authorization: `Bearer ${bearer}` },
  });
}
