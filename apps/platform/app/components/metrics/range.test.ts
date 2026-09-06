import { describe, expect, it } from "vitest";
import { DEFAULT_RANGE, readRange, spanMs, windowFor, writeRange } from "./range";

describe("the range in the URL", () => {
  it("reads a preset", () => {
    expect(readRange(new URLSearchParams("range=24h"))).toBe("24h");
  });

  it("falls back to the default rather than erroring on a bad one", () => {
    // A hand-edited or stale link should show the default view: nothing is wrong
    // with the install, and an error page here would read like an outage.
    expect(readRange(new URLSearchParams("range=nonsense"))).toBe(DEFAULT_RANGE);
    expect(readRange(new URLSearchParams())).toBe(DEFAULT_RANGE);
  });

  it("writes nothing at the default, so a plain view has a plain URL", () => {
    expect(writeRange(DEFAULT_RANGE)).toBe("");
    expect(writeRange("7d")).toBe("range=7d");
  });

  it("round-trips every preset it writes", () => {
    for (const range of ["5m", "30m", "1h", "24h", "7d"] as const) {
      expect(readRange(new URLSearchParams(writeRange(range)))).toBe(range);
    }
  });
});

describe("windowFor", () => {
  it("ends now and reaches back by the preset", () => {
    const now = Date.parse("2026-09-05T12:00:00.000Z");

    expect(windowFor("5m", now)).toEqual({
      from: "2026-09-05T11:55:00.000Z",
      to: "2026-09-05T12:00:00.000Z",
    });
    expect(windowFor("7d", now).from).toBe("2026-08-29T12:00:00.000Z");
  });

  it("reports the same width the chart draws", () => {
    const now = Date.parse("2026-09-05T12:00:00.000Z");
    const window = windowFor("1h", now);
    expect(Date.parse(window.to) - Date.parse(window.from)).toBe(spanMs("1h"));
  });
});
