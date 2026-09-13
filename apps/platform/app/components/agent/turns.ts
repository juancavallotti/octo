/**
 * What a conversation looks like once the frames have been folded together, and
 * the fold itself: given a turn and a frame, the next turn.
 *
 * A turn is an ordered log rather than a set of buckets — what the agent did is a
 * sequence, and this keeps it as one.
 */

import type { AgentEvent } from "./frames";
import { holdTool, mapRun, settleAuthorization, type Authorization } from "./authorization";

export type { Authorization } from "./authorization";

/** One tool the agent called, and how it went. */
export interface ToolRun {
  id: string;
  tool: string;
  done: boolean;
  failed: boolean;
  /** The arguments the model chose, and what came back. Shown on expand. */
  input?: unknown;
  output?: unknown;
  /** Set when this call needed a person before it could run. */
  authorization?: Authorization;
}

/** How full the conversation's context is, and how full it may get. */
export interface ContextGauge {
  used: number;
  max: number;
}

/**
 * One stretch of one kind of work. Segments are appended and never reordered, so
 * their position in the array is their position in time.
 *
 * `iter` is the agent's own turn counter, riding on every frame. It is what
 * separates two rounds of tool calls with nothing in between, which would
 * otherwise read as one long list.
 */
export type Segment =
  | { kind: "thinking"; iter: number; text: string }
  | { kind: "tools"; iter: number; runs: ToolRun[] }
  | { kind: "text"; iter: number; text: string }
  | { kind: "compaction"; iter: number; strategy: string; done: boolean; dropped?: number }
  | { kind: "signal"; iter: number; signal: string; text?: string };

/**
 * What became of a message sent while he was already working.
 *
 * Such a message is not answered by the request that carried it: it is folded in
 * at the top of the run's next iteration, which can be a whole model call away. So
 * it counts as sent-but-not-yet-read until the run says otherwise, which it does
 * by emitting a `signal` when it injects one.
 */
export type Delivery = "pending" | "taken" | "missed";

/** One turn in the transcript. A user turn carries only text. */
export interface Turn {
  id: string;
  role: "user" | "agent";
  segments: Segment[];
  /**
   * Set only on a message handed to a run in progress. An ordinary question —
   * one that started its own run — has no delivery to report: the answer
   * streaming underneath it is the acknowledgement.
   */
  delivery?: Delivery;
  /** Set when the agent declined the question or the run failed. */
  note?: string;
  /** How full the context was at the last model turn, when one reported it. */
  context?: ContextGauge;
  streaming: boolean;
}

/** The runtime's guardrail reasons, said in a way a reader can act on. */
const GUARDRAIL_NOTES: Record<string, string> = {
  "model refused": "He declined this one.",
  "exceeded max iterations":
    "He ran out of steps before finishing. Try narrowing the question, or raise the turn limit under Admin, Platform agent.",
};

/** Why an agent turn carries no answer: the conversation was already claimed. */
export const HANDED_OVER_NOTE =
  "He was already working on this conversation, so your message joined that run " +
  "rather than starting a new one. The answer is going to whoever is reading it.";

/** A fresh turn, for the hook and for replaying a stored conversation. */
export function newTurn(id: string, role: Turn["role"], text = ""): Turn {
  return {
    id,
    role,
    segments: text ? [{ kind: "text", iter: 0, text }] : [],
    streaming: false,
  };
}

/** The answer so far — every text segment, in order. Empty until one arrives. */
export function answerOf(turn: Turn): string {
  return turn.segments
    .filter((s): s is Extract<Segment, { kind: "text" }> => s.kind === "text")
    .map((s) => s.text)
    .join("");
}

/**
 * Fold a message the run has just read into the transcript, where it happened:
 * after everything the run had done when it arrived, and before everything it
 * does because of it. The open agent turn is closed and a new one opened
 * underneath, so what answers this message sits under it.
 *
 * Matched on the text, which is all the two ends share — the message comes back
 * carrying no id of ours. The oldest match wins, which is the order they are
 * injected in. A message that matches nothing is written in rather than dropped:
 * it really did shape what follows.
 *
 * @param currentId the agent turn the run is writing; it is closed
 * @param openedId  the agent turn the run continues in
 */
export function takeIn(turns: Turn[], currentId: string, openedId: string, text: string): Turn[] {
  const said = text.trim();
  const at = turns.findIndex((turn) => turn.id === currentId);
  // With no turn to split at, the message goes at the end and nothing is reordered.
  const head = at < 0 ? turns : turns.slice(0, at);
  const tail = at < 0 ? [] : turns.slice(at + 1);
  const closing = at < 0 ? undefined : turns[at];

  const waiting = tail.findIndex(
    (turn) => turn.role === "user" && turn.delivery === "pending" && answerOf(turn).trim() === said,
  );
  const read: Turn =
    waiting >= 0
      ? { ...tail[waiting], delivery: "taken" }
      : { ...newTurn(`${openedId}:said`, "user", said), delivery: "taken" };

  return [
    ...head,
    // An agent turn with nothing in it is dropped rather than closed: two messages
    // read in the same iteration would otherwise leave an empty turn between them.
    ...(closing && hasContent(closing) ? [{ ...closing, streaming: false }] : []),
    read,
    // The gauge rides across: it is a property of the conversation, not the turn.
    { ...newTurn(openedId, "agent"), streaming: true, context: closing?.context },
    ...tail.filter((_, i) => i !== waiting),
  ];
}

