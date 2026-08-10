import { describe, expect, it } from "vitest";

import { relativeAge } from "./relativeAge";

// A fixed "now" so the boundaries can be asserted exactly rather than
// approximately — this function is all boundaries.
const NOW = Date.parse("2026-08-09T12:00:00Z");
const ago = (seconds: number) => new Date(NOW - seconds * 1000).toISOString();

describe("relativeAge", () => {
  it("picks the largest unit that fits", () => {
    expect(relativeAge(ago(0), NOW)).toBe("0s");
    expect(relativeAge(ago(59), NOW)).toBe("59s");
    expect(relativeAge(ago(60), NOW)).toBe("1m");
    expect(relativeAge(ago(3599), NOW)).toBe("59m");
    expect(relativeAge(ago(3600), NOW)).toBe("1h");
    expect(relativeAge(ago(86_399), NOW)).toBe("23h");
    expect(relativeAge(ago(86_400), NOW)).toBe("1d");
    expect(relativeAge(ago(86_400 * 400), NOW)).toBe("400d");
  });

  // Null rather than "0s" or "—": these are not ages, and the caller decides how
  // to render the absence.
  it("reports nothing for what is not an age", () => {
    expect(relativeAge(undefined, NOW)).toBeNull();
    expect(relativeAge("", NOW)).toBeNull();
    expect(relativeAge("not a timestamp", NOW)).toBeNull();
    // A clock skewed forward is the ordinary cause; "-3s ago" helps nobody.
    expect(relativeAge(ago(-30), NOW)).toBeNull();
  });
});
