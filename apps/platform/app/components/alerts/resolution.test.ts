import { describe as group, expect, it } from "vitest";
import {
  bucketsFor,
  describe as say,
  rescale,
  secondsFor,
  stepFor,
  withCurrent,
  WINDOWS,
} from "./resolution";
import type { AlertCondition } from "@/app/model/alerts";

group("stepFor", () => {
  // Buckets no wider than the gap between checks, so every check has a new one.
  it("narrows the bucket when the watch is checked more often", () => {
    expect(stepFor(30)).toBe(30);
    expect(stepFor(60)).toBe(60);
    expect(stepFor(3600)).toBe(60);
  });
});

group("durations and buckets", () => {
  it("round-trips a span through a bucket count", () => {
    expect(bucketsFor(300, 60)).toBe(5);
    expect(secondsFor(5, 60)).toBe(300);
    expect(bucketsFor(300, 30)).toBe(10);
    expect(secondsFor(10, 30)).toBe(300);
  });

  // A window of no buckets is not a window, whatever was asked for.
  it("never produces fewer than one bucket", () => {
    expect(bucketsFor(5, 60)).toBe(1);
    expect(bucketsFor(0, 60)).toBe(1);
  });
});

group("rescale", () => {
  function condition(params: Record<string, unknown>): AlertCondition {
    return {
      id: "c_1",
      type: "spike",
      source: "traces",
      metric: "traces",
      params,
    };
  }

  // Changing how often a watch is checked changes the bucket width. Left alone,
  // the same count would silently mean half the span — and nothing on the form
  // would appear to have moved, because the form shows durations.
  it("keeps every window covering the span it already covered", () => {
    const before = [
      condition({ windowBuckets: 5, baselineBuckets: 30, direction: "up" }),
    ];
    const after = rescale(before, 60, 30);

    expect(after[0].params).toEqual({
      windowBuckets: 10,
      baselineBuckets: 60,
      direction: "up",
    });
  });

  it("leaves everything alone when the width has not moved", () => {
    const before = [condition({ windowBuckets: 5 })];
    expect(rescale(before, 60, 60)).toBe(before);
  });

  // Only the parameters that are counts of buckets. A threshold is a threshold
  // whatever the resolution.
  it("does not touch a parameter that is not a span", () => {
    const before = [condition({ threshold: 0.05, op: "gt", windowBuckets: 2 })];
    const after = rescale(before, 60, 30);
    expect(after[0].params?.threshold).toBe(0.05);
    expect(after[0].params?.op).toBe("gt");
    expect(after[0].params?.windowBuckets).toBe(4);
  });
});

group("withCurrent", () => {
  // A watch written over the API can hold a value no preset offers, and a select
  // that dropped it would move it the next time anything at all was saved.
  it("keeps a value the presets do not offer, in order", () => {
    const options = withCurrent(WINDOWS, 90);
    expect(options.map((o) => o.seconds)).toContain(90);
    expect(options.map((o) => o.seconds)).toEqual(
      [...options.map((o) => o.seconds)].sort((a, b) => a - b),
    );
  });

  it("adds nothing when the value is already offered", () => {
    expect(withCurrent(WINDOWS, 300)).toBe(WINDOWS);
  });
});

group("describe", () => {
  it("says a duration the plainest way it has", () => {
    expect(say(30)).toBe("30 seconds");
    // Under two minutes it stays in seconds: "1.5 minutes" is the same number
    // said worse.
    expect(say(90)).toBe("90 seconds");
    expect(say(300)).toBe("5 minutes");
    expect(say(3600)).toBe("1 hour");
    expect(say(86400)).toBe("1 day");
    expect(say(0)).toBe("off");
  });
});
