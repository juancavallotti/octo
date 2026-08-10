/**
 * Attaching "what this block was handed" to "what it did".
 *
 * The runtime publishes a `block.pre-invoke` with the incoming message and a
 * `block.post-invoke` with the outcome, and deliberately does *not* claim the two
 * can be paired: `runtime/types/event.go` measures each invocation's duration at
 * the source precisely because a `fork` runs one address on several goroutines,
 * so a listener cannot tell which pre belongs to which post.
 *
 * That constraint is inherited here. The pre-invoke is a **payload attachment and
 * never a timing input** — every span's interval comes from the post-invoke's own
 * measured duration, so a wrong match cannot move a bar. And when the pairing is
 * not provable the row says the input is unknown, because a plausible wrong
 * payload is worse than no payload: nothing about it looks wrong.
 */

import type { TraceRecord } from "@/app/model/traces";
import { overlaps } from "./timeSpans";
import { addressKey, type WaterfallNode } from "./types";

/** One recorded input, with when it was captured on the trace's own axis. */
export interface PreInvoke {
  record: TraceRecord;
  at: number;
}

/**
 * Attach each block span's input, in place.
 *
 * A span keeps `inputMatched: true` with a null input when nothing was recorded
 * at all — payload capture off, or the record lost. It is set false only when an
 * input exists for that address and could not be attributed to this particular
 * invocation, which is a different thing to tell a reader.
 */
export function attachInputs(nodes: WaterfallNode[], pres: PreInvoke[]): void {
  if (pres.length === 0) return;

  const inputs = new Map<string, PreInvoke[]>();
  for (const pre of pres) {
    const key = addressKey(pre.record.eventId, pre.record.path);
    const bucket = inputs.get(key);
    if (bucket) bucket.push(pre);
    else inputs.set(key, [pre]);
  }

  for (const [key, outcomes] of groupOutcomes(nodes)) {
    const available = inputs.get(key);
    if (!available) continue;

    // Concurrency is the case the runtime warns about: overlapping invocations of
    // one address are a fork's branches, and nothing in the stream says which
    // input became which outcome. Refuse the whole bucket rather than order it by
    // a clock that was never measuring that.
    if (concurrent(outcomes)) {
      for (const node of outcomes) node.inputMatched = false;
      continue;
    }

    // Sequential invocations of one address — a foreach, a retry — can be paired:
    // each takes the latest input recorded at or before it started, and an input
    // is claimed only once.
    const queue = [...available].sort((a, b) => a.at - b.at);
    for (const node of outcomes) {
      let picked = -1;
      for (let i = 0; i < queue.length; i++) {
        if (queue[i].at <= node.start) picked = i;
      }
      if (picked < 0) {
        node.inputMatched = false;
        continue;
      }
      node.input = queue[picked].record;
      queue.splice(picked, 1);
    }
  }
}

/** Block spans by address, each bucket in start order. */
function groupOutcomes(nodes: WaterfallNode[]): Map<string, WaterfallNode[]> {
  const outcomes = new Map<string, WaterfallNode[]>();
  for (const node of nodes) {
    if (node.kind !== "block.post-invoke") continue;
    const key = addressKey(node.eventId, node.path);
    const bucket = outcomes.get(key);
    if (bucket) bucket.push(node);
    else outcomes.set(key, [node]);
  }
  for (const bucket of outcomes.values()) {
    bucket.sort((a, b) => a.start - b.start || a.seq - b.seq);
  }
  return outcomes;
}

/** Whether any two of these invocations were running at once. Sorted by start,
 * an overlap anywhere implies an overlap between neighbours. */
function concurrent(sorted: WaterfallNode[]): boolean {
  for (let i = 1; i < sorted.length; i++) {
    if (overlaps(sorted[i - 1], sorted[i])) return true;
  }
  return false;
}
