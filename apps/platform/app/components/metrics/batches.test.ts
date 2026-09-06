import { describe, expect, it } from "vitest";
import type { StatsMetric } from "@/app/model/stats";
import { MAX_NAMES, SERIES_BUDGET, planBatches, seriesCount } from "./batches";

/**
 * Packing the catalogue into requests, sized by the limit that actually binds.
 *
 * The numbers here are a real deployment's: fifty metric names resolving to a
 * hundred and eighty-eight series, of which one metric —
 * octo_flow_duration_seconds_bucket, three flows by three outcomes by twelve
 * bucket boundaries — is a hundred and eight on its own.
 */

function metric(name: string, series: number, pods = 1): StatsMetric {
  return {
    name,
    kind: "gauge",
    series: Array.from({ length: series }, (_, i) => ({
      labels: { i: String(i) },
      pods: Array.from({ length: pods }, (_, p) => `pod-${p}`),
    })),
  };
}

describe("seriesCount", () => {
  it("multiplies label sets by the pods exposing them", () => {
    expect(seriesCount(metric("m", 12, 3))).toBe(36);
  });

  it("counts a metric with no label sets as nothing to fetch", () => {
    expect(seriesCount({ name: "m", kind: "gauge", series: [] })).toBe(0);
  });
});

describe("planBatches", () => {
  it("keeps every name exactly once", () => {
    const catalogue = Array.from({ length: 50 }, (_, i) => metric(`m${i}`, 1));
    const names = planBatches(catalogue).flat();

    expect(names).toHaveLength(50);
    expect(new Set(names).size).toBe(50);
  });

  it("respects the service's cap on metric parameters", () => {
    const catalogue = Array.from({ length: 50 }, (_, i) => metric(`m${i}`, 1));
    for (const batch of planBatches(catalogue)) {
      expect(batch.length).toBeLessThanOrEqual(MAX_NAMES);
    }
  });

  it("packs by series rather than by name count", () => {
    // The bug this prevents: four ordinary metrics and one histogram in the same
    // request, whose response is twenty times its neighbours'.
    const catalogue = [metric("buckets", 108), ...Array.from({ length: 20 }, (_, i) => metric(`m${i}`, 1))];
    const batches = planBatches(catalogue);

    const cost = (batch: string[]) =>
      batch.reduce((n, name) => n + seriesCount(catalogue.find((m) => m.name === name)!), 0);
    for (const batch of batches) expect(cost(batch)).toBeLessThanOrEqual(SERIES_BUDGET);
  });

  it("gives a metric larger than the whole budget its own request", () => {
    // Never dropped: the page exists to show everything, and the service has no
    // way to return part of a metric.
    const catalogue = [metric("huge", SERIES_BUDGET + 50), metric("small", 1)];
    const batches = planBatches(catalogue);

    expect(batches.flat()).toContain("huge");
    expect(batches.find((b) => b.includes("huge"))).toEqual(["huge"]);
  });

  it("plans nothing for an empty catalogue", () => {
    expect(planBatches([])).toEqual([]);
  });
});
