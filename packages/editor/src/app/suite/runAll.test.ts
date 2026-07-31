import { describe, it, expect } from "vitest";
import { planRunAll } from "./runAll";

const good = (flow: string) =>
  `flow: ${flow}\ncases:\n  - name: it runs\n    expect: {}\n`;

describe("planRunAll", () => {
  it("sends every suite whose flow is in the document", () => {
    const plan = planRunAll(
      [
        { flow: "orders", content: good("orders") },
        { flow: "refunds", content: good("refunds") },
      ],
      ["orders", "refunds"],
    );

    expect(plan.targets).toEqual([
      { name: "orders", content: good("orders") },
      { name: "refunds", content: good("refunds") },
    ]);
    expect(plan.skipped).toEqual([]);
  });

  // A rename leaves the old suite on disk, where the tab cannot show it but dolphin and CI
  // both still find it. Saying so here is usually the first the user hears of it.
  it("holds back a suite for a flow this document no longer has", () => {
    const plan = planRunAll([{ flow: "legacy", content: good("legacy") }], ["orders"]);

    expect(plan.targets).toEqual([]);
    expect(plan.skipped).toEqual([
      { flow: "legacy", reason: "there is no flow by that name in this document" },
    ]);
  });

  it("holds back a suite with no cases, as the starting state it is", () => {
    const plan = planRunAll([{ flow: "orders", content: "flow: orders\ncases: []\n" }], [
      "orders",
    ]);

    expect(plan.targets).toEqual([]);
    expect(plan.skipped).toEqual([{ flow: "orders", reason: "has no cases yet" }]);
  });

  // dolphin loads every named suite before running any case, so one it refuses would take
  // the whole run down with it and report nothing about the suites that were fine.
  it("holds back a suite dolphin would refuse, and says what is wrong", () => {
    const plan = planRunAll(
      [{ flow: "orders", content: "flow: orders\ncases:\n  - name: a\n    spys: {}\n" }],
      ["orders"],
    );

    expect(plan.targets).toEqual([]);
    expect(plan.skipped).toHaveLength(1);
    expect(plan.skipped[0].flow).toBe("orders");
    expect(plan.skipped[0].reason).toContain("dolphin would refuse it");
    expect(plan.skipped[0].reason).toContain("spys");
  });

  it("runs the good suites and reports the bad one, in store order", () => {
    const plan = planRunAll(
      [
        { flow: "checkout", content: good("checkout") },
        { flow: "orders", content: "flow: orders\ncases:\n  - name: a\n    spys: {}\n" },
        { flow: "refunds", content: good("refunds") },
      ],
      ["checkout", "orders", "refunds"],
    );

    expect(plan.targets.map((t) => t.name)).toEqual(["checkout", "refunds"]);
    expect(plan.skipped.map((s) => s.flow)).toEqual(["orders"]);
  });

  it("plans nothing for a document with no suites", () => {
    expect(planRunAll([], ["orders"])).toEqual({ targets: [], skipped: [] });
  });
});
