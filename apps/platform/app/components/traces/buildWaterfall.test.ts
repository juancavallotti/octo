import { describe, expect, it } from "vitest";
import { buildWaterfall } from "./buildWaterfall";
import { at, records, T0 } from "./fixtures";
import type { WaterfallNode } from "./types";

/**
 * The reconstruction, against the shapes the runtime actually produces.
 *
 * Every scenario here is one the engine can generate: a request through a chain,
 * a composite with a branch, an agent making model calls, a flow-ref, a fork on
 * several goroutines, an invocation whose terminal was lost. What is asserted is
 * not just the tree but the *reason* for it — which evidence won where the
 * address and the clock disagreed, and what the builder refused to guess.
 */

/** The tree as `label` strings, so an assertion reads like the picture. */
function outline(nodes: WaterfallNode[], indent = ""): string[] {
  return nodes.flatMap((node) => [
    `${indent}${node.label}`,
    ...outline(node.children, `${indent}  `),
  ]);
}

/** Find one node anywhere in the tree. */
function find(nodes: WaterfallNode[], label: string): WaterfallNode | undefined {
  for (const node of nodes) {
    if (node.label === label) return node;
    const deeper = find(node.children, label);
    if (deeper) return deeper;
  }
  return undefined;
}

describe("buildWaterfall", () => {
  it("draws a request as the tree it ran as", () => {
    const trace = records(
      { kind: "source.receive", start: 0, attrs: { method: "POST", route: "/orders" } },
      { kind: "flow.started", start: 1_000, flow: "orders" },
      { kind: "block.pre-invoke", start: 2_000, path: "orders.validate" },
      { kind: "block.post-invoke", start: 2_000, duration: 40_000, path: "orders.validate" },
      { kind: "block.post-invoke", start: 50_000, duration: 900_000, path: "orders.charge" },
      { kind: "flow.completed", start: 1_000, duration: 960_000, flow: "orders" },
      {
        kind: "source.respond",
        start: 0,
        duration: 1_000_000,
        attrs: { method: "POST", route: "/orders", status: 200 },
      },
    );

    const { roots } = buildWaterfall(trace);
    expect(outline(roots)).toEqual([
      "POST /orders",
      "  orders",
      "    validate",
      "    charge",
    ]);
  });

  it("places a span from the completion the runtime stamped it at", () => {
    // The single rule the whole reconstruction rests on: a record marks the end,
    // so the bar is [ts - durationNs, ts]. Read the other way round, every bar in
    // the chart would sit one duration too late.
    const trace = records(
      { kind: "flow.completed", start: 0, duration: 1_000_000, flow: "orders" },
      { kind: "block.post-invoke", start: 400_000, duration: 250_000, path: "orders.charge" },
    );

    const charge = find(buildWaterfall(trace).roots, "charge")!;
    expect(charge.start).toBe(400_000);
    expect(charge.end).toBe(650_000);
  });

  it("keeps two blocks 40 microseconds apart apart", () => {
    // Both finish inside one millisecond. Rounding to milliseconds would give
    // them the same instant, and their order would come down to a coin flip.
    const trace = records(
      { kind: "flow.completed", start: 0, duration: 1_000_000, flow: "orders" },
      { kind: "block.post-invoke", start: 10_000, duration: 40_000, path: "orders.a" },
      { kind: "block.post-invoke", start: 50_000, duration: 40_000, path: "orders.b" },
    );

    const { roots } = buildWaterfall(trace);
    expect(outline(roots)).toEqual(["orders", "  a", "  b"]);
    expect(find(roots, "a")!.end).toBe(50_000);
    expect(find(roots, "b")!.start).toBe(50_000);
  });

  it("nests a block under the composite its address puts it in", () => {
    const trace = records(
      { kind: "block.post-invoke", start: 20_000, duration: 30_000, path: "orders.check[then].notify" },
      { kind: "block.post-invoke", start: 10_000, duration: 60_000, path: "orders.check" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    );

    expect(outline(buildWaterfall(trace).roots)).toEqual([
      "orders",
      "  check",
      "    notify",
    ]);
  });

  it("hangs a model call off the block that made it", () => {
    // An llm.turn is stamped with its block's *own* address rather than one below
    // it. Walking that path for an enclosing block would find the composite the
    // agent sits in and stop there — the call would be drawn as the agent's
    // sibling, billed to the branch instead of to the block that made it.
    const trace = records(
      {
        kind: "llm.turn",
        start: 20_000,
        duration: 400_000,
        path: "orders.route[then].agent",
        blockType: "ai-agent",
        attrs: { model: "claude-sonnet-4" },
      },
      { kind: "block.post-invoke", start: 15_000, duration: 500_000, path: "orders.route[then].agent" },
      { kind: "block.post-invoke", start: 10_000, duration: 600_000, path: "orders.route" },
      { kind: "flow.completed", start: 0, duration: 700_000, flow: "orders" },
    );

    const { roots } = buildWaterfall(trace);
    expect(outline(roots)).toEqual(["orders", "  route", "    agent", "      agent"]);

    const call = find(roots, "route")!.children[0].children[0];
    expect(call.kind).toBe("llm.turn");
  });

  it("hangs a flow-ref's sub-invocation off the block that called it", () => {
    // The sub-flow roots at its own name and carries a fresh event id, so its
    // address shares nothing with the caller's. Only the clock joins them.
    const trace = records(
      { kind: "block.post-invoke", start: 30_000, duration: 40_000, path: "pricing.lookup", eventId: "ev-sub" },
      { kind: "flow.completed", start: 25_000, duration: 50_000, flow: "pricing", eventId: "ev-sub" },
      { kind: "block.post-invoke", start: 20_000, duration: 60_000, path: "orders.price" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    );

    expect(outline(buildWaterfall(trace).roots)).toEqual([
      "orders",
      "  price",
      "    pricing",
      "      lookup",
    ]);
  });

  it("reads the later-published of two identical spans as the outer one", () => {
    // A flow-ref whose sub-flow was measured to the very same instants as the
    // block that called it — with fast blocks and a coarse clock, routine. The
    // clock has nothing left to say, so publication order decides: a caller
    // finishes after the thing it called, so its record is published second.
    const trace = records(
      { kind: "flow.completed", start: 10_000, duration: 40_000, flow: "pricing", eventId: "ev-sub" },
      { kind: "block.post-invoke", start: 10_000, duration: 40_000, path: "orders.price" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    );

    expect(outline(buildWaterfall(trace).roots)).toEqual([
      "orders",
      "  price",
      "    pricing",
    ]);
  });

  it("trusts the address when the clock disagrees with it", () => {
    // A child measured as finishing after its composite did. The address is
    // structural and the clock is not, so the nesting stands — and the trace says
    // it had to make that call rather than quietly making it.
    const trace = records(
      { kind: "block.post-invoke", start: 10_000, duration: 90_000, path: "orders.check[then].slow" },
      { kind: "block.post-invoke", start: 10_000, duration: 50_000, path: "orders.check" },
      { kind: "flow.completed", start: 0, duration: 200_000, flow: "orders" },
    );

    const { roots, warnings } = buildWaterfall(trace);
    expect(outline(roots)).toEqual(["orders", "  check", "    slow"]);
    expect(warnings.map((w) => w.kind)).toContain("child-outlives-parent");
  });

  describe("a fork, running one address on several goroutines", () => {
    const trace = records(
      { kind: "block.pre-invoke", start: 10_000, path: "orders.fan[0].call" },
      { kind: "block.pre-invoke", start: 11_000, path: "orders.fan[0].call" },
      { kind: "block.post-invoke", start: 10_000, duration: 80_000, path: "orders.fan[0].call" },
      { kind: "block.post-invoke", start: 11_000, duration: 70_000, path: "orders.fan[0].call" },
      { kind: "block.post-invoke", start: 5_000, duration: 100_000, path: "orders.fan" },
      { kind: "flow.completed", start: 0, duration: 120_000, flow: "orders" },
    );

    it("keeps both invocations rather than collapsing them", () => {
      const fan = find(buildWaterfall(trace).roots, "fan")!;
      expect(fan.children).toHaveLength(2);
    });

    it("lanes them, which is the only sign they ran at once", () => {
      const fan = find(buildWaterfall(trace).roots, "fan")!;
      expect(fan.children.map((c) => c.lane)).toEqual([0, 1]);
    });

    it("refuses to say which input became which outcome", () => {
      // runtime/types/event.go is explicit that pre/post pairing is unreliable
      // here. A plausible wrong payload is worse than none: nothing looks wrong.
      const fan = find(buildWaterfall(trace).roots, "fan")!;
      expect(fan.children.map((c) => c.inputMatched)).toEqual([false, false]);
      expect(fan.children.map((c) => c.input)).toEqual([null, null]);
    });
  });

  it("pairs inputs with outcomes when the invocations were sequential", () => {
    // A foreach runs one address repeatedly but never at the same time, so each
    // outcome takes the last input recorded before it started.
    const trace = records(
      { kind: "block.pre-invoke", start: 10_000, path: "orders.each[body].send", body: { n: 1 } },
      { kind: "block.post-invoke", start: 10_000, duration: 20_000, path: "orders.each[body].send" },
      { kind: "block.pre-invoke", start: 40_000, path: "orders.each[body].send", body: { n: 2 } },
      { kind: "block.post-invoke", start: 40_000, duration: 20_000, path: "orders.each[body].send" },
      { kind: "block.post-invoke", start: 5_000, duration: 60_000, path: "orders.each" },
      { kind: "flow.completed", start: 0, duration: 80_000, flow: "orders" },
    );

    const each = find(buildWaterfall(trace).roots, "each")!;
    expect(each.children.map((c) => c.input?.body)).toEqual([{ n: 1 }, { n: 2 }]);
    expect(each.children.every((c) => c.inputMatched)).toBe(true);
  });

  it("says nothing about an input that was never recorded", () => {
    // Payload capture off is not the same as a pairing that could not be made,
    // and the row has to be able to tell a reader which one it is looking at.
    const trace = records(
      { kind: "block.post-invoke", start: 10_000, duration: 20_000, path: "orders.send" },
      { kind: "flow.completed", start: 0, duration: 80_000, flow: "orders" },
    );

    const send = find(buildWaterfall(trace).roots, "send")!;
    expect(send.input).toBeNull();
    expect(send.inputMatched).toBe(true);
  });

  it("stands in for an invocation whose outcome was lost", () => {
    // Without a stand-in, these blocks would be adopted by whatever unrelated
    // span happened to enclose them — a tree that looks complete and is wrong.
    const trace = records(
      { kind: "block.post-invoke", start: 30_000, duration: 10_000, path: "pricing.lookup", eventId: "ev-sub", flow: "pricing" },
      { kind: "block.post-invoke", start: 20_000, duration: 60_000, path: "orders.price" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    );

    const { roots, warnings } = buildWaterfall(trace);
    expect(outline(roots)).toEqual(["orders", "  price", "    pricing", "      lookup"]);

    const inferred = find(roots, "pricing")!;
    expect(inferred.inferred).toBe(true);
    expect(inferred.record).toBeNull();
    // Its extent is a lower bound: only what ran inside it is known.
    expect(inferred.start).toBe(30_000);
    expect(inferred.end).toBe(40_000);
    expect(warnings.map((w) => w.kind)).toContain("no-terminal");
  });

  it("draws no row for a record that marks an instant", () => {
    // flow.started, source.receive and block.pre-invoke are markers. Drawing them
    // would double every block in the chart.
    const trace = records(
      { kind: "source.receive", start: 0 },
      { kind: "flow.started", start: 0, flow: "orders" },
      { kind: "block.pre-invoke", start: 10_000, path: "orders.send" },
      { kind: "block.post-invoke", start: 10_000, duration: 20_000, path: "orders.send" },
      { kind: "flow.completed", start: 0, duration: 50_000, flow: "orders" },
    );

    const { roots, count } = buildWaterfall(trace);
    expect(count).toBe(2);
    expect(outline(roots)).toEqual(["orders", "  send"]);
  });

  it("never reports a gap in seq as lost records", () => {
    // seq is gapless across a whole publisher, not within a trace, so one
    // trace's records are normally non-consecutive. Reading a gap here as loss
    // would put a warning on essentially every trace ever recorded.
    const trace = records(
      { kind: "block.post-invoke", start: 10_000, duration: 20_000, path: "orders.send", seq: 4 },
      { kind: "flow.completed", start: 0, duration: 50_000, flow: "orders", seq: 91 },
    );

    expect(buildWaterfall(trace).warnings).toEqual([]);
  });

  it("says when a block was entered and never came back", () => {
    const trace = records(
      { kind: "block.pre-invoke", start: 10_000, path: "orders.hang" },
      { kind: "flow.failed", start: 0, duration: 50_000, flow: "orders" },
    );

    const warning = buildWaterfall(trace).warnings.find(
      (w) => w.kind === "block-never-returned",
    );
    expect(warning?.message).toContain("orders.hang");
  });

  it("takes its extent from the stored rollup so the two agree", () => {
    // The list showed a duration computed from the summary. A chart measured
    // from the records alone would be narrower whenever a record was truncated
    // away, and the same trace would read as two different lengths.
    const trace = records({
      kind: "block.post-invoke",
      start: 10_000,
      duration: 20_000,
      path: "orders.send",
    });

    const { originMs, spanNs } = buildWaterfall(trace, {
      startedAt: T0,
      endedAt: at(500_000),
    });
    expect(originMs).toBe(Date.parse(T0));
    expect(spanNs).toBe(500_000);
  });

  it("widens the rollup's extent rather than clipping a record out of it", () => {
    const trace = records({
      kind: "block.post-invoke",
      start: 0,
      duration: 900_000,
      path: "orders.send",
    });

    expect(buildWaterfall(trace, { startedAt: T0, endedAt: at(100) }).spanNs).toBe(900_000);
  });

  it("drops a record it cannot place, and says it did", () => {
    const trace = records(
      { kind: "flow.completed", start: 0, duration: 50_000, flow: "orders" },
      { kind: "block.post-invoke", start: 10_000, duration: 20_000, path: "orders.send" },
    );
    trace[1].ts = "not a timestamp";

    const { roots, warnings } = buildWaterfall(trace);
    expect(outline(roots)).toEqual(["orders"]);
    expect(warnings.map((w) => w.kind)).toContain("unreadable-timestamp");
  });

  it("returns an empty chart rather than dividing by zero", () => {
    const empty = buildWaterfall([]);
    expect(empty.roots).toEqual([]);
    expect(empty.spanNs).toBe(1);
  });

  it("gives a trace that took no measurable time a width anyway", () => {
    const trace = records({ kind: "flow.completed", start: 0, duration: 0, flow: "orders" });
    expect(buildWaterfall(trace).spanNs).toBe(1);
  });

  it("reads the same trace the same way whatever order the records arrive in", () => {
    // Records are written in batches and are not ordered by seq on the wire, so
    // a tree that depended on arrival order would differ between two readers of
    // the same stored trace.
    const trace = records(
      { kind: "block.post-invoke", start: 20_000, duration: 30_000, path: "orders.check[then].notify" },
      { kind: "block.post-invoke", start: 10_000, duration: 60_000, path: "orders.check" },
      { kind: "block.post-invoke", start: 80_000, duration: 10_000, path: "orders.done" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    );

    const expected = outline(buildWaterfall(trace).roots);
    for (let i = 0; i < trace.length; i++) {
      const rotated = [...trace.slice(i), ...trace.slice(0, i)];
      expect(outline(buildWaterfall(rotated).roots), `rotated by ${i}`).toEqual(expected);
    }
    expect([...trace].reverse().length).toBe(4);
    expect(outline(buildWaterfall([...trace].reverse()).roots)).toEqual(expected);
  });
});
