/**
 * Bridges the browser-side model to the server actions: the model calls an action
 * (which returns a discriminated {@link ActionResult}) and unwraps it here back
 * into value-or-throw, so existing callers keep their try/catch.
 */

import type { ActionResult } from "@/app/actions/_client";

/**
 * Turn a server action's {@link ActionResult} into a value or a thrown Error.
 * Server actions can't throw readable errors across the boundary in production,
 * so they return a result and the model unwraps it here.
 */
export function unwrap<T>(result: ActionResult<T>): T {
  if (!result.ok) throw new Error(result.error);
  return result.data;
}
