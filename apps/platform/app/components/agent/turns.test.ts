/**
 * The fold, which is where the order lives. Every case here is about a sequence
 * rather than a value: what the panel can show is exactly what this preserves.
 */

import { describe, expect, it } from "vitest";

import { acknowledge, answerOf, newTurn, reduce, settle, type Segment, type Turn } from "./turns";
import type { AgentEvent } from "./frames";

/** Fold a run's worth of frames into one turn. */
function run(...events: AgentEvent[]): Turn {
  return events.reduce(reduce, newTurn("t1", "agent"));
}

/** The shape of a turn, as a reader would describe it. */
function shape(turn: Turn): string[] {
  return turn.segments.map((s) => s.kind);
}

const text = (t: string, iteration = 1): AgentEvent => ({ type: "text", iteration, text: t });
const thinking = (t: string, iteration = 1): AgentEvent => ({ type: "thinking", iteration, text: t });
const call = (id: string, iteration = 1): AgentEvent => ({
  type: "tool_call",
  iteration,
  tool: "octo_api",
  toolCallId: id,
});
const result = (id: string, isError = false, iteration = 1): AgentEvent => ({
  type: "tool_result",
  iteration,
  tool: "octo_api",
  toolCallId: id,
  isError,
});

describe("reduce", () => {
  it("joins tokens of the same kind into one segment", () => {
    const turn = run(text("You have "), text("three."));

    expect(shape(turn)).toEqual(["text"]);
    expect(answerOf(turn)).toBe("You have three.");
  });

  it("starts a new segment when the kind of work changes", () => {
    const turn = run(thinking("let me look"), call("c1"), thinking("now I know"), text("Three."));

    expect(shape(turn)).toEqual(["thinking", "tools", "thinking", "text"]);
  });

  // Two rounds of tool calls with nothing said between them are two decisions:
  // the second was made because of what the first returned. Without the turn
  // counter they collapse into one list and the round trip disappears.
  it("keeps two rounds of tool calls apart", () => {
    const turn = run(call("c1", 1), result("c1", false, 1), call("c2", 2), result("c2", false, 2));

    expect(shape(turn)).toEqual(["tools", "tools"]);
  });

  it("groups calls made in the same round", () => {
    const turn = run(call("c1"), call("c2"));

    expect(shape(turn)).toEqual(["tools"]);
    expect((turn.segments[0] as Extract<Segment, { kind: "tools" }>).runs).toHaveLength(2);
  });

  // A result arrives after the branch that ran it returned, by which time the
  // agent may have opened another round — so the chip to close is not always in
  // the last segment.
  it("closes a tool in an earlier round", () => {
    const turn = run(call("c1", 1), call("c2", 2), result("c1", true, 2));

    const first = turn.segments[0] as Extract<Segment, { kind: "tools" }>;
    expect(first.runs[0]).toMatchObject({ id: "c1", done: true, failed: true });
    const second = turn.segments[1] as Extract<Segment, { kind: "tools" }>;
    expect(second.runs[0]).toMatchObject({ id: "c2", done: false });
  });

  it("takes the gauge from a finished model turn", () => {
    const turn = run({ type: "turn_end", iteration: 1, contextTokens: 1200, contextMaxTokens: 16000 });

    expect(turn.context).toEqual({ used: 1200, max: 16000 });
  });

  it("brackets a compaction and closes it with what it dropped", () => {
    const turn = run(
      { type: "compaction_start", iteration: 2, strategy: "summarize" },
      { type: "compaction_end", iteration: 2, dropped: 12 },
    );

    expect(turn.segments).toEqual([
      { kind: "compaction", iter: 2, strategy: "summarize", done: true, dropped: 12 },
    ]);
  });

  // A stream can be joined late, or a kind filtered out of `emit`. An end with no
  // start must not invent one, or the panel reports a compaction that it never saw
  // begin and cannot say anything about.
  it("ignores the end of a compaction it never saw start", () => {
    const turn = run({ type: "compaction_end", iteration: 2, dropped: 12 });

    expect(turn.segments).toEqual([]);
  });

  it("records a message handed to the run mid-answer", () => {
    const turn = run(text("looking"), {
      type: "signal",
      iteration: 2,
      signal: "context",
      text: "actually, focus on pricing",
    });

    expect(shape(turn)).toEqual(["text", "signal"]);
  });

  // The closing frame repeats what streamed, so it is taken only when nothing
  // did — which is the guardrail path, the one case where it is the whole answer.
  it("takes the final answer only when nothing streamed", () => {
    expect(answerOf(run({ type: "done", text: "the guardrail's reply" }))).toBe(
      "the guardrail's reply",
    );
    expect(answerOf(run(text("streamed."), { type: "done", text: "the whole answer" }))).toBe(
      "streamed.",
    );
  });

  it("turns a guardrail reason into something a reader can act on", () => {
    expect(run({ type: "guardrail", reason: "exceeded max iterations" }).note).toContain(
      "ran out of steps",
    );
    // A reason this build has never heard of still gets a sentence.
    expect(run({ type: "guardrail", reason: "who knows" }).note).toBeTruthy();
  });

  it("leaves the turn untouched by a gauge with no budget behind it", () => {
    const turn = run({ type: "turn_end", iteration: 1, contextTokens: 1200 });
    expect(turn.context).toBeUndefined();
  });
});

