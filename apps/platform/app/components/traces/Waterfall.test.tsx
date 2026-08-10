import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import Waterfall from "./Waterfall";
import { buildWaterfall } from "./buildWaterfall";
import { records } from "./fixtures";
import { MAX_ROWS } from "./chartLayout";

/**
 * The chart, as a reader meets it. What matters here is not pixels but claims:
 * that a nested block is drawn inside its composite, that a bar too short to see
 * is still drawn, that nothing is hidden without saying so, and that an ordinary
 * scroll still scrolls.
 */

/** A request through a composite, an agent and a model call. */
function trace() {
  return buildWaterfall(
    records(
      { kind: "block.post-invoke", start: 10_000, duration: 40_000, path: "orders.check[then].notify", blockType: "rest" },
      { kind: "block.post-invoke", start: 5_000, duration: 60_000, path: "orders.check", blockType: "if" },
      { kind: "block.post-invoke", start: 70_000, duration: 30, path: "orders.stamp", blockType: "set-variable" },
      { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
    ),
  );
}

function renderChart(waterfall = trace()) {
  const onSelect = vi.fn();
  render(<Waterfall waterfall={waterfall} selectedId={null} onSelect={onSelect} />);
  return onSelect;
}

describe("Waterfall", () => {
  it("is a treegrid, so it can be read and navigated as one", () => {
    renderChart();
    const grid = screen.getByRole("treegrid", { name: "Trace waterfall" });
    expect(within(grid).getAllByRole("row")).toHaveLength(4);
  });

  it("nests a block inside the composite it ran in", () => {
    renderChart();
    const rows = screen.getAllByRole("row");
    expect(rows.map((r) => r.getAttribute("aria-level"))).toEqual(["1", "2", "3", "2"]);
  });

  it("draws a span too short to see rather than dropping it", () => {
    // 30ns beside a 100µs trace is 0.03% of the width. Nothing on screen is
    // indistinguishable from a block that never ran.
    renderChart();
    const stamp = screen.getAllByRole("row").find((r) => r.textContent?.includes("stamp"))!;
    // The bar lives in the track cell, which is the row's second gridcell.
    const track = within(stamp).getAllByRole("gridcell")[1];
    const bar = track.firstElementChild as HTMLElement;
    expect(bar.style.minWidth).toBe("2px");
    expect(Number.parseFloat(bar.style.width)).toBeGreaterThan(0);
  });

  it("folds a subtree away and says how much it folded", async () => {
    renderChart();
    await userEvent.click(screen.getAllByRole("button", { name: "Collapse" })[1]);
    expect(screen.getAllByRole("row")).toHaveLength(3);
    expect(screen.getByText("+1")).toBeInTheDocument();
  });

  it("collapses and expands everything at once", async () => {
    renderChart();
    await userEvent.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.getAllByRole("row")).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "Expand all" }));
    expect(screen.getAllByRole("row")).toHaveLength(4);
  });

  it("hands back the span that was clicked", async () => {
    const onSelect = renderChart();
    await userEvent.click(screen.getByText("notify"));
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ label: "notify" }));
  });

  it("counts every span, including the ones folded away", () => {
    renderChart();
    expect(screen.getByText("4 spans")).toBeInTheDocument();
  });

  it("says when the row cap kept spans off the chart", () => {
    // A runaway foreach is exactly the trace someone opens to find out why. The
    // cap has to hold, and it has to admit what it is not showing.
    const runaway = MAX_ROWS + 20;
    const big = buildWaterfall(
      records(
        ...Array.from({ length: runaway }, (_, i) => ({
          kind: "block.post-invoke",
          start: i * 100,
          duration: 50,
          path: `orders.b${i}`,
        })),
        { kind: "flow.completed", start: 0, duration: runaway * 100, flow: "orders" },
      ),
    );
    render(<Waterfall waterfall={big} selectedId={null} onSelect={vi.fn()} />);
    expect(screen.getByText(/more not drawn/)).toBeInTheDocument();
  });

  it("surfaces what the reconstruction could not know", () => {
    // An invocation with no terminal record. Left unsaid, its inferred span
    // would read as a measurement.
    const lossy = buildWaterfall(
      records(
        { kind: "block.post-invoke", start: 30_000, duration: 10_000, path: "pricing.lookup", eventId: "ev-sub", flow: "pricing" },
        { kind: "block.post-invoke", start: 20_000, duration: 60_000, path: "orders.price" },
        { kind: "flow.completed", start: 0, duration: 100_000, flow: "orders" },
      ),
    );
    render(<Waterfall waterfall={lossy} selectedId={null} onSelect={vi.fn()} />);
    expect(screen.getByText(/never reported an outcome/)).toBeInTheDocument();
    expect(
      screen.getByLabelText(/this span is inferred from what ran inside it/),
    ).toBeInTheDocument();
  });

  it("offers a way back only once there is somewhere to come back from", () => {
    renderChart();
    expect(screen.queryByRole("button", { name: "Fit (Esc)" })).not.toBeInTheDocument();
  });
});
