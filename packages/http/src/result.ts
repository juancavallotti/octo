/**
 * A discriminated result type for operations that may fail with a message instead
 * of throwing — what Next.js server actions need, since they can't throw readable
 * errors across the boundary in production. Callers branch on `ok`.
 */
export type ActionResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string; status?: number };

/**
 * Per-request options every primitive in this module accepts.
 *
 * `headers` merges over whatever the primitive sets for itself (a Content-Type it
 * chose), so a caller can add an Authorization without having to know what else
 * is on the request.
 */
export interface RequestOptions {
  headers?: Record<string, string>;
  signal?: AbortSignal;
}
