import { describe, expect, it } from "vitest";
import { mergeShape, parseTrace, reduceTrace, shapeOf, type Shape } from "./shapes";

/**
 * The privacy rule is the reason this module exists, so most of what is pinned here
 * is what must NOT come out of it.
 */

const keys = (s: Shape | undefined) => (s?.f ? Object.keys(s.f).sort() : null);
const json = (s: unknown) => JSON.stringify(s);

describe("shapeOf", () => {
  it("keeps keys and types", () => {
    expect(shapeOf({ id: "a", n: 1, ok: true })).toEqual({
      t: "object",
      f: { id: { t: "string" }, n: { t: "number" }, ok: { t: "bool" } },
    });
  });

  it("carries no value anywhere in its output", () => {
    const secret = { token: "Bearer sk-live-abcdef", email: "a@b.com", card: "4111111111111111" };
    const out = json(shapeOf(secret));
    expect(out).not.toContain("sk-live");
    expect(out).not.toContain("a@b.com");
    expect(out).not.toContain("4111");
    // The field NAMES are the point, and they survive.
    expect(keys(shapeOf(secret))).toEqual(["card", "email", "token"]);
  });

  it("collapses an object keyed by data to a bare map", () => {
    // Keys are values here — an object keyed by email address publishes the
    // addresses. Nobody completes a path into one, so it says only "a map".
    const byEmail = { "a@x.com": 1, "b@y.com": 2, "c@z.com": 3 };
    expect(shapeOf(byEmail)).toEqual({ t: "object", f: {} });
    expect(json(shapeOf(byEmail))).not.toContain("@");
  });

  it("collapses an object keyed by id", () => {
    const byId = Object.fromEntries(
      ["550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"].map((k) => [k, 1]),
    );
    expect(shapeOf(byId).f).toEqual({});
  });

  it("collapses an object with more keys than anyone types", () => {
    const wide = Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`field${i}`, i]));
    expect(shapeOf(wide).f).toEqual({});
  });

  it("keeps a normal record of the same data's field names", () => {
    expect(keys(shapeOf({ userId: "u", orderId: "o" }))).toEqual(["orderId", "userId"]);
  });

  it("merges a list's elements and stops after a sample", () => {
    expect(shapeOf([{ a: 1 }, { b: 2 }])).toEqual({
      t: "list",
      of: { t: "object", f: { a: { t: "number" }, b: { t: "number" } } },
    });
  });

  it("stops descending rather than committing a whole tree to a file", () => {
    let deep: unknown = "leaf";
    for (let i = 0; i < 20; i++) deep = { next: deep };
    expect(json(shapeOf(deep)).length).toBeLessThan(200);
  });
});

describe("mergeShape", () => {
  it("widens two different scalars", () => {
    expect(mergeShape({ t: "string" }, { t: "number" })).toEqual({ t: "dyn" });
  });

  it("unions object keys across runs", () => {
    expect(keys(mergeShape(shapeOf({ a: 1 }), shapeOf({ b: 2 })))).toEqual(["a", "b"]);
  });

  it("keeps the shape when one run saw null", () => {
    expect(keys(mergeShape(shapeOf({ a: 1 }), { t: "null" }))).toEqual(["a"]);
  });

  it("stays collapsed once either side was a bare map", () => {
    expect(mergeShape({ t: "object", f: {} }, shapeOf({ a: 1 })).f).toEqual({ a: { t: "number" } });
  });
});

describe("reduceTrace", () => {
  const records = [
    { kind: "flow.started", flow: "orders" },
    { kind: "block.pre-invoke", path: "orders.charge", body: { orderId: "a" } },
    { kind: "block.post-invoke", path: "orders.charge", body: { chargeId: "c" }, vars: { status: "ok" } },
  ];

  it("separates what a block received from what it produced", () => {
    const out = reduceTrace(records);
    expect(keys(out.get("orders.charge")?.in?.body)).toEqual(["orderId"]);
    expect(keys(out.get("orders.charge")?.out?.body)).toEqual(["chargeId"]);
    expect(keys(out.get("orders.charge")?.out?.vars)).toEqual(["status"]);
  });

  it("ignores records that are not about a block", () => {
    expect(reduceTrace([{ kind: "flow.started" }]).size).toBe(0);
  });

  it("accumulates across several cases' traces", () => {
    const out = reduceTrace(records);
    reduceTrace([{ kind: "block.post-invoke", path: "orders.charge", body: { refundId: "r" } }], out);
    expect(keys(out.get("orders.charge")?.out?.body)).toEqual(["chargeId", "refundId"]);
  });
});

describe("parseTrace", () => {
  it("reads JSON Lines", () => {
    expect(parseTrace('{"kind":"a"}\n{"kind":"b"}\n')).toHaveLength(2);
  });

  it("keeps everything before a torn last line", () => {
    // The writing process can be killed mid-record; what came before is still good.
    expect(parseTrace('{"kind":"a"}\n{"kind":"b"')).toHaveLength(1);
  });
});
