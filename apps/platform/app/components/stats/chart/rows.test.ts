import { describe, expect, it } from "vitest";
import { toRows } from "./rows";

describe("toRows", () => {
  it("merges columns onto shared moments", () => {
    const rows = toRows([
      { key: "cpu", times: [1, 2], values: [0.1, 0.2] },
      { key: "mem", times: [1, 2], values: [10, 20] },
    ]);
    expect(rows).toEqual([
      { t: 1, cpu: 0.1, mem: 10 },
      { t: 2, cpu: 0.2, mem: 20 },
    ]);
  });

  it("unions moments the two columns do not share", () => {
    // Pods scrape independently, so their timestamps do not line up.
    const rows = toRows([
      { key: "a", times: [1, 3], values: [1, 3] },
      { key: "b", times: [2], values: [2] },
    ]);
    expect(rows.map((r) => r.t)).toEqual([1, 2, 3]);
    expect(rows[1]).toEqual({ t: 2, b: 2 });
  });

  it("leaves a gap absent rather than zero", () => {
    // The single outcome that has to be impossible: a scrape that did not
    // happen drawn as a measurement of nothing.
    const rows = toRows([{ key: "a", times: [1, 2, 3], values: [1, null, 3] }]);
    expect(rows[1].a).toBeUndefined();
    expect(rows[1].a).not.toBe(0);
  });

  it("sorts by time whatever order the columns arrived in", () => {
    const rows = toRows([{ key: "a", times: [30, 10, 20], values: [3, 1, 2] }]);
    expect(rows.map((r) => r.t)).toEqual([10, 20, 30]);
  });

  it("has nothing to say about no columns", () => {
    expect(toRows([])).toEqual([]);
  });

  it("stops at the shorter of two ragged arrays", () => {
    expect(toRows([{ key: "a", times: [1, 2, 3], values: [1, 2] }])).toHaveLength(2);
  });
});
