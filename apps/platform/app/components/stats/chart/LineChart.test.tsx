import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import LineChart from "./LineChart";

/**
 * The geometry is tested in scale.test.ts; what is left to check here is that the
 * projection reaches the DOM intact. A chart fails silently — a NaN in a path's
 * `d` makes the browser drop the whole subpath, with no error anywhere — so the
 * cases that would produce one are worth pinning at this level too.
 */

const NOW = 1_757_000_000_000;

function points(values: (number | null)[]) {
  return {
    times: values.map((_, i) => NOW + i * 1000),
    values,
  };
}

/** Every `d` the chart drew. */
function paths(container: HTMLElement): string[] {
  return [...container.querySelectorAll("path")].map((p) => p.getAttribute("d") ?? "");
}

describe("LineChart", () => {
  it("projects both units without producing NaN", () => {
    const { container } = render(
      <LineChart
        cpu={[{ pod: "pod-a", points: points([0.1, 0.2, 0.15]) }]}
        memory={[{ pod: "pod-a", points: points([1e8, 1.1e8, 1.05e8]) }]}
        fromMs={NOW}
        toMs={NOW + 3000}
      />,
    );

    const drawn = paths(container).filter(Boolean);
    expect(drawn).toHaveLength(2);
    for (const d of drawn) expect(d).not.toContain("NaN");
  });

  it("breaks a line at a gap instead of drawing through it", () => {
    const { container } = render(
      <LineChart
        cpu={[{ pod: "pod-a", points: points([0.1, null, 0.15]) }]}
        memory={[]}
        fromMs={NOW}
        toMs={NOW + 3000}
      />,
    );

    const [cpu] = paths(container).filter(Boolean);
    expect(cpu.match(/M/g)).toHaveLength(2);
  });

  it("renders a flat series rather than collapsing to nothing", () => {
    // A pod holding steady has min === max, which is a division by zero in every
    // naive scale and the likeliest way this chart comes out blank.
    const { container } = render(
      <LineChart
        cpu={[{ pod: "pod-a", points: points([0, 0, 0]) }]}
        memory={[{ pod: "pod-a", points: points([5e7, 5e7, 5e7]) }]}
        fromMs={NOW}
        toMs={NOW + 3000}
      />,
    );

    for (const d of paths(container).filter(Boolean)) {
      expect(d).not.toContain("NaN");
      expect(d).toContain("L");
    }
  });

  it("draws axes and a readout with nothing to plot", () => {
    const { container, getByText } = render(
      <LineChart cpu={[]} memory={[]} fromMs={NOW} toMs={NOW + 3000} />,
    );

    expect(getByText("latest")).toBeInTheDocument();
    expect(container.querySelector("svg")).toBeInTheDocument();
  });
});
