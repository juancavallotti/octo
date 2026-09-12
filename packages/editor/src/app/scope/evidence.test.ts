import { describe, expect, it } from "vitest";
import type { BlockMock } from "../meta/types";
import type { Suite } from "../suite/types";
import { emptyEvidence, fromBlockMocks, fromSuite, fromTestInputs } from "./evidence";
import type { ValueShape } from "./types";

/**
 * Everything here is evidence the user wrote down and the repository already carries.
 * The point of the module is that none of it costs a run, so the tests are about
 * reading it faithfully — including refusing to read what is half-written.
 */

const keys = (s: ValueShape | undefined) =>
  s?.kind === "object" ? Object.keys(s.fields).sort() : null;

describe("fromTestInputs", () => {
  it("reads the body and variables the user runs the flow with", () => {
    const shape = fromTestInputs([
      { id: "1", name: "one", data: '{"orderId":"a"}', vars: '{"userId":"u"}' },
    ]);
    expect(keys(shape.body)).toEqual(["orderId"]);
    expect(keys(shape.vars)).toEqual(["userId"]);
  });

  it("merges every input rather than trusting the first", () => {
    const shape = fromTestInputs([
      { id: "1", name: "happy", data: '{"a":1}' },
      { id: "2", name: "sad", data: '{"b":2}' },
    ]);
    expect(keys(shape.body)).toEqual(["a", "b"]);
  });

  it("skips one that is still being typed", () => {
    expect(fromTestInputs([{ id: "1", name: "x", data: '{"a":' }]).body).toBeUndefined();
  });
});

describe("fromBlockMocks", () => {
  const mock = (cases: BlockMock["cases"], fallback?: BlockMock["default"]): BlockMock => ({
    address: "orders.charge",
    enabled: true,
    cases,
    ...(fallback ? { default: fallback } : {}),
  });

  it("takes the body a mock returns as what that block produces", () => {
    // The clearest evidence there is: somebody wrote out what this block answers.
    const evidence = emptyEvidence();
    fromBlockMocks([mock([{ when: "true", body: '{"chargeId":"ch_1"}' }])], evidence);
    expect(keys(evidence.at.get("orders.charge")?.out?.body)).toEqual(["chargeId"]);
  });

  it("merges the cases and the default, since any of them may be what runs", () => {
    const evidence = emptyEvidence();
    fromBlockMocks(
      [mock([{ when: "x", body: '{"a":1}' }], { body: '{"b":2}' })],
      evidence,
    );
    expect(keys(evidence.at.get("orders.charge")?.out?.body)).toEqual(["a", "b"]);
  });

  it("takes the variables a mock sets alongside its body", () => {
    const evidence = emptyEvidence();
    fromBlockMocks([mock([{ when: "true", body: "{}", vars: '{"status":"ok"}' }])], evidence);
    expect(keys(evidence.at.get("orders.charge")?.out?.vars)).toEqual(["status"]);
  });

  it("learns nothing from a case that produces no message", () => {
    const evidence = emptyEvidence();
    fromBlockMocks([mock([{ when: "true", error: "boom" }, { when: "x", drop: true }])], evidence);
    expect(evidence.at.get("orders.charge")).toBeUndefined();
  });
});

describe("fromSuite", () => {
  const suite: Suite = {
    flow: "orders",
    inputs: { vip: { data: { orderId: "a", tier: "vip" }, vars: { userId: "u" } } },
    mocks: { "orders.charge": { cases: [{ when: "true", body: { chargeId: "ch_1" } }] } },
    cases: [
      {
        name: "a vip order",
        input: "vip",
        expect: { body: { receiptUrl: "https://x" }, vars: { emailed: true } },
        spies: {
          "orders.notify": {
            records: [{ input: { body: { to: "a@b" } }, output: { body: { sent: true } } }],
          },
        },
      },
      { name: "inline", input: { data: { orderId: "b", coupon: "X" } } },
    ],
  };

  it("takes a shared input as what the flow starts with", () => {
    const evidence = emptyEvidence();
    fromSuite(suite, evidence);
    expect(keys(evidence.root.get("orders")?.body)).toEqual(["coupon", "orderId", "tier"]);
    expect(keys(evidence.root.get("orders")?.vars)).toEqual(["userId"]);
  });

  it("takes an expectation as what the flow answers with", () => {
    const evidence = emptyEvidence();
    fromSuite(suite, evidence);
    expect(keys(evidence.output.get("orders")?.body)).toEqual(["receiptUrl"]);
    expect(keys(evidence.output.get("orders")?.vars)).toEqual(["emailed"]);
  });

  it("takes the suite's mocks as what those blocks produce", () => {
    const evidence = emptyEvidence();
    fromSuite(suite, evidence);
    expect(keys(evidence.at.get("orders.charge")?.out?.body)).toEqual(["chargeId"]);
  });

  it("takes a spy expectation as both sides of that block", () => {
    const evidence = emptyEvidence();
    fromSuite(suite, evidence);
    const at = evidence.at.get("orders.notify");
    expect(keys(at?.in?.body)).toEqual(["to"]);
    expect(keys(at?.out?.body)).toEqual(["sent"]);
  });

  it("ignores a null mock, which lifts a mock rather than describing one", () => {
    const evidence = emptyEvidence();
    fromSuite({ flow: "orders", mocks: { "orders.charge": null }, cases: [] }, evidence);
    expect(evidence.at.get("orders.charge")).toBeUndefined();
  });

  it("learns nothing from a suite with no flow to attach it to", () => {
    const evidence = emptyEvidence();
    fromSuite({ flow: "", cases: [{ name: "x", input: { data: { a: 1 } } }] }, evidence);
    expect(evidence.root.size).toBe(0);
  });
});
