import { describe, expect, it } from "vitest";
import {
  compareInstants,
  contains,
  minusNs,
  mergeIntervals,
  nsBetween,
  overlaps,
  packLanes,
  parseInstant,
  unionLength,
} from "./timeSpans";

describe("parseInstant", () => {
  it("keeps the digits Date.parse throws away", () => {
    // The whole reason this is not just Date.parse: a transform block runs in
    // tens of microseconds, so a millisecond is larger than whole blocks here.
    const at = parseInstant("2026-08-09T10:00:00.123456789Z");
    expect(at).toEqual({ ms: Date.parse("2026-08-09T10:00:00.123Z"), ns: 456789 });
  });

  it("reads a timestamp with microsecond precision, which is what Postgres gives", () => {
    expect(parseInstant("2026-08-09T10:00:00.123456Z")?.ns).toBe(456000);
  });

  it("reads one with no fraction at all", () => {
    expect(parseInstant("2026-08-09T10:00:00Z")?.ns).toBe(0);
  });

  it("reads one with fewer digits than a millisecond", () => {
    const at = parseInstant("2026-08-09T10:00:00.5Z");
    expect(at).toEqual({ ms: Date.parse("2026-08-09T10:00:00.500Z"), ns: 0 });
  });

  it("refuses a string that is not a timestamp", () => {
    expect(parseInstant("not a time")).toBeNull();
    expect(parseInstant("")).toBeNull();
  });
});

describe("instant arithmetic", () => {
  const base = { ms: 1_000_000, ns: 500_000 };

  it("measures a distance that spans milliseconds exactly", () => {
    const later = { ms: 1_000_003, ns: 250_000 };
    expect(nsBetween(base, later)).toBe(3_000_000 - 250_000);
  });

  it("stays exact for a sub-microsecond distance", () => {
    // 40 nanoseconds. Anything that routed this through a float millisecond
    // would return 0 and collapse the two instants onto each other.
    expect(nsBetween(base, { ms: 1_000_000, ns: 500_040 })).toBe(40);
  });

  it("borrows across the millisecond when stepping back", () => {
    // The span of a 2ms block that finished 0.5ms into a millisecond.
    expect(minusNs(base, 2_000_000)).toEqual({ ms: 999_998, ns: 500_000 });
    expect(minusNs(base, 600_000)).toEqual({ ms: 999_999, ns: 900_000 });
  });

  it("orders by the sub-millisecond half when the milliseconds tie", () => {
    expect(compareInstants(base, { ms: 1_000_000, ns: 500_001 })).toBeLessThan(0);
    expect(compareInstants(base, base)).toBe(0);
  });
});

describe("interval predicates", () => {
  it("counts touching bounds as enclosed", () => {
    // A block that starts and ends with the composite around it is inside it.
    expect(contains({ start: 0, end: 10 }, { start: 0, end: 10 })).toBe(true);
    expect(contains({ start: 0, end: 10 }, { start: 2, end: 8 })).toBe(true);
    expect(contains({ start: 2, end: 8 }, { start: 0, end: 10 })).toBe(false);
  });

  it("does not call touching spans concurrent", () => {
    // One ending exactly as the next begins is a sequence, not a fork.
    expect(overlaps({ start: 0, end: 10 }, { start: 10, end: 20 })).toBe(false);
    expect(overlaps({ start: 0, end: 11 }, { start: 10, end: 20 })).toBe(true);
  });
});

describe("unionLength", () => {
  it("counts wall-clock time rather than adding durations up", () => {
    // Four branches of a fork, each 100ns, all running together. The trace spent
    // 100ns on them; summing would claim 400 and let one branch account for more
    // time than the whole trace lasted.
    const branches = Array.from({ length: 4 }, () => ({ start: 0, end: 100 }));
    expect(unionLength(branches)).toBe(100);
  });

  it("adds up spans that did not overlap", () => {
    expect(
      unionLength([
        { start: 0, end: 10 },
        { start: 20, end: 25 },
      ]),
    ).toBe(15);
  });

  it("joins spans that merely touch", () => {
    expect(
      unionLength([
        { start: 10, end: 20 },
        { start: 0, end: 10 },
      ]),
    ).toBe(20);
  });

  it("is zero for nothing at all", () => {
    expect(unionLength([])).toBe(0);
  });

  it("does not disturb its input", () => {
    const spans = [
      { start: 20, end: 30 },
      { start: 0, end: 25 },
    ];
    mergeIntervals(spans);
    expect(spans[0]).toEqual({ start: 20, end: 30 });
  });
});

describe("packLanes", () => {
  it("leaves a sequence on one lane", () => {
    expect(
      packLanes([
        { start: 0, end: 10 },
        { start: 10, end: 20 },
        { start: 20, end: 30 },
      ]),
    ).toEqual([0, 0, 0]);
  });

  it("gives concurrent spans their own lanes", () => {
    expect(
      packLanes([
        { start: 0, end: 100 },
        { start: 10, end: 90 },
        { start: 20, end: 80 },
      ]),
    ).toEqual([0, 1, 2]);
  });

  it("reuses a lane once it is free", () => {
    // Two overlap, then a third starts after the first finished.
    expect(
      packLanes([
        { start: 0, end: 50 },
        { start: 10, end: 100 },
        { start: 60, end: 70 },
      ]),
    ).toEqual([0, 1, 0]);
  });

  it("answers in the order it was asked, not in time order", () => {
    expect(
      packLanes([
        { start: 60, end: 70 },
        { start: 0, end: 100 },
      ]),
    ).toEqual([1, 0]);
  });
});
