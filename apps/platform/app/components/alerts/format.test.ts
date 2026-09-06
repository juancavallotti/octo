import { describe, expect, it } from "vitest";
import {
  describeEpisode,
  describeOutcome,
  describeSchedule,
  describeVerdict,
  explainReason,
  formatSeconds,
  formatValue,
} from "./format";
import type { AlertOutcome, Incident, Watch } from "@/app/model/alerts";

function outcome(over: Partial<AlertOutcome> = {}): AlertOutcome {
  return {
    conditionId: "c_1",
    kind: "threshold",
    label: "error_rate gt over 15m",
    threshold: 0.05,
    observed: 0.41,
    samples: 15,
    windowFrom: "2026-09-06T09:45:00Z",
    windowTo: "2026-09-06T10:00:00Z",
    verdict: "true",
    ...over,
  };
}

describe("formatValue", () => {
  it("renders a ratio as a percentage", () => {
    expect(formatValue(0.41, "ratio")).toBe("41.0%");
    // A very small rate needs more precision, or every one of them reads 0.0%.
    expect(formatValue(0.0004, "ratio")).toBe("0.04%");
  });

  it("renders money with enough places to be a number", () => {
    expect(formatValue(12.5, "usd")).toBe("$12.50");
    expect(formatValue(0.0032, "usd")).toBe("$0.0032");
  });

  it("renders nanoseconds as something a human reads", () => {
    expect(formatValue(1_500_000, "ns")).toBe("1.5ms");
    expect(formatValue(2_000_000_000, "ns")).toBe("2.00s");
  });

  it("renders bytes in the unit they fit", () => {
    expect(formatValue(1536, "bytes")).toBe("1.5 KB");
    expect(formatValue(900, "bytes")).toBe("900 B");
  });

  // A missing measurement is not a zero, and the difference is the whole gap
  // policy this feature is arranged around.
  it("renders an absent value as absent, not as zero", () => {
    expect(formatValue(null)).toBe("—");
    expect(formatValue(undefined)).toBe("—");
    expect(formatValue(0)).toBe("0");
  });

  it("trims the noise off a plain number", () => {
    expect(formatValue(1234)).toBe("1,234");
    expect(formatValue(1.5)).toBe("1.5");
  });
});

describe("explainReason", () => {
  // The answer to "why did this not fire", which is the question asked after
  // every alert somebody expected.
  it("says a decline code in English", () => {
    expect(explainReason("below_min_delta")).toBe(
      "the change was real but too small to care about",
    );
    expect(explainReason("denominator_too_small")).toBe(
      "too few requests for the rate to mean anything",
    );
  });

  // A code this page has not learned yet must still read as words rather than
  // as a snake_case identifier nobody outside the service recognises.
  it("falls back to something readable", () => {
    expect(explainReason("some_new_gate")).toBe("some new gate");
    expect(explainReason(undefined)).toBe("");
  });
});

describe("describeOutcome", () => {
  it("says what was observed against what", () => {
    expect(describeOutcome(outcome({ unit: "ratio" }))).toBe(
      "observed 41.0% against 5.0%",
    );
  });

  it("includes the baseline when there is one", () => {
    expect(
      describeOutcome(outcome({ observed: 60, threshold: 4, baseline: 1 })),
    ).toBe("observed 60 against 4, against a baseline of 1");
  });

  // The numbers come off the outcome, so a row still describes the comparison
  // that was made rather than one against a threshold since retuned.
  it("reports an unmeasured condition without inventing a number", () => {
    expect(describeOutcome(outcome({ observed: null }))).toBe(
      "observed — against 0.05",
    );
  });
});

describe("formatSeconds", () => {
  it("says a duration the way somebody would", () => {
    expect(formatSeconds(30)).toBe("30s");
    expect(formatSeconds(300)).toBe("5m");
    expect(formatSeconds(90)).toBe("1.5m");
    expect(formatSeconds(7200)).toBe("2h");
  });

  // Zero is "off" rather than "0s": it is how a repeat interval says "announce
  // once", and 0s would read as "constantly".
  it("reads zero as off", () => {
    expect(formatSeconds(0)).toBe("off");
  });
});

describe("describeVerdict and describeSchedule", () => {
  it("says a composite verdict out loud", () => {
    expect(describeVerdict({ matched: 2, total: 3 }, "any")).toBe(
      "2 of 3 matched (any)",
    );
  });

  it("says a schedule in one line, omitting what is off", () => {
    const watch = {
      intervalSeconds: 60,
      forSeconds: 300,
      renotifySeconds: 0,
    } as Watch;
    expect(describeSchedule(watch)).toBe("every 1m · held 5m");
    expect(describeSchedule({ ...watch, renotifySeconds: 3600 })).toBe(
      "every 1m · held 5m · repeats 1h",
    );
  });
});

describe("describeEpisode", () => {
  const opened = "2026-09-06T10:00:00Z";
  const now = Date.parse("2026-09-06T10:45:00Z");

  it("says how long an open episode has been running", () => {
    const incident = { openedAt: opened, resolvedAt: null } as Incident;
    expect(describeEpisode(incident, now)).toBe("45m so far");
  });

  // Resolved and stale are different facts everywhere else, so the page has to
  // say which one closed it rather than just "closed".
  it("says how an episode ended", () => {
    const incident = {
      openedAt: opened,
      resolvedAt: "2026-09-06T10:20:00Z",
      closedReason: "stale",
    } as Incident;
    expect(describeEpisode(incident, now)).toBe("20m, stale");
  });
});
