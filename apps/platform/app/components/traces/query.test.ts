import { describe, expect, it } from "vitest";
import {
  buildHref,
  buildPath,
  DEFAULT_FILTERS,
  NOTHING_SELECTED,
  parsePathname,
  readFilters,
  readSelection,
  TRACES_BASE,
  windowFor,
  writeFilters,
} from "./query";

describe("the traces selection path", () => {
  const cases: [label: string, selection: Parameters<typeof buildPath>[0], path: string][] = [
    ["nothing", NOTHING_SELECTED, ""],
    [
      "an app across every version",
      { deploymentId: "dep-1", appVersion: null, traceId: null },
      "/a/dep-1",
    ],
    [
      "an app at one version",
      { deploymentId: "dep-1", appVersion: "v1.2.0", traceId: null },
      "/a/dep-1/v/v1.2.0",
    ],
    [
      "a trace under an app",
      { deploymentId: "dep-1", appVersion: null, traceId: "tr-9" },
      "/a/dep-1/t/tr-9",
    ],
    [
      "a trace under one version of an app",
      { deploymentId: "dep-1", appVersion: "v1.2.0", traceId: "tr-9" },
      "/a/dep-1/v/v1.2.0/t/tr-9",
    ],
  ];

  it.each(cases)("round-trips %s", (_label, selection, path) => {
    expect(buildPath(selection)).toBe(path);
    expect(parsePathname(`${TRACES_BASE}${path}`)).toEqual(selection);
  });

  it("builds a full route", () => {
    expect(buildHref({ deploymentId: "dep-1", appVersion: null, traceId: null })).toBe(
      "/platform/traces/a/dep-1",
    );
    expect(buildHref(NOTHING_SELECTED)).toBe("/platform/traces");
  });

  it("encodes a version tag that is not path-safe", () => {
    // Tags are whatever someone typed, and trace ids come off the wire.
    const selection = { deploymentId: "dep-1", appVersion: "release/2026 Q3", traceId: "a/b" };
    const path = buildPath(selection);
    expect(path).toBe("/a/dep-1/v/release%2F2026%20Q3/t/a%2Fb");
    expect(parsePathname(`${TRACES_BASE}${path}`)).toEqual(selection);
  });

  it("selects nothing from a path that names no app", () => {
    // A version or a trace only means something under an app: without one there
    // is no list to narrow and no app to fetch the trace against.
    expect(readSelection([])).toEqual(NOTHING_SELECTED);
    expect(readSelection(["v", "v1.0"])).toEqual(NOTHING_SELECTED);
    expect(readSelection(["t", "tr-9"])).toEqual(NOTHING_SELECTED);
    expect(readSelection(["a"])).toEqual(NOTHING_SELECTED);
  });

  it("ignores segments it does not recognize rather than guessing", () => {
    expect(readSelection(["a", "dep-1", "zzz", "x"])).toEqual({
      deploymentId: "dep-1",
      appVersion: null,
      traceId: null,
    });
  });

  it("reads a path that is not under the traces route as no selection", () => {
    expect(parsePathname("/platform/logs")).toEqual(NOTHING_SELECTED);
  });
});

describe("the traces filter query string", () => {
  it("leaves a default view's URL clean", () => {
    expect(writeFilters(DEFAULT_FILTERS)).toBe("");
    expect(readFilters(new URLSearchParams(""))).toEqual(DEFAULT_FILTERS);
  });

  it("round-trips everything a reader can narrow by", () => {
    const filters = {
      window: "7d" as const,
      statuses: ["failed" as const, "dropped" as const],
      hasLlm: true,
      minDurationMs: 500,
      q: "POST /orders",
    };
    const written = writeFilters(filters);
    expect(readFilters(new URLSearchParams(written))).toEqual(filters);
  });

  it("drops a status the service would reject rather than sending it", () => {
    // The service answers an unmatchable status with an error, which renders as
    // an outage — for what is really a stale link or a typed URL.
    const params = new URLSearchParams("status=failed&status=banana");
    expect(readFilters(params).statuses).toEqual(["failed"]);
  });

  it("falls back to the default window for one it does not know", () => {
    expect(readFilters(new URLSearchParams("w=eternity")).window).toBe(
      DEFAULT_FILTERS.window,
    );
  });

  it("ignores a duration threshold that is not one", () => {
    expect(readFilters(new URLSearchParams("slower=soon")).minDurationMs).toBeNull();
    expect(readFilters(new URLSearchParams("slower=-5")).minDurationMs).toBeNull();
    expect(readFilters(new URLSearchParams("slower=0")).minDurationMs).toBeNull();
  });
});

describe("windowFor", () => {
  const now = Date.parse("2026-08-09T12:00:00.000Z");

  it("resolves a preset against the instant it was given", () => {
    // Given rather than read: a window recomputed each render would change each
    // render, and a changing window is a query that refetches forever.
    expect(windowFor("1h", now)).toEqual({
      from: "2026-08-09T11:00:00.000Z",
      to: "2026-08-09T12:00:00.000Z",
    });
    expect(windowFor("7d", now).from).toBe("2026-08-02T12:00:00.000Z");
  });
});
