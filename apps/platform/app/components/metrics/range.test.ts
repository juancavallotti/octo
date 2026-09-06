import { describe, expect, it } from "vitest";
import { DEFAULT_VIEW, VIEWS, readView, viewPreset, windowFor, writeView } from "./range";

/**
 * The views exist to name the tiers, so the test that matters is that each one
 * still does. A view whose tier drifted to "auto" would put back exactly the
 * failure this design removes: a window slightly longer than the live tier
 * reaches, answered entirely from buckets, without saying so.
 */
describe("the views are the tiers", () => {
  it("names one tier each, and never auto", () => {
    expect(viewPreset("hourly").tier).toBe("live");
    expect(viewPreset("weekly").tier).toBe("rollup");
    for (const preset of VIEWS) expect(preset.tier).not.toBe("auto");
  });

  it("offers exactly the two the storage holds", () => {
    expect(VIEWS.map((v) => v.key)).toEqual(["hourly", "weekly"]);
  });

  it("polls the live tier faster than the history one", () => {
    // Live samples every second; a bucket is written once per rollup interval.
    expect(viewPreset("hourly").refreshMs).toBeLessThan(viewPreset("weekly").refreshMs);
  });
});

describe("the view in the URL", () => {
  it("reads a view", () => {
    expect(readView(new URLSearchParams("view=weekly"))).toBe("weekly");
  });

  it("falls back to the default rather than erroring on a bad one", () => {
    // A hand-edited or stale link should show the default view: nothing is wrong
    // with the install, and an error page here would read like an outage.
    expect(readView(new URLSearchParams("view=nonsense"))).toBe(DEFAULT_VIEW);
    expect(readView(new URLSearchParams())).toBe(DEFAULT_VIEW);
    // The ladder of windows this replaced.
    expect(readView(new URLSearchParams("view=30m"))).toBe(DEFAULT_VIEW);
  });

  it("writes nothing at the default, so a plain view has a plain URL", () => {
    expect(writeView(DEFAULT_VIEW)).toBe("");
    expect(writeView("weekly")).toBe("view=weekly");
  });

  it("round-trips every view it writes", () => {
    for (const preset of VIEWS) {
      expect(readView(new URLSearchParams(writeView(preset.key)))).toBe(preset.key);
    }
  });
});

describe("windowFor", () => {
  it("ends now and reaches back by the view's span", () => {
    const now = Date.parse("2026-09-05T12:00:00.000Z");
    expect(windowFor("hourly", now)).toEqual({
      from: "2026-09-05T11:00:00.000Z",
      to: "2026-09-05T12:00:00.000Z",
    });
    expect(windowFor("weekly", now).from).toBe("2026-08-29T12:00:00.000Z");
  });

  it("asks for the tier's reach at the shipped defaults", () => {
    // Generous on purpose: a tier holding less returns less, and the chart draws
    // what came back. Asking for less would hide data on a longer-configured
    // install.
    expect(viewPreset("hourly").spanMs).toBe(60 * 60_000);
    expect(viewPreset("weekly").spanMs).toBe(7 * 24 * 60 * 60_000);
  });
});
