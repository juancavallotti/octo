import type { ActionResult } from "@octo/http";

export type { ActionResult } from "@octo/http";

/**
 * Turn a server action's {@link ActionResult} into a value or a thrown Error, so
 * the editor capability providers keep a value-or-throw contract. Server actions
 * can't throw readable errors across the boundary in production, so they return a
 * result and the provider unwraps it here.
 */
export function unwrap<T>(result: ActionResult<T>): T {
  if (!result.ok) throw new Error(result.error);
  return result.data;
}
