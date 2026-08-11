/**
 * Authorization gates for the server actions. The action is the trust boundary:
 * it authorizes, then delegates to the (auth-agnostic) orchestrator client lib.
 * Reads require a session; writes require the write roles — the same split the
 * route handlers' `withAuth` applied. A denied check short-circuits to an error
 * result with the wording the routes returned.
 *
 * Two of the four gates also resolve the caller's durable orchestrator user id, for
 * operations the orchestrator scopes to a user rather than to the cluster: their API
 * keys, and the dev runs they have running. That id is always resolved here, from the
 * session — never taken from client input, which is what keeps one user from addressing
 * another's.
 */

import {
  AuthError,
  ForbiddenError,
  requireRole,
  requireSession,
  writeRoles,
} from "@/app/auth/guard";
import { authEnabled } from "@/auth";
import type { Session } from "next-auth";
import type { ActionResult } from "@octo/http";
import * as client from "./_client";

/**
 * Authorize `roles`, returning the authenticated session on success or an error
 * result on failure. The session is handed to the action so it can attribute
 * writes to the acting user without a second `auth()` round-trip.
 */
async function gate(
  roles: string[],
): Promise<{ session: Session } | { ok: false; error: string }> {
  try {
    return { session: await requireRole(...roles) };
  } catch (err) {
    if (err instanceof ForbiddenError) return { ok: false, error: "forbidden" };
    if (err instanceof AuthError) {
      return { ok: false, error: "unauthenticated" };
    }
    throw err;
  }
}

/**
 * Run `fn` for any authenticated caller (session only). `fn` receives the
 * session; callers that don't need it can ignore the argument.
 */
export async function withRead<T>(
  fn: (session: Session) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  const g = await gate([]);
  return "session" in g ? fn(g.session) : g;
}

/** Run `fn` only for a caller holding the write roles. */
export async function withWrite<T>(
  fn: (session: Session) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  const g = await gate(writeRoles);
  return "session" in g ? fn(g.session) : g;
}

// Stable identity for the local (no-SSO) dev session, which has no OIDC subject.
// Bootstrapping it on demand gives `task dev` a real user row to own keys and dev runs.
const LOCAL_SUBJECT = "local-dev";
const LOCAL_EMAIL = "local@localhost";
const LOCAL_NAME = "Local Dev";

/**
 * The caller's durable orchestrator user id. With SSO it is on the session, put there
 * when the user was bootstrapped at sign-in. In local dev there is no IdP, so a stable
 * sentinel user is bootstrapped on demand and its id used — which is why this can reach
 * the orchestrator at all.
 *
 * Throws AuthError when no user can be resolved, which the gates below turn into an
 * error result so an action never throws across the boundary.
 */
async function userIdOf(session: Session): Promise<string> {
  if (!authEnabled) {
    const res = await client.bootstrapUser(LOCAL_SUBJECT, LOCAL_EMAIL, LOCAL_NAME);
    if (!res.ok) throw new AuthError(res.error);
    return res.data.id;
  }
  const id = session.user.id;
  if (!id) throw new AuthError("user not provisioned");
  return id;
}

/** Authorize `roles`, resolve the caller's user id, and run `fn` scoped to it. */
async function gateUser<T>(
  roles: string[],
  fn: (userId: string, session: Session) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  const g = await gate(roles);
  if (!("session" in g)) return g;
  let userId: string;
  try {
    userId = await userIdOf(g.session);
  } catch (err) {
    if (err instanceof AuthError) return { ok: false, error: err.message };
    throw err;
  }
  return fn(userId, g.session);
}

/**
 * Run `fn` for any authenticated caller, scoped to their own user id — the gate for
 * "each caller manages their own", which is what a user's API keys and dev runs are.
 * No write-role check: those roles guard cluster-wide operations, and these are not.
 */
export function withUser<T>(
  fn: (userId: string, session: Session) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  return gateUser([], fn);
}

/** {@link withUser}, for an operation that also spends cluster resources. */
export function withWriteUser<T>(
  fn: (userId: string, session: Session) => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  return gateUser(writeRoles, fn);
}

/**
 * The caller's user id for a *route handler* rather than an action — a streaming endpoint,
 * which a server action cannot back. It reads the session itself, since nothing handed it
 * one, and throws AuthError for the route to turn into a 401.
 */
export async function currentUserId(): Promise<string> {
  return userIdOf(await requireSession());
}

/**
 * {@link currentUserId}, for a route handler whose caller can cause cluster-wide
 * writes. Throws ForbiddenError when the session lacks the write roles, for the
 * route to turn into a 403.
 *
 * The agent's chat route is the caller, and it needs this rather than
 * {@link currentUserId} because of what sits behind it: the agent holds full
 * read-write access to the orchestrator API, so admitting any signed-in user there
 * would route around the very gate every other write goes through.
 */
export async function currentWriteUserId(): Promise<{ id: string; name: string }> {
  const session = await requireRole(...writeRoles);
  return { id: await userIdOf(session), name: session.user?.name ?? "" };
}
