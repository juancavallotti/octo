import { describe, it, expect } from "vitest";
import { toMockSpecs } from "./debug";
import type { BlockMock } from "../meta/types";

/** A mock on one address, with whatever cases the test cares about. */
function mock(over: Partial<BlockMock> = {}): BlockMock {
  return { address: "orders.charge", enabled: true, cases: [], ...over };
}

describe("toMockSpecs", () => {
  // The one real conversion: the meta file holds JSON text, the runner wants values.
  it("parses a case's body and vars from JSON text into values", () => {
    const specs = toMockSpecs([
      mock({
        cases: [
          {
            when: "body.amount > 100",
            body: '{"charged": true}',
            vars: '{"status": "402"}',
          },
        ],
      }),
    ]);

    expect(specs).toEqual({
      "orders.charge": {
        cases: [
          {
            when: "body.amount > 100",
            body: { charged: true },
            vars: { status: "402" },
          },
        ],
      },
    });
  });

  it("passes an error case and a drop case through untouched", () => {
    const specs = toMockSpecs([
      mock({
        cases: [
          { when: "body.amount > 100", error: "card declined" },
          { when: "true", drop: true },
        ],
      }),
    ]);

    expect(specs["orders.charge"].cases).toEqual([
      { when: "body.amount > 100", error: "card declined" },
      { when: "true", drop: true },
    ]);
  });

  // The runtime rejects a default carrying a condition: it is what runs when nothing
  // matched, so a condition on it would be a contradiction.
  it("strips the condition from a default", () => {
    const specs = toMockSpecs([
      mock({ default: { when: "leftover", body: '{"ok": true}' } }),
    ]);

    expect(specs["orders.charge"].default).toEqual({ body: { ok: true } });
    expect(specs["orders.charge"].default).not.toHaveProperty("when");
  });

  /**
   * A half-written case is normal, not exceptional — the meta file is hand-editable and a
   * mock is built a field at a time. The runtime validates the whole set at the edge and
   * fails the RUN if any case is malformed, so passing one through would leave a user with
   * one broken case unable to exercise their flow at all.
   */
  describe("dropping what the runner would reject", () => {
    it("drops a case that sets more than one outcome", () => {
      const specs = toMockSpecs([
        mock({
          cases: [
            { when: "a", body: "{}", error: "boom" }, // two outcomes: not a case
            { when: "b", error: "boom" },
          ],
        }),
      ]);

      expect(specs["orders.charge"].cases).toEqual([{ when: "b", error: "boom" }]);
    });

    it("drops a case that sets no outcome at all", () => {
      const specs = toMockSpecs([
        mock({ cases: [{ when: "a" }, { when: "b", drop: true }] }),
      ]);

      expect(specs["orders.charge"].cases).toEqual([{ when: "b", drop: true }]);
    });

    it("drops a case whose vars ride along with no body", () => {
      const specs = toMockSpecs([mock({ cases: [{ when: "a", error: "x", vars: "{}" }] })]);
      expect(specs).toEqual({});
    });

    // Treating an unparseable body as absent would turn a body case into an outcome-less
    // one, and the block would stand in for nothing rather than return what was written.
    it("drops a case whose body is not JSON", () => {
      const specs = toMockSpecs([mock({ cases: [{ when: "a", body: "{not json" }] })]);
      expect(specs).toEqual({});
    });
  });

  /**
   * An empty spec is not the same as no spec: the runtime rejects one that says nothing
   * ("needs at least one case, or a default"), so sending it would fail the run — where
   * leaving it out simply does not mock the block, which is what an empty mock means.
   */
  it("leaves out a mock with nothing usable left in it", () => {
    expect(toMockSpecs([mock({ cases: [] })])).toEqual({});
    expect(toMockSpecs([mock({ cases: [{ when: "a" }] })])).toEqual({});
  });

  it("keeps a mock that has only a default", () => {
    const specs = toMockSpecs([mock({ default: { drop: true } })]);
    expect(specs).toEqual({ "orders.charge": { default: { drop: true } } });
  });

  it("keys every mock by its address", () => {
    const specs = toMockSpecs([
      mock({ address: "orders.charge", default: { drop: true } }),
      mock({ address: "orders.notify", default: { drop: true } }),
    ]);
    expect(Object.keys(specs)).toEqual(["orders.charge", "orders.notify"]);
  });
});
