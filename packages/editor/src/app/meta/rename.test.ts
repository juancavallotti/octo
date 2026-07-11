import { describe, it, expect } from "vitest";
import { blankDocument, emptyFlow, type EditorDocument } from "../model/document";
import { flowIdNames, syncFlowNames } from "./rename";
import type { FileMeta } from "./types";

/** A document with the given top-level flows. */
function docOf(...flows: ReturnType<typeof emptyFlow>[]): EditorDocument {
  return { ...blankDocument(), flows };
}

/** Meta holding one input per named flow, so a move is visible. */
function metaOf(...names: string[]): FileMeta {
  const flows: FileMeta["flows"] = {};
  for (const name of names) {
    flows[name] = { inputs: [{ id: `in-${name}`, name: "happy path" }] };
  }
  return { flows };
}

describe("flowIdNames", () => {
  it("maps each top-level flow's id to its name", () => {
    const a = emptyFlow("orders");
    const b = emptyFlow("refunds");
    expect(flowIdNames(docOf(a, b))).toEqual(
      new Map([
        [a.id, "orders"],
        [b.id, "refunds"],
      ]),
    );
  });
});

describe("syncFlowNames", () => {
  it("moves a renamed flow's entry to its new key", () => {
    const flow = emptyFlow("orders");
    const prev = flowIdNames(docOf(flow));
    flow.name = "purchases"; // the reducer renames in place; the id is unchanged
    const next = flowIdNames(docOf(flow));

    const { meta, changed } = syncFlowNames(metaOf("orders"), prev, next);

    expect(changed).toBe(true);
    expect(Object.keys(meta.flows)).toEqual(["purchases"]);
    expect(meta.flows.purchases.inputs[0].id).toBe("in-orders");
  });

  it("is a no-op when nothing was renamed", () => {
    const flow = emptyFlow("orders");
    const ids = flowIdNames(docOf(flow));
    const before = metaOf("orders");

    const { meta, changed } = syncFlowNames(before, ids, ids);

    expect(changed).toBe(false);
    expect(meta).toBe(before); // the same object: nothing to persist
  });

  /**
   * The case that makes this design safe. On a reload every client id is minted
   * afresh, so the two maps share no ids — no id can look "renamed", and the sync
   * cannot misfire on the very situation that makes ids untrustworthy.
   */
  it("is a no-op across a reload, where every id is new", () => {
    const before = docOf(emptyFlow("orders"), emptyFlow("refunds"));
    const reloaded = docOf(emptyFlow("orders"), emptyFlow("refunds")); // fresh ids
    const meta = metaOf("orders", "refunds");

    const { meta: out, changed } = syncFlowNames(
      meta,
      flowIdNames(before),
      flowIdNames(reloaded),
    );

    expect(changed).toBe(false);
    expect(Object.keys(out.flows).sort()).toEqual(["orders", "refunds"]);
  });

  // A flow removed by accident should not take its saved inputs with it — undoing the
  // delete brings them back.
  it("keeps the entry of a deleted flow", () => {
    const flow = emptyFlow("orders");
    const prev = flowIdNames(docOf(flow));
    const next = flowIdNames(docOf()); // the flow is gone

    const { meta, changed } = syncFlowNames(metaOf("orders"), prev, next);

    expect(changed).toBe(false);
    expect(meta.flows.orders).toBeDefined();
  });

  it("ignores a flow that is new in this document", () => {
    const added = emptyFlow("refunds");
    const { meta, changed } = syncFlowNames(
      metaOf("orders"),
      flowIdNames(docOf()),
      flowIdNames(docOf(added)),
    );

    expect(changed).toBe(false);
    expect(Object.keys(meta.flows)).toEqual(["orders"]);
  });

  // Merging two flows' inputs would be worse than leaving a stale key behind: the
  // document is invalid anyway (duplicate flow names) and the user must fix it.
  it("refuses a rename onto a name already in use", () => {
    const flow = emptyFlow("orders");
    const prev = flowIdNames(docOf(flow));
    flow.name = "refunds"; // now collides with an existing entry
    const next = flowIdNames(docOf(flow));

    const { meta, changed } = syncFlowNames(metaOf("orders", "refunds"), prev, next);

    expect(changed).toBe(false);
    expect(meta.flows.orders.inputs[0].id).toBe("in-orders");
    expect(meta.flows.refunds.inputs[0].id).toBe("in-refunds");
  });

  it("ignores a rename of a flow that has no saved inputs", () => {
    const flow = emptyFlow("orders");
    const prev = flowIdNames(docOf(flow));
    flow.name = "purchases";
    const next = flowIdNames(docOf(flow));

    const { changed } = syncFlowNames({ flows: {} }, prev, next);

    expect(changed).toBe(false);
  });
});
