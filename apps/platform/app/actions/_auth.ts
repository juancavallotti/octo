/**
 * Authorization gates for the server actions. The action is the trust boundary:
 * it authorizes, then delegates to the (auth-agnostic) orchestrator client lib.
 * Reads require a session; writes require the write roles — the same split the
 * route handlers' `withAuth` applied. A denied check short-circuits to an error
 * result with the wording the routes returned.
 */

import {
  AuthError,
  ForbiddenError,
  requireRole,
  writeRoles,
} from "@/app/auth/guard";
import type { Session } from "next-auth";
import type { ActionResult } from "@octo/http";

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
