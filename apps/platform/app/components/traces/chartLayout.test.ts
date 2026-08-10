import { describe, expect, it } from "vitest";
import { buildWaterfall } from "./buildWaterfall";
import { records } from "./fixtures";
import {
  axisTicks,
  barStyle,
  clampView,
  flattenWaterfall,
  MAX_ROWS,
  panBy,
  zoomAt,
} from "./chartLayout";
import type { WaterfallNode } from "./types";

/** A tree of the given shape, labelled a, b, c… so assertions read. */
function tree(): WaterfallNode[] {
  const trace = records(
    { kind: "block.post-invoke", start: 20, duration: 30, path: "orders.a[then].b" },
    { kind: "block.post-invoke", start: 10, duration: 60, path: "orders.a" },
    { kind: "block.post-invoke", start: 80, duration: 10, path: "orders.c" },
    { kind: "flow.completed", start: 0, duration: 100, flow: "orders" },
  );
  return buildWaterfall(trace).roots;
}

const VIEW = { start: 0, end: 100 };

describe("flattenWaterfall", () => {
  it("walks the tree into rows, deepest last", () => {
    const { rows } = flattenWaterfall(tree(), new Set());
    expect(rows.map((r) => r.node.label)).toEqual(["orders", "a", "b", "c"]);
    expect(rows.map((r) => r.node.depth)).toEqual([0, 1, 2, 1]);
  });

  it("hides a collapsed subtree and says how much it hid", () => {
    const { rows } = flattenWaterfall(tree(), new Set(["r1"]));
    const a = rows.find((r) => r.node.label === "a")!;
    expect(a.collapsed).toBe(true);
    expect(a.hidden).toBe(1);
    expect(rows.map((r) => r.node.label)).toEqual(["orders", "a", "c"]);
  });

  it("marks only the rows that hold something as collapsible", () => {
    const { rows, collapsible } = flattenWaterfall(tree(), new Set());
    expect(rows.filter((r) => r.expandable).map((r) => r.node.label)).toEqual([
      "orders",
      "a",
    ]);
    expect(collapsible).toHaveLength(2);
  });

  it("stops at the cap and counts what it left out", () => {
    // A runaway foreach is exactly the trace someone opens to find out why, so
    // the cap has to hold — and has to admit how much it is not showing.
    const { rows, cut } = flattenWaterfall(tree(), new Set(), 2);
    expect(rows).toHaveLength(2);
    // The two rows shown are "orders" and "a"; b and c are what remain.
    expect(cut).toBe(2);
  });

  it("caps at two thousand rows by default", () => {
    expect(MAX_ROWS).toBe(2000);
  });
});

describe("barStyle", () => {
  it("places a bar as a fraction of the viewport", () => {
    expect(barStyle({ start: 25, end: 50 }, VIEW)).toEqual({
      left: "25%",
      width: "25%",
    });
  });

  it("keeps a span too short to see from disappearing", () => {
    // A 40µs block beside a 3s model call rounds to nothing, and nothing on
    // screen is indistinguishable from a block that never ran.
    const style = barStyle({ start: 10, end: 10.0001 }, { start: 0, end: 1_000_000 })!;
    expect(Number.parseFloat(style.width)).toBeGreaterThan(0);
  });

  it("clips a bar that runs past the edge instead of dropping it", () => {
    // Zoom into a child and its parent starts before the view does. A row with
    // no bar at all would read as a row with no time in it.
    expect(barStyle({ start: -50, end: 50 }, VIEW)).toEqual({ left: "0%", width: "50%" });
    expect(barStyle({ start: 50, end: 500 }, VIEW)).toEqual({ left: "50%", width: "50%" });
  });

  it("draws nothing for a span outside the viewport", () => {
    expect(barStyle({ start: 200, end: 300 }, VIEW)).toBeNull();
    expect(barStyle({ start: -300, end: -200 }, VIEW)).toBeNull();
  });
});

describe("the viewport", () => {
  const bounds = { start: 0, end: 1000 };

  it("zooms about the point under the cursor", () => {
    // Halving the width around the middle leaves the middle where it was.
    expect(zoomAt(bounds, 0.5, 0.5, bounds)).toEqual({ start: 250, end: 750 });
    // …and around the left edge keeps the left edge.
    expect(zoomAt(bounds, 0, 0.5, bounds)).toEqual({ start: 0, end: 500 });
  });

  it("never zooms out past the trace", () => {
    expect(zoomAt({ start: 400, end: 600 }, 0.5, 100, bounds)).toEqual(bounds);
  });

  it("never zooms in to nothing", () => {
    const tiny = zoomAt(bounds, 0.5, 1e-9, bounds);
    expect(tiny.end - tiny.start).toBeGreaterThan(0);
  });

  it("pans without changing the width, and stops at the ends", () => {
    const view = { start: 400, end: 600 };
    expect(panBy(view, 0.5, bounds)).toEqual({ start: 500, end: 700 });
    // Pushed past the end, it stops flush against it rather than shrinking.
    expect(panBy(view, 100, bounds)).toEqual({ start: 800, end: 1000 });
  });

  it("holds a view inside the trace", () => {
    expect(clampView({ start: -500, end: -300 }, bounds)).toEqual({ start: 0, end: 200 });
    expect(clampView({ start: -100, end: 5000 }, bounds)).toEqual(bounds);
  });
});

describe("axisTicks", () => {
  it("lands on numbers a reader would say out loud", () => {
    const steps = axisTicks({ start: 0, end: 1000 }, 5).map((t) => t.at);
    expect(steps).toEqual([0, 200, 400, 600, 800, 1000]);
  });

  it("starts from the first round number inside the view", () => {
    const ticks = axisTicks({ start: 130, end: 530 }, 4);
    expect(ticks[0].at).toBe(200);
    expect(ticks[0].fraction).toBeCloseTo(0.175);
  });

  it("gives up rather than spinning on a degenerate viewport", () => {
    expect(axisTicks({ start: 0, end: 0 })).toEqual([]);
    expect(axisTicks({ start: 10, end: 0 })).toEqual([]);
    expect(axisTicks({ start: 0, end: 100 }, 0)).toEqual([]);
  });

  it("never returns an unbounded number of ticks", () => {
    expect(axisTicks({ start: 0, end: 1e9 }, 10_000).length).toBeLessThanOrEqual(65);
  });
});
