/**
 * A tool call the agent is holding in front of a person, and what became of it.
 *
 * Split from the fold next door because it is a different question. That one is
 * about the ORDER of what the agent did; this is about one call being suspended
 * mid-sequence while somebody decides, which is the only thing in a run that
 * waits on a human.
 *
 * It lives on the tool run rather than as a segment of its own, deliberately: the
 * call is already on screen when the question arrives — the agent reports a call
 * before it asks about it — so the chip that shows the arguments is the right
 * place to ask about them.
 */

import type { AgentEvent } from "./frames";
import type { Segment, ToolRun } from "./turns";

/**
 * A tool call waiting on a person, once the agent has asked.
 *
 * `pending` is the only state with a question in it. The other two are what the
 * chip shows afterwards, because a call somebody allowed and a call that ran
 * freely are not the same thing to read back later.
 */
export interface Authorization {
  id: string;
  state: "pending" | "allowed" | "denied";
  /** How long the run said it would wait. */
  expiresInSeconds?: number;
}

/** Attach a pending authorization to the call it is about. */
export function holdTool(
  segments: Segment[],
  event: Extract<AgentEvent, { type: "tool_authorization" }>,
): Segment[] {
  return mapRun(segments, (r) => r.id === event.toolCallId, (r) => ({
    ...r,
    authorization: {
      id: event.authorizationId,
      state: "pending",
      expiresInSeconds: event.expiresInSeconds,
    },
  }));
}

/**
 * Record what was decided about a call.
 *
 * Matched on the authorization id rather than the tool call id because that is
 * what the answer quotes, and because a person may have answered from another
 * tab: the decision is about the call, not about who clicked.
 */
export function settleAuthorization(
  segments: Segment[],
  authorizationId: string | undefined,
  allowed: boolean | undefined,
): Segment[] {
  if (!authorizationId) return segments;
  return mapRun(
    segments,
    (r) => r.authorization?.id === authorizationId,
    (r) => ({
      ...r,
      authorization: { ...r.authorization!, state: allowed ? "allowed" : "denied" },
    }),
  );
}

/** Apply a change to whichever tool run matches, wherever its segment is. */
export function mapRun(
  segments: Segment[],
  match: (run: ToolRun) => boolean,
  change: (run: ToolRun) => ToolRun,
): Segment[] {
  return segments.map((segment) =>
    segment.kind === "tools" && segment.runs.some(match)
      ? { ...segment, runs: segment.runs.map((r) => (match(r) ? change(r) : r)) }
      : segment,
  );
}
