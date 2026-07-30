import { describe, it, expect } from "vitest";
import { promoteRun, type PromotableRun } from "./promote";

/** A run that produced a message, with the setup that produced it. */
function ran(over: Partial<PromotableRun> = {}): PromotableRun {
  return {
    outcome: "message",
    message: {
      event_id: "e1",
      variables: { tier: "gold", "octo.attempt": "1" },
      body: { charged: true },
    },
    setup: {
      input: { data: { orderId: 42 } },
      mocks: { "orders.pay": { default: { body: { ok: true } } } },
      spies: { "orders.audit": 2 },
    },
    ...over,
  };
}

describe("promoteRun", () => {
  /**
   * The trap. A run's result is the whole `{event_id, variables, body}` envelope, and a
   * case that asserted that as its body would compare a body against an envelope — a
   * test that cannot pass, on every promoted case.
   */
  it("asserts the message's body, not the message", () => {
    const c = promoteRun(ran(), { name: "it charges" });

    expect(c.expect?.body).toEqual({ charged: true });
  });

  /**
   * `expect.vars` is a subset check, so seeding it from everything the run built up
   * asserts more than the user meant — engine bookkeeping included — and breaks on
   * changes that have nothing to do with the test.
   */
  it("leaves the variables alone unless they are asked for", () => {
    expect(promoteRun(ran(), { name: "x" }).expect?.vars).toBeUndefined();

    expect(promoteRun(ran(), { name: "x", vars: true }).expect?.vars).toEqual({
      tier: "gold",
      "octo.attempt": "1",
    });
  });

  it("carries the input and the mocks the run was made with", () => {
    const c = promoteRun(ran(), { name: "it charges" });

    expect(c.input).toEqual({ data: { orderId: 42 } });
    expect(c.mocks).toEqual({ "orders.pay": { default: { body: { ok: true } } } });
  });

  it("asserts what the spies saw only when asked", () => {
    expect(promoteRun(ran(), { name: "x" }).spies).toBeUndefined();

    expect(promoteRun(ran(), { name: "x", spies: true }).spies).toEqual({
      "orders.audit": { count: 2 },
    });
  });

  // `count: 0` says the block never ran, which is an assertion people reach for often.
  // Coercing it to "no assertion" would quietly drop the test they meant to write.
  it("keeps a spy that saw nothing as count 0", () => {
    const c = promoteRun(ran({ setup: { spies: { "orders.audit": 0 } } }), {
      name: "x",
      spies: true,
    });

    expect(c.spies).toEqual({ "orders.audit": { count: 0 } });
  });

  it("states a dropped message as the outcome it is", () => {
    const c = promoteRun(ran({ outcome: "dropped" }), { name: "it filters" });

    expect(c.expect).toEqual({ dropped: true });
  });

  /**
   * What the console holds for a failed flow is the runner's stderr — a timestamped log
   * line — so the phrase to assert is the user's to give. The setup that provoked the
   * failure is the valuable half, and it comes across either way.
   */
  it("asserts the failure text the caller gives, and keeps the setup", () => {
    const c = promoteRun(ran({ outcome: "error", message: undefined }), {
      name: "a declined card",
      errorContains: "card declined",
    });

    expect(c.expect).toEqual({ error: "card declined" });
    expect(c.mocks).toEqual({ "orders.pay": { default: { body: { ok: true } } } });
  });

  // dolphin has no "expect any failure", so without a phrase the case says the flow
  // completed — the opposite of what happened. The UI requires one; this pins why.
  it("asserts nothing for a failure with no text to look for", () => {
    const c = promoteRun(ran({ outcome: "error" }), { name: "x" });

    expect(c.expect).toBeUndefined();
  });

  it("asserts only that the flow ran when it produced no body", () => {
    const c = promoteRun(ran({ message: { event_id: "e1", variables: {} } }), {
      name: "x",
    });

    expect(c.expect).toBeUndefined();
  });

  // A run whose output was not a message at all invents nothing.
  it("asserts nothing when the output was not a message", () => {
    const c = promoteRun(ran({ message: "something the runner printed" }), { name: "x" });

    expect(c.expect).toBeUndefined();
  });

  it("says nothing about an input or a mock the run did not have", () => {
    const c = promoteRun(ran({ setup: { input: {}, mocks: {} } }), { name: "  it runs  " });

    expect(c.name).toBe("it runs");
    expect(c.input).toBeUndefined();
    expect(c.mocks).toBeUndefined();
  });
});
