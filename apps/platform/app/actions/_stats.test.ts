import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The pod stats client, at the places it can quietly lie.
 *
 * The sharp one is the gap. A reading of null means the scrape did not report
 * that series at that moment — a removed flow, a restarting process, a metric
 * that has not been touched yet. The storage layer encodes it as null on purpose
 * and the service is built so it cannot become anything else. This is the last
 * layer that can throw the distinction away with a `?? 0` that looks like
 * defensive coding, and a chart handed a zero draws a cliff that never happened.
 *
 * The rest is the query string. `metric` is what bounds the whole read — rows are
 * stored positionally, so an unfiltered query reads every series of every pod —
 * and it is repeatable, which is exactly the shape a `set()` silently collapses.
 */

const requestJson = vi.fn();
vi.mock("@octo/http", () => ({
  requestJson: (method: string, url: string) => requestJson(method, url),
}));

import * as stats from "./_stats";
import type { RawPodsPage, RawSeriesPage } from "./_statsWire";

const BASE = "http://logs:8091";
const DEP = "octo-dep-b01836dc";

/** The URL the single mocked call was made against. */
function calledUrl(): string {
  expect(requestJson).toHaveBeenCalledTimes(1);
  return requestJson.mock.calls[0][1] as string;
}

/** Its query string, as a lookup that keeps repeated keys. */
function calledParams(): URLSearchParams {
  return new URL(calledUrl()).searchParams;
}

function ok<T>(data: T) {
  requestJson.mockResolvedValue({ ok: true, data });
}

/** A series page with only the fields the service always sends. */
function seriesPage(over: Partial<RawSeriesPage> = {}): RawSeriesPage {
  return {
    deploymentId: DEP,
    tier: "live",
    step: "1s",
    from: "2026-09-05T12:00:00Z",
    to: "2026-09-05T12:05:00Z",
    series: null,
    warnings: null,
    truncated: false,
    ...over,
  };
}

beforeEach(() => {
  process.env.LOGS_URL = BASE;
});

afterEach(() => {
  vi.clearAllMocks();
  delete process.env.LOGS_URL;
});

describe("configuration", () => {
  it("does not call out when LOGS_URL is unset", async () => {
    delete process.env.LOGS_URL;

    const res = await stats.getPods(DEP);

    expect(res).toEqual({
      ok: false,
      error: "pod stats not configured (LOGS_URL unset)",
    });
    expect(requestJson).not.toHaveBeenCalled();
  });

  it("does not double the slash when LOGS_URL has a trailing one", async () => {
    process.env.LOGS_URL = `${BASE}/`;
    ok<RawPodsPage>({ deploymentId: DEP, items: null, truncated: false });

    await stats.getPods(DEP);

    expect(calledUrl()).toBe(`${BASE}/stats/${DEP}/pods`);
  });

  it("encodes the deployment id into the path", async () => {
    ok<RawPodsPage>({ deploymentId: "a/b", items: null, truncated: false });

    await stats.getPods("a/b");

    expect(calledUrl()).toBe(`${BASE}/stats/a%2Fb/pods`);
  });
});

describe("the series query", () => {
  it("repeats metric, pod and label rather than collapsing them", async () => {
    ok(seriesPage());

    await stats.getSeries(DEP, {
      metrics: ["process_cpu_seconds_total", "process_resident_memory_bytes"],
      pods: ["pod-a", "pod-b"],
      labels: { flow: "main", state: "ok" },
    });

    const params = calledParams();
    expect(params.getAll("metric")).toEqual([
      "process_cpu_seconds_total",
      "process_resident_memory_bytes",
    ]);
    expect(params.getAll("pod")).toEqual(["pod-a", "pod-b"]);
    expect(params.getAll("label").sort()).toEqual(["flow=main", "state=ok"]);
  });

  it("refuses a query naming no metric without calling out", async () => {
    const res = await stats.getSeries(DEP, { metrics: [] });

    expect(res).toEqual({ ok: false, error: "name at least one metric to read" });
    expect(requestJson).not.toHaveBeenCalled();
  });

  it("omits everything left at its default", async () => {
    ok(seriesPage());

    await stats.getSeries(DEP, { metrics: ["m"] });

    const params = calledParams();
    expect([...params.keys()]).toEqual(["metric"]);
  });

  it("sends the projection as one comma separated parameter", async () => {
    ok(seriesPage());

    await stats.getSeries(DEP, {
      metrics: ["m"],
      stats: ["value", "min", "max"],
      tier: "rollup",
      counters: "absolute",
      limit: 500,
    });

    const params = calledParams();
    expect(params.get("stats")).toBe("value,min,max");
    expect(params.get("tier")).toBe("rollup");
    expect(params.get("counters")).toBe("absolute");
    expect(params.get("limit")).toBe("500");
  });
});

describe("mapping", () => {
  it("keeps a gap null rather than making it a measurement", async () => {
    ok(
      seriesPage({
        series: [
          {
            pod: "pod-a",
            name: "process_resident_memory_bytes",
            kind: "gauge",
            times: [1, 2, 3],
            values: [10, null, 12],
          },
        ],
      }),
    );

    const res = await stats.getSeries(DEP, { metrics: ["m"] });
    if (!res.ok) throw new Error(res.error);

    const values = res.data.series[0].values;
    expect(values).toEqual([10, null, 12]);
    expect(values[1]).toBeNull();
    expect(values[1]).not.toBe(0);
  });

  it("fills in the collections the service omits when empty", async () => {
    ok(
      seriesPage({
        series: [
          {
            pod: "pod-a",
            name: "m",
            kind: "gauge",
            times: [1],
            values: [1],
          },
        ],
      }),
    );

    const res = await stats.getSeries(DEP, { metrics: ["m"] });
    if (!res.ok) throw new Error(res.error);

    const series = res.data.series[0];
    expect(series.ends).toEqual([]);
    expect(series.min).toEqual([]);
    expect(series.labels).toEqual({});
    expect(res.data.warnings).toEqual([]);
  });

  it("reports truncation and warnings rather than swallowing them", async () => {
    ok(
      seriesPage({
        truncated: true,
        warnings: [{ pod: "pod-c", reason: "no rows in window" }],
      }),
    );

    const res = await stats.getSeries(DEP, { metrics: ["m"] });
    if (!res.ok) throw new Error(res.error);

    expect(res.data.truncated).toBe(true);
    expect(res.data.warnings).toEqual([{ pod: "pod-c", reason: "no rows in window" }]);
  });

  it("narrows a kind it does not know rather than passing it through", async () => {
    ok(
      seriesPage({
        series: [
          { pod: "p", name: "m", kind: "histogram", times: [1], values: [1] },
        ],
      }),
    );

    const res = await stats.getSeries(DEP, { metrics: ["m"] });
    if (!res.ok) throw new Error(res.error);

    expect(res.data.series[0].kind).toBe("unknown");
  });

  it("keeps a missing start time absent rather than dating it to the epoch", async () => {
    ok<RawPodsPage>({
      deploymentId: DEP,
      items: [
        {
          pod: "pod-a",
          lastSeen: "2026-09-05T12:00:00Z",
          reporting: true,
          sampleInterval: "1s",
          rollupInterval: "1h0m0s",
          retention: "168h0m0s",
          generation: 2,
          series: 95,
          liveRows: 3600,
          rollupRows: 168,
        },
      ],
      truncated: false,
    });

    const res = await stats.getPods(DEP);
    if (!res.ok) throw new Error(res.error);

    expect(res.data.items[0].startedAt).toBeNull();
  });
});