/**
 * A message sent while he was working. The runtime answers the request that
 * carried it with nothing, so this state — and the frame that clears it — is the
 * only thing standing between the reader and a bubble they cannot tell was read.
 */
describe("acknowledge", () => {
  const waiting = (id: string, said: string): Turn => ({
    ...newTurn(id, "user", said),
    delivery: "pending",
  });

  it("marks the message the run says it folded in", () => {
    const turns = acknowledge([waiting("u1", "and the logs?")], "context", "and the logs?");

    expect(turns?.[0].delivery).toBe("taken");
  });

  it("marks one he accepted and never reached", () => {
    const turns = acknowledge([waiting("u1", "and the logs?")], "unanswered", "and the logs?");

    expect(turns?.[0].delivery).toBe("missed");
  });

  // The oldest, because that is the order the run injects them in — marking the
  // later one would leave the earlier waiting forever behind a message it was
  // sent before.
  it("takes the oldest of two identical messages", () => {
    const turns = acknowledge(
      [waiting("u1", "again"), waiting("u2", "again")],
      "context",
      "again",
    );

    expect(turns?.map((t) => t.delivery)).toEqual(["taken", "pending"]);
  });

  it("leaves a message already accounted for alone", () => {
    const settled: Turn = { ...newTurn("u1", "user", "and the logs?"), delivery: "taken" };

    expect(acknowledge([settled], "context", "and the logs?")).toBeNull();
  });

  // Null is what sends it to the transcript instead, which is the only place a
  // message another tab sent can appear at all.
  it("reports no match for a message this window never sent", () => {
    expect(acknowledge([waiting("u1", "mine")], "context", "somebody else's")).toBeNull();
  });

  it("ignores a signal that says nothing about delivery", () => {
    expect(acknowledge([waiting("u1", "mine")], "stop", "mine")).toBeNull();
  });
});

describe("settle", () => {
  // Nothing more is coming: the acknowledgement only ever arrives on the stream
  // the run owns, so a message still waiting when it closes was never taken.
  it("gives up on whatever was still waiting when the run ended", () => {
    const turns = settle([
      { ...newTurn("u1", "user", "taken"), delivery: "taken" },
      { ...newTurn("u2", "user", "waiting"), delivery: "pending" },
      newTurn("u3", "user", "an ordinary question"),
    ]);

    expect(turns.map((t) => t.delivery)).toEqual(["taken", "missed", undefined]);
  });
});
