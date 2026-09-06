import { describe, expect, it } from "vitest";
import type { StatsSeries } from "@/app/model/stats";
import { formatCores, latest, toCores, toGauge } from "./metrics";

/**
 * Turning a counter's growth into cores is the one number on these charts that is
 * derived rather than read, so it is the one that can be wrong without looking
 * wrong. Every case here is about the divisor.
 */

/** A series with only what the test cares about set. */
function series(over: Partial<StatsSeries>): StatsSeries {
  return {
    pod: "pod-a",
    name: "process_cpu_seconds_total",
    kind: "counter",
    labels: {},
    times: [],
    ends: [],
    values: [],
    min: [],
    max: [],
    last: [],
    samples: [],
    ...over,
  };
}

describe("toCores", () => {
  it("divides live growth by the gap to the previous point", () => {
    // Half a CPU-second per second is half a core.
    const out = toCores(
      series({ times: [1000, 2000, 3000], values: [0.5, 0.5, 0.25] }),
      1000,
    );
    expect(out.values).toEqual([0.5, 0.5, 0.25]);
  });

  it("uses the real gap when a scrape was missed, not the nominal step", () => {
    // Ten seconds of wall clock, one CPU-second: a tenth of a core, not one core.
    const out = toCores(series({ times: [1000, 11_000], values: [1, 1] }), 1000);
    expect(out.values[1]).toBeCloseTo(0.1, 9);
  });

  it("uses the bucket's own width on the rollup tier", () => {
    // Buckets are not contiguous, which is why ends is carried at all: the second
    // bucket is 60s wide even though 300s elapsed since the first one started.
    const out = toCores(
      series({
        times: [0, 300_000],
        ends: [60_000, 360_000],
        values: [30, 6],
      }),
      3_600_000,
    );
    expect(out.values[0]).toBeCloseTo(0.5, 9);
    expect(out.values[1]).toBeCloseTo(0.1, 9);
  });

  it("falls back to the step for the first point, which is real data", () => {
    // The service seeds the first delta from a row before the window, so this
    // point is a measurement rather than a zero to be dropped.
    const out = toCores(series({ times: [1000], values: [2] }), 1000);
    expect(out.values).toEqual([2]);
  });

  it("keeps a gap a gap", () => {
    const out = toCores(
      series({ times: [1000, 2000, 3000], values: [1, null, 1] }),
      1000,
    );
    expect(out.values[1]).toBeNull();
    expect(out.values[1]).not.toBe(0);
  });

  it("does not correct a reset the service already handled", () => {
    // A restart is reported as the new reading, not a negative delta. Clamping it
    // again here would erase the first second of the new process.
    const out = toCores(series({ times: [1000, 2000], values: [5, 0.2] }), 1000);
    expect(out.values[1]).toBeCloseTo(0.2, 9);
  });

  it("yields a gap rather than an infinity when the interval is not positive", () => {
    const out = toCores(series({ times: [1000, 1000], values: [1, 1] }), 0);
    expect(out.values.every((v) => v === null || Number.isFinite(v))).toBe(true);
    expect(out.values[1]).toBeNull();
  });

  it("keeps the columns parallel when they arrive ragged", () => {
    const out = toCores(series({ times: [1, 2, 3], values: [1, 2] }), 1000);
    expect(out.times).toHaveLength(2);
    expect(out.values).toHaveLength(2);
  });
});

describe("toGauge", () => {
  it("reports what was stored", () => {
    const out = toGauge(series({ times: [1, 2], values: [100, null] }));
    expect(out.values).toEqual([100, null]);
  });
});

describe("latest", () => {
  it("skips trailing gaps to find the last real reading", () => {
    expect(latest({ times: [1, 2, 3], values: [4, 5, null] })).toBe(5);
  });

  it("is null when nothing was measured", () => {
    expect(latest({ times: [1], values: [null] })).toBeNull();
  });
});

describe("formatCores", () => {
  it("keeps an idle pod from reading as exactly zero", () => {
    expect(formatCores(0.0004)).toBe("0.0004");
    expect(formatCores(0.0004)).not.toBe("0.00");
  });

  it("shortens as the number grows", () => {
    expect(formatCores(1.234)).toBe("1.23");
    expect(formatCores(0.0523)).toBe("0.052");
  });

  it("marks a gap rather than printing one", () => {
    expect(formatCores(null)).toBe("—");
  });
});
