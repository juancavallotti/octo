import { describe, expect, it, vi } from "vitest";
import { readCatalogue, readOverview, type StatsReads } from "./deploymentReads";

/**
 * The page's reads composed into one round trip. What has to hold is the order
 * — the pods are read before anything windowed, so the window is the tier's real
 * reach rather than a guess corrected on the next tick — and that a failing half
 * does not take the other half down with it.
 */

const NOW = 1_757_000_000_000;

function pod(rollupInterval: string) {
  return {
    pod: "octo-dep-1-abc",
    lastSeen: new Date(NOW).toISOString(),
    reporting: true,
    startedAt: null,
    sampleInterval: "1s",
    rollupInterval,
    retention: "168h0m0s",
    generation: 1,
    series: 95,
    liveRows: 3600,
    rollupRows: 168,
  };
}

function page(series: unknown[] = []) {
  return {
    deploymentId: "dep-1",
    tier: "live" as const,
    step: "1s",
    from: "",
    to: "",
    series,
    warnings: [],
    truncated: false,
  };
}

function reads(over: Partial<StatsReads> = {}): StatsReads {
  return {
    getPods: vi.fn(async () => ({
      ok: true as const,
      data: { deploymentId: "dep-1", items: [pod("10m0s")], truncated: false },
    })),
    getMetrics: vi.fn(async () => ({
      ok: true as const,
      data: { deploymentId: "dep-1", items: [], warnings: [], truncated: false },
    })),
    getSeries: vi.fn(async () => ({ ok: true as const, data: page() })),
    ...over,
  } as StatsReads;
}

describe("readOverview", () => {
  it("sizes the series window to what the pods say the tier holds", async () => {
    const r = reads();
    await readOverview(r, "dep-1", "live", NOW);

    const query = vi.mocked(r.getSeries).mock.calls[0][1];
    // Ten minutes of live tier, plus the tenth reachFor adds.
    expect(NOW - Date.parse(query.from!)).toBe(11 * 60_000);
  });

  it("keeps the series when the pod read fails, and says why", async () => {
    const r = reads({ getPods: async () => ({ ok: false, error: "pods unavailable" }) });
    const overview = await readOverview(r, "dep-1", "live", NOW);

    expect(overview.series).not.toBeNull();
    expect(overview.pods).toBeNull();
    expect(overview.error).toBe("pods unavailable");
  });
});

describe("readCatalogue", () => {
  it("fails as a whole when a batch fails, rather than drawing part of the grid as all of it", async () => {
    const r = reads({
      getMetrics: async () => ({
        ok: true,
        data: {
          deploymentId: "dep-1",
          items: [{ name: "go_goroutines", kind: "gauge", series: [] }],
          warnings: [],
          truncated: false,
        },
      }),
      getSeries: async () => ({ ok: false, error: "batch refused" }),
    } as Partial<StatsReads>);

    expect(await readCatalogue(r, "dep-1", "live", NOW)).toEqual({
      ok: false,
      error: "batch refused",
    });
  });

  it("gives a known metric with no rows its own empty card", async () => {
    const r = reads({
      getMetrics: async () => ({
        ok: true,
        data: {
          deploymentId: "dep-1",
          items: [{ name: "go_goroutines", kind: "gauge", series: [] }],
          warnings: [],
          truncated: false,
        },
      }),
    } as Partial<StatsReads>);

    const result = await readCatalogue(r, "dep-1", "live", NOW);
    expect(result.ok && result.data.metrics.map((m) => [m.name, m.series])).toEqual([
      ["go_goroutines", []],
    ]);
  });
});