/** Whether a turn has anything to show. */
function hasContent(turn: Turn): boolean {
  return turn.segments.length > 0 || Boolean(turn.note);
}

/**
 * Mark a message the run took responsibility for and never answered. Null when
 * nothing pending matches the text, which leaves the caller nothing to place.
 */
export function acknowledge(turns: Turn[], signal: string, text: string): Turn[] | null {
  const said = text.trim();
  if (signal !== "unanswered" || !said) return null;
  const i = turns.findIndex(
    (turn) => turn.role === "user" && turn.delivery === "pending" && answerOf(turn).trim() === said,
  );
  if (i < 0) return null;
  return turns.with(i, { ...turns[i], delivery: "missed" });
}

/**
 * Settle whatever is still waiting once the run has ended. An acknowledgement
 * only arrives on the stream the run owns, so anything still pending when it
 * closes was never taken.
 */
export function settle(turns: Turn[]): Turn[] {
  return turns.map((turn) => (turn.delivery === "pending" ? { ...turn, delivery: "missed" } : turn));
}

/** Fold one frame into a turn. */
export function reduce(turn: Turn, event: AgentEvent): Turn {
  const iter = event.iteration ?? 0;

  switch (event.type) {
    case "text":
    case "thinking":
      return appendText(turn, event.type, iter, event.text);

    case "tool_call":
      return withSegments(turn, openTool(turn.segments, iter, event));

    // The question, attached to the call it is about — which is already on screen,
    // since the agent reports the call before it asks about it.
    case "tool_authorization":
      return withSegments(turn, holdTool(turn.segments, event));

    case "tool_result":
      return withSegments(turn, closeTool(turn.segments, event));

    // The gauge is exact: what the provider read plus what it produced.
    case "turn_end":
      return event.contextMaxTokens
        ? { ...turn, context: { used: event.contextTokens ?? 0, max: event.contextMaxTokens } }
        : turn;

    // Bracketed rather than reported once: a summarize is a real model call and
    // can take seconds, which without an open segment look like a freeze.
    case "compaction_start":
      return push(turn, { kind: "compaction", iter, strategy: event.strategy ?? "", done: false });

    case "compaction_end":
      return withSegments(turn, finishCompaction(turn.segments, event.dropped));

    // Something that reached the run from outside it. An answer to a gated call
    // belongs on the call that asked rather than in the log.
    case "signal":
      return event.signal === "authorize"
        ? withSegments(turn, settleAuthorization(turn.segments, event.authorizationId, event.allowed))
        : push(turn, { kind: "signal", iter, signal: event.signal, text: event.text });

    // The final answer, which on a streaming run is the text already accumulated.
    // Taken only when nothing streamed, so a non-streaming agent still shows one.
    case "done":
      return answerOf(turn) || !event.text
        ? turn
        : push(turn, { kind: "text", iter, text: event.text });

    // Naming the conversation says nothing about the turn it arrived on.
    case "thread_title":
      return turn;

    case "error":
      return { ...turn, note: event.error };

    // The reason is diagnostic — "model refused", "exceeded max iterations" —
    // so it is translated rather than shown.
    case "guardrail":
      return { ...turn, note: GUARDRAIL_NOTES[event.reason ?? ""] ?? "He stopped short of an answer." };
  }
}

/** Append to the segment in progress, or start one when the work has changed. */
function appendText(turn: Turn, kind: "text" | "thinking", iter: number, text: string): Turn {
  const last = turn.segments.at(-1);
  if (last && last.kind === kind && last.iter === iter) {
    return withSegments(turn, [
      ...turn.segments.slice(0, -1),
      { ...last, text: last.text + text },
    ]);
  }
  return push(turn, { kind, iter, text });
}

function openTool(
  segments: Segment[],
  iter: number,
  event: Extract<AgentEvent, { type: "tool_call" }>,
): Segment[] {
  const run: ToolRun = {
    id: event.toolCallId,
    tool: event.tool,
    done: false,
    failed: false,
    input: event.input,
  };
  const last = segments.at(-1);
  if (last && last.kind === "tools" && last.iter === iter) {
    return [...segments.slice(0, -1), { ...last, runs: [...last.runs, run] }];
  }
  return [...segments, { kind: "tools", iter, runs: [run] }];
}

/**
 * Close a tool wherever it is. Searched across every segment rather than only the
 * last: a result arrives after the branch that ran it returned, and the agent may
 * have opened another round of calls in the meantime.
 */
function closeTool(
  segments: Segment[],
  event: Extract<AgentEvent, { type: "tool_result" }>,
): Segment[] {
  return mapRun(
    segments,
    (r) => r.id === event.toolCallId,
    (r) => ({
      ...r,
      done: true,
      failed: Boolean(event.isError),
      output: event.output,
      // A result ends the question whatever happened; still pending here means it
      // was never allowed.
      authorization:
        r.authorization?.state === "pending"
          ? { ...r.authorization, state: "denied" as const }
          : r.authorization,
    }),
  );
}

/** Close the compaction still in progress, if the panel saw it start. */
function finishCompaction(segments: Segment[], dropped?: number): Segment[] {
  const open = segments.findLastIndex((s) => s.kind === "compaction" && !s.done);
  if (open < 0) return segments;
  const segment = segments[open] as Extract<Segment, { kind: "compaction" }>;
  return segments.with(open, { ...segment, done: true, dropped });
}

const push = (turn: Turn, segment: Segment): Turn =>
  withSegments(turn, [...turn.segments, segment]);

const withSegments = (turn: Turn, segments: Segment[]): Turn => ({ ...turn, segments });
