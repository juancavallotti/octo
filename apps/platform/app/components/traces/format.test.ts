import { describe, expect, it } from "vitest";
import {
  describeCost,
  describeCostStatus,
  formatAge,
  formatCost,
  formatDuration,
  formatTokens,
  formatWindow,
} from "./format";

describe("formatDuration", () => {
  it("keeps a fast block visible instead of rounding it to nothing", () => {
    // A set-variable runs in tens of nanoseconds; a millisecond-only formatter
    // would print "0ms" for every one of them.
    expect(formatDuration(40)).toBe("40ns");
    expect(formatDuration(40_000)).toBe("40µs");
    expect(formatDuration(1_500_000)).toBe("1.5ms");
  });

  it("scales up to a model call and past a minute", () => {
    expect(formatDuration(2_400_000_000)).toBe("2.4s");
    expect(formatDuration(95_000_000_000)).toBe("1m 35s");
  });

  // Rounding can carry a figure across the boundary that chose its unit, and the
  // unit has to be chosen from the figure that will actually be shown. Before
  // this, 999,999ns rendered as "1000µs" — a quantity with a shorter name.
  it("promotes a figure that rounds up into the next unit", () => {
    expect(formatDuration(999_499)).toBe("999µs");
    expect(formatDuration(999_999)).toBe("1ms");
    expect(formatDuration(999_999_999)).toBe("1s");
    // Seconds roll over at 60, not at 1000, so this is the same bug on a
    // non-decimal boundary: it used to read "60s".
    expect(formatDuration(59_999_999_999)).toBe("1m 0s");
  });

  it("says nothing rather than something wrong for a non-duration", () => {
    expect(formatDuration(-1)).toBe("—");
    expect(formatDuration(Number.NaN)).toBe("—");
  });
});

describe("formatTokens", () => {
  it("groups a four-figure count rather than abbreviating it", () => {
    // The comparison a reader makes is between two counts, and at four figures
    // the digits are still the fastest way to make it.
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(842)).toBe("842");
    expect(formatTokens(1_800)).toBe("1,800");
    expect(formatTokens(9_999)).toBe("9,999");
  });

  it("abbreviates once the tail stops carrying meaning", () => {
    expect(formatTokens(10_000)).toBe("10k");
    expect(formatTokens(128_400)).toBe("128k");
    expect(formatTokens(2_450_000)).toBe("2.45M");
  });

  it("promotes a figure that rounds up into the next unit", () => {
    expect(formatTokens(999_499)).toBe("999k");
    // 999.5 thousands rounds to 1000, and "1000k" names a quantity the reader
    // already has a shorter name for.
    expect(formatTokens(999_500)).toBe("1M");
    expect(formatTokens(999_999)).toBe("1M");
    expect(formatTokens(1_000_000)).toBe("1M");
    expect(formatTokens(999_999_999)).toBe("1B");
  });

  it("says nothing rather than something wrong for a non-count", () => {
    expect(formatTokens(-1)).toBe("—");
    expect(formatTokens(Number.NaN)).toBe("—");
  });
});

describe("formatCost", () => {
  it("never prints a real charge as zero", () => {
    // One token of a cheap model is $7.5e-8. At two decimals it reads "$0.00",
    // which is the exact claim the whole cost path avoids making.
    expect(formatCost(0.000000075)).toBe("<$0.0001");
    expect(formatCost(0.00042)).toBe("$0.0004");
  });

  it("prints an actual zero as zero", () => {
    expect(formatCost(0)).toBe("$0");
  });

  it("uses cents once there are cents", () => {
    expect(formatCost(1.239)).toBe("$1.24");
    expect(formatCost(0.5)).toBe("$0.5000");
  });

  // The same rule the units follow: which side of a dollar the figure falls on
  // is decided after rounding. It used to read "$1.0000".
  it("drops to cents for a fraction that rounds up to a dollar", () => {
    expect(formatCost(0.99999)).toBe("$1.00");
    expect(formatCost(0.99994)).toBe("$0.9999");
  });
});

describe("describeCost", () => {
  it("reports a fully priced total as the total", () => {
    expect(describeCost(0.42, 0, true)).toMatchObject({ text: "$0.4200", partial: false });
  });

  it("marks a total with unpriced calls as a lower bound", () => {
    const described = describeCost(0.42, 2, true);
    expect(described.text).toBe("≥ $0.4200");
    expect(described.partial).toBe(true);
    expect(described.title).toContain("2 calls");
  });

  it("refuses to call an entirely unpriced trace free", () => {
    // The sum really is 0 — nothing could be priced. Rendering it as "$0" would
    // say the model calls cost nothing rather than that nobody could say.
    const described = describeCost(0, 1, true);
    expect(described.text).toBe("unpriced");
    expect(described.partial).toBe(true);
  });

  it("shows nothing at all when there were no model calls", () => {
    // Distinct from "$0": a trace with no model calls has no cost to report,
    // and a dollar figure beside it invites the reader to compare the two.
    expect(describeCost(0, 0, false)).toMatchObject({ text: "—", partial: false });
  });
});

describe("formatAge", () => {
  const now = Date.parse("2026-08-09T12:00:00Z");

  it("scales from seconds to days", () => {
    expect(formatAge("2026-08-09T11:59:30Z", now)).toBe("30s ago");
    expect(formatAge("2026-08-09T11:30:00Z", now)).toBe("30m ago");
    expect(formatAge("2026-08-09T06:00:00Z", now)).toBe("6h ago");
    expect(formatAge("2026-08-07T12:00:00Z", now)).toBe("2d ago");
  });

  it("does not report a clock skew as the future", () => {
    expect(formatAge("2026-08-09T12:00:05Z", now)).toBe("just now");
  });

  it("says nothing for an unreadable timestamp", () => {
    expect(formatAge("nope", now)).toBe("—");
  });
});

describe("formatWindow", () => {
  it("describes the span the counts were measured over", () => {
    expect(formatWindow("2026-08-08T12:00:00Z", "2026-08-09T12:00:00Z")).toBe("last 24 hours");
    expect(formatWindow("2026-08-09T11:00:00Z", "2026-08-09T12:00:00Z")).toBe("last 1 hour");
    expect(formatWindow("2026-08-02T12:00:00Z", "2026-08-09T12:00:00Z")).toBe("last 7 days");
  });

  it("says nothing for a window that is not one", () => {
    expect(formatWindow("2026-08-09T12:00:00Z", "2026-08-09T12:00:00Z")).toBe("");
    expect(formatWindow("nope", "also nope")).toBe("");
  });
});

describe("describeCostStatus", () => {
  it("says a reported cost is the charge rather than an estimate", () => {
    // The one status where the number is more certain than `priced`, not less.
    expect(describeCostStatus("reported")).toMatch(/not an estimate from a rate card/);
  });

  it("never describes an unpriced call as free", () => {
    expect(describeCostStatus("unpriced_model")).toMatch(/not the same as it being free/);
  });

  it("says nothing for a record that is not a model call", () => {
    expect(describeCostStatus("")).toBe("");
  });
});
