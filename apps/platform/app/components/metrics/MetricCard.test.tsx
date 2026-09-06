import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { StatsMetric, StatsSeries } from "@/app/model/stats";
import MetricCard, { labelKey } from "./MetricCard";
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

/** One decoded series with the labels and readings a test cares about. */
function series(labels: Record<string, string>, values: (number | null)[]): StatsSeries {
  return {
    pod: "pod-a",
    name: "octo_flow_messages_total",
    kind: "gauge",
    labels,
    times: values.map((_, i) => NOW + i * 1000),
    ends: [],
    values,
    min: [],
    max: [],
    last: [],
    samples: [],
  };
}

function entry(all: StatsSeries[]): CataloguedMetric {
  const metric: StatsMetric = {
    name: "octo_flow_messages_total",
    kind: "gauge",
    series: all.map((s) => ({ labels: s.labels, pods: [s.pod] })),
  };
  return { name: metric.name, metric, series: all };
}

describe("steadiness", () => {
  it("does not call a single reading unchanged", () => {
    // The history tier at the short end of its range routinely returns one
    // bucket. Claiming stability from one measurement asserts something nobody
    // observed — and it suppresses the chart that would have shown as much.
    render(
      <MetricCard
        entry={entry([series({}, [42])])}
        stepMs={1000}
        fromMs={NOW}
        toMs={NOW + 1000}
      />,
    );
    expect(screen.queryByText("unchanged over this window")).not.toBeInTheDocument();
  });

  it("calls a genuinely flat series unchanged", () => {
    render(
      <MetricCard
        entry={entry([series({}, [42, 42, 42])])}
        stepMs={1000}
        fromMs={NOW}
        toMs={NOW + 3000}
      />,
    );
    expect(screen.getByText("unchanged over this window")).toBeInTheDocument();
  });
});

describe("label-set identity", () => {
  it("keeps two label sets apart when a value contains the separators", () => {
    // Joined as k=v pairs, {a: "b,c=d"} and {a: "b", c: "d"} both flatten to
    // "a=b,c=d" — two chart lines sharing one key and losing their identity.
    const collidingPair = series({ a: "b,c=d" }, [1]);
    const innocentPair = series({ a: "b", c: "d" }, [1]);

    expect(labelKey(collidingPair)).not.toBe(labelKey(innocentPair));
  });

  it("is stable whatever order the labels arrive in", () => {
    expect(labelKey(series({ b: "2", a: "1" }, [1]))).toBe(
      labelKey(series({ a: "1", b: "2" }, [1])),
    );
  });

  it("gives an unlabelled series a key of its own", () => {
    expect(labelKey(series({}, [1]))).toBe(labelKey(series({}, [2])));
    expect(labelKey(series({}, [1]))).not.toBe(labelKey(series({ a: "" }, [1])));
  });
});
