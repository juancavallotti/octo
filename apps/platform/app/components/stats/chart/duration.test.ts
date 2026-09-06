import { describe, expect, it } from "vitest";
import { formatStep, parseGoDuration } from "./duration";

/**
 * Go duration strings arrive from the service and become a divisor. A misread one
 * does not fail loudly — it silently scales a chart, which is why "not a duration"
 * has to be distinguishable from "zero".
 */
describe("parseGoDuration", () => {
  it("reads the forms Duration.String() emits", () => {
    expect(parseGoDuration("1s")).toBe(1000);
    expect(parseGoDuration("1h0m0s")).toBe(3_600_000);
    expect(parseGoDuration("1m30s")).toBe(90_000);
    expect(parseGoDuration("500ms")).toBe(500);
    expect(parseGoDuration("168h0m0s")).toBe(604_800_000);
    expect(parseGoDuration("1.5s")).toBe(1500);
    expect(parseGoDuration("0s")).toBe(0);
  });

  it("does not read ms as minutes", () => {
    expect(parseGoDuration("500ms")).not.toBe(30_000_000);
  });

  it("reads the sub-second units, including both micro signs", () => {
    expect(parseGoDuration("1500ns")).toBeCloseTo(0.0015, 9);
    expect(parseGoDuration("250us")).toBe(0.25);
    expect(parseGoDuration("250µs")).toBe(0.25);
  });

  it("returns null for anything that is not a duration", () => {
    expect(parseGoDuration("")).toBeNull();
    expect(parseGoDuration("soon")).toBeNull();
    expect(parseGoDuration("1")).toBeNull();
    expect(parseGoDuration("1x")).toBeNull();
  });

  it("refuses a string that only starts like a duration", () => {
    // The trailing text is the tell: a partial parse would report one second.
    expect(parseGoDuration("1s and a bit")).toBeNull();
  });
});

describe("formatStep", () => {
  it("says a duration as short as it can", () => {
    expect(formatStep(1000)).toBe("1s");
    expect(formatStep(90_000)).toBe("1.5m");
    expect(formatStep(3_600_000)).toBe("1h");
    expect(formatStep(604_800_000)).toBe("7d");
    expect(formatStep(250)).toBe("250ms");
  });

  it("marks a step it cannot state rather than printing a zero", () => {
    expect(formatStep(0)).toBe("—");
    expect(formatStep(Number.NaN)).toBe("—");
  });
});
