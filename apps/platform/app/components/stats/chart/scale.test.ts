import { describe, expect, it } from "vitest";
import {
  binaryStep,
  downsample,
  extent,
  niceStep,
  plotExtent,
  ticks,
  timeTicks,
  unionExtent,
  type Reading,
} from "./scale";

describe("extents", () => {
  it("ignores gaps rather than treating them as zero", () => {
    expect(extent([null, 5, null, 9])).toEqual({ min: 5, max: 9 });
  });

  it("is null when nothing was measured", () => {
    expect(extent([])).toBeNull();
    expect(extent([null, null])).toBeNull();
  });

  it("unions what it was given and skips what it was not", () => {
    expect(unionExtent([{ min: 1, max: 3 }, null, { min: 0, max: 2 }]))
      .toEqual({ min: 0, max: 3 });
    expect(unionExtent([null])).toBeNull();
  });
});

describe("plotExtent", () => {
  it("anchors a non-negative series at zero", () => {
    // 118..120 MiB drawn full-height reads as a crisis; from zero it reads flat.
    expect(plotExtent({ min: 118, max: 120 }).min).toBe(0);
  });

  it("keeps a negative floor, since zero is not the bottom there", () => {
    expect(plotExtent({ min: -4, max: 4 }).min).toBe(-4);
  });

  it("leaves headroom above the peak", () => {
    // Flush against the top, a stroke is half outside the clip and the highest
    // reading reads as a line that was cut off.
    const range = plotExtent({ min: 0, max: 100 });
    expect(range.max).toBeGreaterThan(100);
    expect(range.max).toBeLessThan(110);
  });

  it("gives a flat series a height rather than a zero-width domain", () => {
    const flat = plotExtent({ min: 7, max: 7 });
    expect(flat.max).toBeGreaterThan(flat.min);

    const zero = plotExtent({ min: 0, max: 0 });
    expect(zero.max).toBeGreaterThan(zero.min);
  });

  it("keeps a flat negative series the right way up", () => {
    // Scaling -4 by 1.25 puts the maximum below the minimum and reverses the
    // whole axis, which no scale or tick generator recovers from.
    const range = plotExtent({ min: -4, max: -4 });
    expect(range.max).toBeGreaterThan(range.min);
  });

  it("is drawable when there is nothing to draw", () => {
    expect(plotExtent(null)).toEqual({ min: 0, max: 1 });
  });
});

describe("ticks", () => {
  it("lands on 1, 2 or 5 boundaries at or above the ask", () => {
    // Above, not below: rounding down turns a request for four gridlines into
    // eight, and eight labelled rules is a lattice rather than a reference.
    expect(niceStep(0.037)).toBe(0.05);
    expect(niceStep(7)).toBe(10);
    expect(niceStep(23)).toBe(50);
    expect(niceStep(2)).toBe(2);
    expect(niceStep(0)).toBe(1);
  });

  it("keeps close to the tick count it was asked for", () => {
    // The bug this pins: a byte axis over 0..140 MB asked for four ticks and
    // drew eight.
    expect(ticks(0, 1.4e8, 4).length).toBeLessThanOrEqual(5);
  });

  it("steps a byte axis in binary units", () => {
    // A decimal step reads "19 MiB, 38 MiB, 57 MiB", which is arithmetic nobody
    // wants to do while looking at a chart.
    expect(binaryStep(3.5e7)).toBe(64 * 1024 * 1024);
    expect(binaryStep(700)).toBe(1024);
    for (const at of ticks(0, 1.4e8, 4, binaryStep)) {
      expect(at % (1024 * 1024)).toBe(0);
    }
  });

  it("covers the domain without drifting off the boundary", () => {
    const out = ticks(0, 1, 4);
    expect(out[0]).toBe(0);
    expect(out[out.length - 1]).toBeLessThanOrEqual(1);
    // Repeated addition would leave 0.30000000000000004 on the axis.
    for (const tick of out) expect(String(tick).length).toBeLessThan(8);
  });

  it("degrades to a single tick rather than looping", () => {
    expect(ticks(5, 5)).toEqual([5]);
    expect(ticks(Number.NaN, 1)).toHaveLength(1);
  });
});

describe("timeTicks", () => {
  it("aligns to whole multiples so neighbouring views share gridlines", () => {
    const out = timeTicks(1_000_000_000, 1_000_300_000, 4);
    for (const at of out) expect(at % 60_000).toBe(0);
  });

  it("picks a wall-clock step, not a power of ten", () => {
    const out = timeTicks(0, 604_800_000, 4);
    expect(out.length).toBeGreaterThan(1);
    const gap = out[1] - out[0];
    expect([86_400_000, 172_800_000, 604_800_000]).toContain(gap);
  });

  it("handles a window with no width", () => {
    expect(timeTicks(10, 10)).toEqual([10]);
  });
});

describe("downsample", () => {
  it("leaves a column shorter than the budget alone", () => {
    const out = downsample([1, 2, 3], [1, 2, 3], 10);
    expect(out.values).toEqual([1, 2, 3]);
  });

  it("keeps the peak of each bucket rather than averaging it away", () => {
    const times = Array.from({ length: 100 }, (_, i) => i);
    const values: Reading[] = times.map(() => 1);
    values[42] = 99; // the only reason anyone glances at a sparkline

    const out = downsample(times, values, 10);
    expect(out.values).toHaveLength(10);
    expect(out.values).toContain(99);
  });

  it("keeps a bucket with no reading a gap", () => {
    const out = downsample([0, 1, 2, 3], [null, null, 5, 6], 2);
    expect(out.values).toEqual([null, 6]);
  });

  it("reports the peak at its own moment, not the bucket's start", () => {
    const times = Array.from({ length: 100 }, (_, i) => i * 1000);
    const values: Reading[] = times.map(() => 1);
    values[42] = 99;

    const out = downsample(times, values, 10);
    const at = out.times[out.values.indexOf(99)];
    expect(at).toBe(42_000);
  });

  it("keeps the columns parallel", () => {
    const times = Array.from({ length: 50 }, (_, i) => i * 1000);
    const out = downsample(times, times.map(() => 1), 7);
    expect(out.times).toHaveLength(out.values.length);
    expect(out.times[0]).toBe(0);
  });
});
