/**
 * The message a `that:` expression would be evaluated against, pulled out of whatever the
 * last run reported.
 *
 * dolphin evaluates `that:` over a runtime message through the same CEL seam every block
 * uses, so `body` and `vars` are the only two things bound — see `matchThat` in
 * runtime/dolphin/internal/assert/match.go. A report carries the whole message
 * (`{event_id, variables, body}`), so this is where the two names are lined up.
 */

export interface CelSample {
  /** Bound to `body`. */
  body?: unknown;
  /** Bound to `vars`. Called `variables` on the wire. */
  vars?: Record<string, unknown>;
}

/** Read a message's body and variables, or null when there is no message to read. */
export function sampleOf(message: unknown): CelSample | null {
  if (typeof message !== "object" || message === null) return null;
  const m = message as { body?: unknown; variables?: unknown };
  const sample: CelSample = {};
  if (m.body !== undefined) sample.body = m.body;
  if (typeof m.variables === "object" && m.variables !== null && !Array.isArray(m.variables)) {
    sample.vars = m.variables as Record<string, unknown>;
  }
  return Object.keys(sample).length ? sample : null;
}
