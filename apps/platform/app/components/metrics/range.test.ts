import { describe, expect, it } from "vitest";
import type { StatsPod } from "@/app/model/stats";
import {
  DEFAULT_VIEW,
  VIEWS,
  readView,
  reachFor,
  viewPreset,
  windowFor,
  writeView,
} from "./range";

/**
 * The views exist to name the tiers, so the test that matters is that each one
 * still does. A view whose tier drifted to "auto" would put back exactly the
 * failure this design removes: a window slightly longer than the live tier
 * reaches, answered entirely from buckets, without saying so.
 */
describe("the views are the tiers", () => {
  it("names one tier each, and never auto", () => {
    expect(viewPreset("live").tier).toBe("live");
    expect(viewPreset("historic").tier).toBe("rollup");
    for (const preset of VIEWS) expect(preset.tier).not.toBe("auto");
  });

  it("offers exactly the two the storage holds", () => {
    expect(VIEWS.map((v) => v.key)).toEqual(["live", "historic"]);
  });

  it("names them after tiers rather than durations", () => {
    // Every duration here is a per-install setting, so a view called "Hourly"
    // would be a lie on an install configured for anything else.
    expect(VIEWS.map((v) => v.label)).toEqual(["Live", "Historic"]);
  });

  it("polls the live tier faster than the history one", () => {
    expect(viewPreset("live").refreshMs).toBeLessThan(viewPreset("historic").refreshMs);
  });
});

function pod(over: Partial<StatsPod> = {}): StatsPod {
  return {
    pod: "pod-a",
    lastSeen: "2026-09-05T12:00:00Z",
    reporting: true,
    startedAt: null,
    sampleInterval: "1s",
    rollupInterval: "15m0s",
    retention: "168h0m0s",
    generation: 1,
    series: 95,
    liveRows: 900,
    rollupRows: 672,
    ...over,
  };
}

describe("reachFor", () => {
  it("deduces the history reach from the configured retention", () => {
    // Not seven days: retention is a setting, and an install keeping thirty
    // would have three weeks of stored history the page never asked for.
    const month = reachFor("historic", [pod({ retention: "720h0m0s" })]);
    expect(month).toBeGreaterThan(29 * 24 * 3_600_000);

    const day = reachFor("historic", [pod({ retention: "24h0m0s" })]);
    expect(day).toBeLessThan(2 * 24 * 3_600_000);
  });

  it("deduces the live reach from the bucket width", () => {
    // The live tier holds one bucket's worth of samples, so the bucket is its
    // reach. Read here, decided in the sidecar.
    expect(reachFor("live", [pod({ rollupInterval: "15m0s" })])).toBeGreaterThan(15 * 60_000);
    expect(reachFor("live", [pod({ rollupInterval: "15m0s" })])).toBeLessThan(20 * 60_000);
  });

  it("takes the widest pod, since one may still be on an older config", () => {
    const reach = reachFor("historic", [
      pod({ retention: "24h0m0s" }),
      pod({ retention: "168h0m0s" }),
    ]);
    expect(reach).toBeGreaterThan(167 * 3_600_000);
  });

  it("leaves headroom so the oldest row is not clipped by its own boundary", () => {
    expect(reachFor("historic", [pod({ retention: "24h0m0s" })])).toBeGreaterThan(
      24 * 3_600_000,
    );
  });

  it("falls back to the view's guess when nothing has reported", () => {
    expect(reachFor("live", [])).toBe(viewPreset("live").askMs);
  });

  it("ignores a duration it cannot read rather than reaching back to zero", () => {
    expect(reachFor("historic", [pod({ retention: "" })])).toBe(
      viewPreset("historic").askMs,
    );
  });
});

describe("the view in the URL", () => {
  it("reads a view", () => {
    expect(readView(new URLSearchParams("view=historic"))).toBe("historic");
  });

  it("falls back to the default rather than erroring on a bad one", () => {
    expect(readView(new URLSearchParams("view=nonsense"))).toBe(DEFAULT_VIEW);
    expect(readView(new URLSearchParams())).toBe(DEFAULT_VIEW);
    // The ladder of windows this replaced.
    expect(readView(new URLSearchParams("view=30m"))).toBe(DEFAULT_VIEW);
  });

  it("writes nothing at the default, so a plain view has a plain URL", () => {
    expect(writeView(DEFAULT_VIEW)).toBe("");
    expect(writeView("historic")).toBe("view=historic");
  });

  it("round-trips every view it writes", () => {
    for (const preset of VIEWS) {
      expect(readView(new URLSearchParams(writeView(preset.key)))).toBe(preset.key);
    }
  });
});

describe("windowFor", () => {
  it("reaches back by the span it was given", () => {
    const now = Date.parse("2026-09-05T12:00:00.000Z");
    expect(windowFor("live", now, 3_600_000)).toEqual({
      from: "2026-09-05T11:00:00.000Z",
      to: "2026-09-05T12:00:00.000Z",
    });
  });
});
