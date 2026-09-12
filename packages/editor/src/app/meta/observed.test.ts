import { describe, expect, it } from "vitest";
import { byFlow, flowOfAddress, mergeObserved, mergeShape, pruneObserved } from "./observed";
import type { EncodedShape, ObservedEntry } from "./types";

const obj = (f: Record<string, EncodedShape>): EncodedShape => ({ t: "object", f });
const str: EncodedShape = { t: "string" };
const keys = (s: EncodedShape | undefined) => (s?.f ? Object.keys(s.f).sort() : null);

describe("mergeShape", () => {
  it("unions the keys two runs saw", () => {
    expect(keys(mergeShape(obj({ a: str }), obj({ b: str })))).toEqual(["a", "b"]);
  });

  it("widens a field two runs disagreed about", () => {
    expect(mergeShape(obj({ a: str }), obj({ a: { t: "number" } })).f?.a).toEqual({ t: "dyn" });
  });

  it("keeps the shape a run saw when another saw null", () => {
    expect(keys(mergeShape(obj({ a: str }), { t: "null" }))).toEqual(["a"]);
  });

  it("collapses to a map once the union grows past what anyone types", () => {
    const wide = (offset: number) =>
      obj(Object.fromEntries(Array.from({ length: 20 }, (_, i) => [`f${i + offset}`, str])));
    expect(mergeShape(wide(0), wide(100)).f).toEqual({});
  });
});

describe("mergeObserved", () => {
  const entry = (body: EncodedShape): ObservedEntry => ({ out: { body } });

  it("accumulates across runs rather than replacing", () => {
    // A run exercises the cases it was given; the next may take another branch.
    // Replacing would make the menu depend on which case ran most recently.
    const merged = mergeObserved(
      { "orders.charge": entry(obj({ chargeId: str })) },
      { "orders.charge": entry(obj({ refundId: str })) },
    );
    expect(keys(merged["orders.charge"].out?.body)).toEqual(["chargeId", "refundId"]);
  });

  it("keeps an address only one run reached", () => {
    const merged = mergeObserved({ "a.x": entry(obj({ a: str })) }, { "a.y": entry(obj({ b: str })) });
    expect(Object.keys(merged).sort()).toEqual(["a.x", "a.y"]);
  });

  it("keeps in and out apart", () => {
    const merged = mergeObserved(
      { "a.x": { in: { body: obj({ sent: str }) } } },
      { "a.x": { out: { body: obj({ got: str }) } } },
    );
    expect(keys(merged["a.x"].in?.body)).toEqual(["sent"]);
    expect(keys(merged["a.x"].out?.body)).toEqual(["got"]);
  });
});

describe("flowOfAddress", () => {
  it("reads the flow a block address is rooted at", () => {
    expect(flowOfAddress("orders.charge")).toBe("orders");
    expect(flowOfAddress("orders.route[a].send")).toBe("orders");
  });

  it("reads it from an error chain too", () => {
    expect(flowOfAddress("orders[error].notify")).toBe("orders");
  });

  it("has no answer for something that is not an address", () => {
    expect(flowOfAddress("")).toBeNull();
    expect(flowOfAddress(".leading")).toBeNull();
  });
});

describe("byFlow", () => {
  it("files each address under the flow it is in, not the flow that ran", () => {
    // A suite reaches another flow through a flow-ref, and its blocks are addressed
    // at that flow. Filing them under the caller would re-root them on its rename.
    const split = byFlow({
      "orders.charge": { out: { body: obj({ a: str }) } },
      "billing.invoice": { out: { body: obj({ b: str }) } },
    });
    expect([...split.keys()].sort()).toEqual(["billing", "orders"]);
    expect(Object.keys(split.get("orders")!)).toEqual(["orders.charge"]);
  });
});

describe("pruneObserved", () => {
  const observed = { "a.x": { out: { body: str } }, "a.gone": { out: { body: str } } };

  it("drops what no longer exists", () => {
    // A block rename changes its address, and nothing re-roots it — so without this
    // the file grows entries for blocks that are gone, for as long as the project is.
    expect(Object.keys(pruneObserved(observed, ["a.x"]) ?? {})).toEqual(["a.x"]);
  });

  it("becomes absent rather than empty when nothing survives", () => {
    expect(pruneObserved(observed, [])).toBeUndefined();
    expect(pruneObserved(undefined, ["a.x"])).toBeUndefined();
  });
});
