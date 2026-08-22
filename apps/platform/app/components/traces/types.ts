/**
 * What a reconstructed trace looks like once the records have been turned into a
 * tree. Shared by the builder and every component that draws one.
 */

import type { TraceRecord } from "@/app/model/traces";

/**
 * The record kinds that occupy an interval rather than mark an instant.
 *
 * The runtime stamps a record when the thing it describes *finished* and carries
 * how long it took, so a span is exactly `[ts - durationNs, ts]` and no pairing
 * of a start record with an end record is needed. That is what makes a waterfall
 * reconstructible from a stream that has no span ids in it at all.
 */
export const SPAN_KINDS = new Set([
  "block.post-invoke",
  "flow.completed",
  "flow.dropped",
  "flow.failed",
  "llm.turn",
  "llm.embed",
  "agent.compaction",
  "source.respond",
]);

/** The three kinds that end an invocation, whatever the outcome. */
export const FLOW_TERMINALS = new Set([
  "flow.completed",
  "flow.dropped",
  "flow.failed",
]);

/** One model call. Used for cost, and classified by kind rather than by block. */
export const MODEL_KINDS = new Set(["llm.turn", "llm.embed"]);

/**
 * The kinds a block stamps with its *own* address rather than one below it, so
 * the block at exactly that path is their host rather than an enclosing one.
 *
 * Model calls are the obvious members. `agent.compaction` is the other: an agent
 * shrinking its own conversation is work it did at its own address, and walking
 * for an *enclosing* block would hang it off whatever composite the agent sits
 * in — drawn as the agent's sibling, billed to the branch instead of to the
 * block that did it.
 *
 * A summarize compaction and the model call it makes therefore land as siblings
 * under the agent, both at its address. They are not double counted: only a
 * block.post-invoke can host a span, so the compaction has no children, and a
 * childless span is classified by its block — an ai-agent, which is control.
 * The call's own time is billed once, as the model call it is.
 */
export const SELF_ADDRESSED_KINDS = new Set([
  ...MODEL_KINDS,
  "agent.compaction",
]);

/**
 * The key identifying one block's invocations: which message, and where.
 *
 * Neither half alone will do. A path repeats across the sub-invocations a
 * flow-ref or a split creates, and one event id covers every block of one
 * invocation. A NUL joins them because neither can contain one, so the pair
 * cannot be spelled two ways.
 */
export function addressKey(eventId: string, path: string): string {
  return `${eventId}\u0000${path}`;
}

/** One row of the waterfall: a span, and the spans that ran inside it. */
export interface WaterfallNode {
  /** The record's id, or `inferred:<eventId>` for a root nothing reported. */
  id: string;
  kind: string;
  /** How the row is labelled: the block's own name, or the flow's. */
  label: string;
  /** The block address this span was reported at; empty for flow and source spans. */
  path: string;
  eventId: string;
  /** Nanoseconds from the trace origin. */
  start: number;
  end: number;
  /** As the runtime measured it, which is not always `end - start` for an
   * inferred node — there, the interval is derived and this is 0. */
  durationNs: number;
  seq: number;
  /** The record behind this row, or null for an inferred root. */
  record: TraceRecord | null;
  /**
   * The `block.pre-invoke` carrying what this block was handed, when it could be
   * matched. Null means either none was recorded or the match was not provable —
   * see {@link inputMatched}, which distinguishes the two.
   */
  input: TraceRecord | null;
  /**
   * False when a pre-invoke exists for this address but could not be attributed
   * to this particular invocation, which is what a `fork` does: it runs the same
   * path on several goroutines, so nothing joins one input to one outcome. The
   * row must say the input is unknown rather than show a plausible wrong one.
   */
  inputMatched: boolean;
  /** True when no terminal record survived and this span was derived from its
   * children — the interval is a lower bound on what really ran. */
  inferred: boolean;
  /** Which row this span sits on among siblings that overlap it. 0 unless
   * something ran concurrently. */
  lane: number;
  /** Depth in the tree, so a flattened row knows its indent without a walk. */
  depth: number;
  children: WaterfallNode[];
}

/** Something provable about this trace's own records that a reader should know. */
export interface WaterfallWarning {
  kind:
    | "no-terminal"
    | "block-never-returned"
    | "child-outlives-parent"
    | "unreadable-timestamp";
  /** One line, already written for a reader rather than for a log. */
  message: string;
}

/** A trace, ready to draw. */
export interface Waterfall {
  roots: WaterfallNode[];
  /** Epoch milliseconds of offset 0, for wall-clock axis labels. */
  originMs: number;
  /** The full width of the chart in nanoseconds; at least 1 so nothing divides
   * by zero on a trace that took no measurable time. */
  spanNs: number;
  /** How many span rows the tree holds, including collapsed ones. */
  count: number;
  warnings: WaterfallWarning[];
}
