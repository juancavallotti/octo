import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { StatsMetric } from "@/app/model/stats";
import MetricCard from "./MetricCard";
import type { CataloguedMetric } from "./useDeploymentCatalogue";

const NOW = 1_757_000_000_000;

/**
 * An info metric — value always 1, content in the labels — as two pods on
 * different builds actually report it. This is not hypothetical: the local
 * cluster showed octo_build_info twice, with two build dates, the moment a
 * deployment was rolled.
 */
function buildInfo(): CataloguedMetric {
  const metric: StatsMetric = {
    name: "octo_build_info",
    kind: "gauge",
    series: [
      { labels: { version: "0.9.1", build_date: "2026-09-06T04:44:55Z" }, pods: ["pod-a"] },
      { labels: { version: "0.9.1", build_date: "2026-09-06T05:34:48Z" }, pods: ["pod-b"] },
    ],
  };
  return { name: metric.name, metric, series: [] };
}

describe("an info metric reported by two pods", () => {
  it("renders every label set, including a repeated label name", () => {
    // Keyed on the label name alone, React sees duplicate siblings and the
    // second build_date can be dropped or mis-reconciled on a later render.
    render(
      <MetricCard entry={buildInfo()} stepMs={1000} fromMs={NOW - 60_000} toMs={NOW} />,
    );

    expect(screen.getAllByText("build_date")).toHaveLength(2);
    expect(screen.getByText("2026-09-06T04:44:55Z")).toBeInTheDocument();
    expect(screen.getByText("2026-09-06T05:34:48Z")).toBeInTheDocument();
  });

  it("states the metric rather than charting a flat line at one", () => {
    render(
      <MetricCard entry={buildInfo()} stepMs={1000} fromMs={NOW - 60_000} toMs={NOW} />,
    );
    expect(screen.getByText(/reported through its labels/)).toBeInTheDocument();
  });
});
