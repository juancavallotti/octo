import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

/**
 * The sparkline feed, at the two things that make it safe to put on a page that
 * lists every deployment.
 *
 * The first is degradation. Pod stats are off by default, so on most installs
 * every one of these calls fails, and the page must respond by showing nothing at
 * all rather than by growing an error strip per card.
 *
 * The second is the fold. A deployment is one line, not one per pod, and summing
 * pods is only correct if an absent pod contributes nothing rather than a zero —
 * otherwise every restart draws a cliff.
 */

const readStatsSeries = vi.fn();
vi.mock("@/app/model/stats", () => ({
  readStatsSeries: (id: string, q: unknown) => readStatsSeries(id, q),
}));

import { useDeploymentStats } from "./useDeploymentStats";

const NOW = 1_757_000_000_000;

function page(series: unknown[]) {
  return {
    deploymentId: "dep-1",
    tier: "live",
    step: "1s",
    from: new Date(NOW - 300_000).toISOString(),
    to: new Date(NOW).toISOString(),
    series,
    warnings: [],
    truncated: false,
  };
}

function memory(pod: string, values: (number | null)[]) {
  return {
    pod,
    name: "process_resident_memory_bytes",
    kind: "gauge",
    labels: {},
    times: values.map((_, i) => NOW + i * 1000),
    ends: [],
    values,
    min: [],
    max: [],
    last: [],
    samples: [],
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

it("asks for a five minute live window per deployment", async () => {
  readStatsSeries.mockResolvedValue(page([]));

  renderHook(() => useDeploymentStats(["dep-1", "dep-2"]));

  await waitFor(() => expect(readStatsSeries).toHaveBeenCalledTimes(2));
  const query = readStatsSeries.mock.calls[0][1];
  expect(query.tier).toBe("live");
  const width = Date.parse(query.to) - Date.parse(query.from);
  expect(width).toBe(5 * 60_000);
});

it("reports unavailable rather than an error when nothing answers", async () => {
  readStatsSeries.mockRejectedValue(new Error("pod stats not configured (OBSERVABILITY_URL unset)"));

  const { result } = renderHook(() => useDeploymentStats(["dep-1"]));

  await waitFor(() => expect(result.current.available).toBe(false));
  expect(result.current.data.size).toBe(0);
});

it("keeps the deployments that did answer when one fails", async () => {
  readStatsSeries.mockImplementation((id: string) =>
    id === "dep-1"
      ? Promise.resolve(page([memory("pod-a", [100])]))
      : Promise.reject(new Error("gone")),
  );

  const { result } = renderHook(() => useDeploymentStats(["dep-1", "dep-2"]));

  await waitFor(() => expect(result.current.data.size).toBe(1));
  expect(result.current.available).toBe(true);
  expect(result.current.data.has("dep-1")).toBe(true);
});

it("sums pods without letting an absent one read as zero", async () => {
  readStatsSeries.mockResolvedValue(
    page([memory("pod-a", [100, 100]), memory("pod-b", [50, null])]),
  );

  const { result } = renderHook(() => useDeploymentStats(["dep-1"]));

  await waitFor(() => expect(result.current.data.size).toBe(1));
  const spark = result.current.data.get("dep-1");
  // The second moment is pod-a alone, not pod-a plus a fabricated zero — which
  // would draw a cliff every time a pod restarted.
  expect(spark?.memory.values).toEqual([150, 100]);
});

it("does not split a deployment id that contains a separator", async () => {
  // Ids are opaque. Joining them into one string and splitting it back would
  // turn one deployment into two requests for deployments that do not exist.
  readStatsSeries.mockResolvedValue(page([]));

  renderHook(() => useDeploymentStats(["dep,with,commas"]));

  await waitFor(() => expect(readStatsSeries).toHaveBeenCalledTimes(1));
  expect(readStatsSeries.mock.calls[0][0]).toBe("dep,with,commas");
});

it("ignores a slow poll that lands after a newer one", async () => {
  // Thirty seconds is not a guarantee: a poll slower than the interval overlaps
  // the next one, and if the older answer lands last the cards go backwards.
  vi.useFakeTimers();
  const landings: Array<(v: unknown) => void> = [];
  readStatsSeries.mockImplementation(
    () => new Promise((resolve) => landings.push(resolve)),
  );

  const { result } = renderHook(() => useDeploymentStats(["dep-1"]));
  await vi.waitFor(() => expect(landings).toHaveLength(1));

  // The interval fires while the first poll is still out.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000);
  });
  expect(landings).toHaveLength(2);

  // The newer answer lands first, then the older one.
  await act(async () => {
    landings[1](page([memory("pod-a", [222])]));
    landings[0](page([memory("pod-a", [111])]));
    await vi.advanceTimersByTimeAsync(0);
  });

  expect(result.current.data.get("dep-1")?.memory.values).toEqual([222]);
});

it("holds nothing for a page with no running deployments", async () => {
  const { result } = renderHook(() => useDeploymentStats([]));

  expect(result.current.data.size).toBe(0);
  expect(readStatsSeries).not.toHaveBeenCalled();
});
