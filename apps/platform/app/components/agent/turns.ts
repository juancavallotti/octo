/**
 * What a conversation looks like once the frames have been folded together, and
 * the fold itself.
 *
 * Separate from the hook because it is the part with no React in it: given a turn
 * and a frame it returns the next turn, which is the whole of what the panel
 * renders and the easiest thing in this feature to test.
 *
 * A turn is an ordered log rather than a set of buckets. It used to hold one
 * reasoning string and one flat list of tools, which meant a run that thought,
 * called two tools, thought again and answered rendered as one block of reasoning
 * and one undifferentiated list — with the order, and so the story, gone. What the
 * agent did is a sequence, and this keeps it as one.
 */

import type { AgentEvent } from "./frames";

/** One tool the agent called, and how it went. */
export interface ToolRun {
  id: string;
  tool: string;
  done: boolean;
  failed: boolean;
  /** The arguments the model chose, and what came back. Shown on expand. */
  input?: unknown;
  output?: unknown;
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
 * `iter` is the agent's own turn counter, which rides on every frame. It is what
 * separates two rounds of tool calls with nothing in between: without it they read
 * as one long list, and the fact that the agent went back to the model — the
 * expensive part — is invisible.
 */
export type Segment =
  | { kind: "thinking"; iter: number; text: string }
  | { kind: "tools"; iter: number; runs: ToolRun[] }
  | { kind: "text"; iter: number; text: string }
  | { kind: "compaction"; iter: number; strategy: string; done: boolean; dropped?: number }
  | { kind: "signal"; iter: number; signal: string; text?: string };

/** One turn in the transcript. A user turn carries only text. */
export interface Turn {
  id: string;
  role: "user" | "agent";
  segments: Segment[];
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
    "He ran out of steps before finishing. Try narrowing the question, or raise AGENT_MAX_ITERATIONS on his deployment.",
};

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

/** Fold one frame into a turn. */
export function reduce(turn: Turn, event: AgentEvent): Turn {
  const iter = event.iteration ?? 0;

  switch (event.type) {
    case "text":
    case "thinking":
      return appendText(turn, event.type, iter, event.text);

    case "tool_call":
      return withSegments(turn, openTool(turn.segments, iter, event));

    case "tool_result":
      return withSegments(turn, closeTool(turn.segments, event));

    // The gauge is exact — what the provider read plus what it produced — and it
    // rides on every model turn, so it fills rather than only reporting an
    // overflow after the fact.
    case "turn_end":
      return event.contextMaxTokens
        ? { ...turn, context: { used: event.contextTokens ?? 0, max: event.contextMaxTokens } }
        : turn;

    // Bracketed rather than reported once, because a summarize is a real model
    // call and can take seconds. Without the open segment those seconds look like
    // a freeze.
    case "compaction_start":
      return push(turn, { kind: "compaction", iter, strategy: event.strategy ?? "", done: false });

    case "compaction_end":
      return withSegments(turn, finishCompaction(turn.segments, event.dropped));

    // Something that reached the run from outside it: a message handed over
    // mid-answer, or one it never got a turn to answer.
    case "signal":
      return push(turn, { kind: "signal", iter, signal: event.signal, text: event.text });

    // The final answer, which on a streaming run is the text already accumulated.
    // Taken only when nothing streamed, so a non-streaming agent still shows one.
    case "done":
      return answerOf(turn) || !event.text
        ? turn
        : push(turn, { kind: "text", iter, text: event.text });

    case "error":
      return { ...turn, note: event.error };

    // The reason is diagnostic and written for a log — "model refused",
    // "exceeded max iterations". The *reply* to the user comes from the
    // guardrail's own set-payload, and reaches the panel as the closing frame.
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
  return segments.map((segment) =>
    segment.kind === "tools" && segment.runs.some((r) => r.id === event.toolCallId)
      ? {
          ...segment,
          runs: segment.runs.map((r) =>
            r.id === event.toolCallId
              ? { ...r, done: true, failed: Boolean(event.isError), output: event.output }
              : r,
          ),
        }
      : segment,
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
