/**
 * Reading the aggregator's fold back out of a record.
 *
 * These are the two places a folded row could quietly lie: a span labelled as one
 * frame while standing for a thousand, and a body shown as the run's text when it
 * is only the first frame's.
 */

import { describe, expect, it } from "vitest";

import { bodyIsFirstOnly, foldedCount, spanLabel } from "./folded";
import type { TraceRecord } from "@/app/model/traces";

function record(attrs: Record<string, unknown>, over: Partial<TraceRecord> = {}): TraceRecord {
  return {
    id: "r1",
    trace_id: "t1",
    seq: 1,
    kind: "block.post-invoke",
    event_id: "e1",
    correlation_id: "",
    flow: "chat",
    path: "chat.dr-octo[events].sse-event",
    block_type: "sse-event",
    ts: "2026-08-23T03:00:00Z",
    duration_ns: 1000,
    error: "",
    dropped: false,
    truncated: false,
    attrs,
    app_name: "octo",
    app_version: "v1",
    ...over,
  } as unknown as TraceRecord;
}

describe("foldedCount", () => {
  it("is zero for a record that stands only for itself", () => {
    expect(foldedCount(record({}))).toBe(0);
  });

  it("reads the count the aggregator wrote", () => {
    expect(foldedCount(record({ folded: { count: 1204, firstSeq: 1, lastSeq: 2407 } }))).toBe(1204);
  });

  // A fold of one is not a fold, and saying "×1" on a row would be noise.
  it("ignores a count of one", () => {
    expect(foldedCount(record({ folded: { count: 1 } }))).toBe(0);
  });

  // attrs is whatever a runtime wrote, including a runtime newer than this UI.
  it("survives a folded attribute of the wrong shape", () => {
    expect(foldedCount(record({ folded: "yes" }))).toBe(0);
    expect(foldedCount(record({ folded: null }))).toBe(0);
    expect(foldedCount(record({ folded: { count: "many" } }))).toBe(0);
  });
});

describe("bodyIsFirstOnly", () => {
  it("is false when the run's payloads were merged", () => {
    expect(bodyIsFirstOnly(record({ folded: { count: 9 } }))).toBe(false);
  });

  it("is true when the aggregator could not merge them", () => {
    expect(bodyIsFirstOnly(record({ folded: { count: 9, bodies: "first" } }))).toBe(true);
  });
});

describe("spanLabel", () => {
  it("names a block by its address", () => {
    expect(spanLabel(record({}))).toBe("sse-event");
  });

  // Without the count a folded span reads as one frame that took ten seconds,
  // which is the wrong thing to conclude about it.
  it("says how many records a folded row stands for", () => {
    expect(spanLabel(record({ folded: { count: 1204 } }))).toBe("sse-event ×1204");
  });

  it("falls back to the route for a response record", () => {
    const resp = record({ method: "POST", route: "/chat" }, { kind: "source.respond", path: "" });
    expect(spanLabel(resp)).toBe("POST /chat");
  });

  it("falls back to the flow when a record names nothing else", () => {
    expect(spanLabel(record({}, { path: "", kind: "flow.completed" }))).toBe("chat");
  });
});
