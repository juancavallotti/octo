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
import type { ActionResult } from "@octo/http";

/** Map an authorization failure to an error result; null means "authorized". */
async function gate(
  roles: string[],
): Promise<{ ok: false; error: string } | null> {
  try {
    await requireRole(...roles);
    return null;
  } catch (err) {
    if (err instanceof ForbiddenError) return { ok: false, error: "forbidden" };
    if (err instanceof AuthError) {
      return { ok: false, error: "unauthenticated" };
    }
    throw err;
  }
}

/** Run `fn` for any authenticated caller (session only). */
export async function withRead<T>(
  fn: () => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  return (await gate([])) ?? fn();
}

/** Run `fn` only for a caller holding the write roles. */
export async function withWrite<T>(
  fn: () => Promise<ActionResult<T>>,
): Promise<ActionResult<T>> {
  return (await gate(writeRoles)) ?? fn();
}
