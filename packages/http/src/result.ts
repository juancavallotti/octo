/**
 * A discriminated result type for operations that may fail with a message instead
 * of throwing — what Next.js server actions need, since they can't throw readable
 * errors across the boundary in production. Callers branch on `ok`.
 */
export type ActionResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string };
