import { describe, expect, it } from "vitest";
import { buildWaterfall } from "./buildWaterfall";
import { records } from "./fixtures";
import {
  axisTicks,
  barRect,
  clampScroll,
  fitView,
  flattenWaterfall,
  isFitted,
  MAX_ROWS,
  MAX_TRACK_PX,
  panBy,
  pxPerNs,
  rangeZoom,
  timeAt,
  trackWidth,
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

/** A second of trace in a 900px window: 120px/s puts it well inside the width. */
const SECOND = { spanNs: 1e9, containerPx: 900 };
/** A minute in the same window: 7200px of track, so it overflows and scrolls. */
const MINUTE = { spanNs: 60e9, containerPx: 900 };

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

describe("the time scale", () => {
  it("draws a trace ten times as long ten times as wide", () => {
    // The whole point of a constant density. Stretching each trace to the window
    // made a 100ms trace and a 30s trace the same picture, so a duration could
    // only ever be read as a ratio between siblings — never as a quantity, and
    // never compared across two traces.
    const ten = { spanNs: 600e9, containerPx: 900 };
    expect(trackWidth(1, ten) / trackWidth(1, MINUTE)).toBeCloseTo(10);
  });

  it("fills the window rather than drawing a short trace as a sliver", () => {
    // A floor, not a mode: a second at 120px/s would be 120px of an 900px
    // window, which is a chart nobody can read.
    expect(trackWidth(1, SECOND)).toBe(900);
    expect(trackWidth(1, MINUTE)).toBeCloseTo(7200, 6);
  });

  it("crosses between the two without a step", () => {
    // 7.5s is exactly where 120px/s reaches 900px, so the two rules have to
    // agree there or the chart jumps as a trace crosses it.
    const at = { spanNs: 7.5e9, containerPx: 900 };
    expect(trackWidth(1, at)).toBe(900);
    expect(pxPerNs(1, at)).toBeCloseTo(pxPerNs(1, MINUTE), 12);
  });

  it("will not lay out a track no browser should be asked to paint", () => {
    const hour = { spanNs: 3600e9, containerPx: 900 };
    expect(trackWidth(500, hour)).toBe(MAX_TRACK_PX);
    // And the zoom is clamped with it, or zooming back out feels dead for
    // several clicks while a factor that changed nothing counts back down.
    expect(zoomAt({ zoom: 1, scrollLeft: 0 }, 0, 1e6, hour).zoom).toBeLessThan(500);
  });
});

describe("barRect", () => {
  it("places a bar in pixels along the track", () => {
    const scale = pxPerNs(1, MINUTE);
    const bar = barRect({ start: 6e9, end: 12e9 }, scale);
    expect(bar.left).toBeCloseTo(720, 6);
    expect(bar.width).toBeCloseTo(720, 6);
  });

  it("keeps a span too short to see from disappearing", () => {
    // A 40µs block beside a 3s model call rounds to nothing, and nothing on
    // screen is indistinguishable from a block that never ran.
    expect(barRect({ start: 10, end: 10.0001 }, pxPerNs(1, MINUTE)).width).toBe(2);
  });

  it("lays out a bar that is off screen rather than dropping it", () => {
    // There is nothing to clip any more: the whole trace is laid out, and being
    // off *screen* is a scroll position rather than a fact about the geometry.
    const scale = pxPerNs(1, MINUTE);
    expect(barRect({ start: 55e9, end: 60e9 }, scale).left).toBeCloseTo(6600, 6);
  });
});

describe("the viewport", () => {
  it("zooms about the point under the cursor", () => {
    const before = timeAt(300, 1, MINUTE);
    const after = zoomAt({ zoom: 1, scrollLeft: 0 }, 300, 2, MINUTE);
    expect(timeAt(after.scrollLeft + 300, after.zoom, MINUTE)).toBeCloseTo(before, 6);
  });

  it("holds the cursor's moment when it is already scrolled along", () => {
    const view = { zoom: 4, scrollLeft: 5000 };
    const before = timeAt(view.scrollLeft + 200, view.zoom, MINUTE);
    const after = zoomAt(view, 200, 0.8, MINUTE);
    expect(timeAt(after.scrollLeft + 200, after.zoom, MINUTE)).toBeCloseTo(before, 6);
  });

  it("pans by what is on screen, and stops at the ends", () => {
    expect(panBy({ zoom: 1, scrollLeft: 0 }, 0.2, MINUTE).scrollLeft).toBe(180);
    expect(panBy({ zoom: 1, scrollLeft: 0 }, -1, MINUTE).scrollLeft).toBe(0);
    // 7200px of track in a 900px window leaves 6300px to scroll through.
    expect(panBy({ zoom: 1, scrollLeft: 0 }, 100, MINUTE).scrollLeft).toBeCloseTo(6300, 6);
  });

  it("cannot be scrolled off either end of the track", () => {
    expect(clampScroll(-500, 1, MINUTE)).toBe(0);
    expect(clampScroll(99_999, 1, MINUTE)).toBeCloseTo(6300, 6);
    // A trace that fits has nowhere to scroll to at all.
    expect(clampScroll(500, 1, SECOND)).toBe(0);
  });

  it("puts a dragged range across the whole window", () => {
    const view = rangeZoom(10e9, 20e9, MINUTE);
    expect(timeAt(view.scrollLeft, view.zoom, MINUTE)).toBeCloseTo(10e9, 0);
    expect(timeAt(view.scrollLeft + 900, view.zoom, MINUTE)).toBeCloseTo(20e9, 0);
  });

  it("fits the whole trace back on screen", () => {
    const view = fitView(MINUTE);
    expect(trackWidth(view.zoom, MINUTE)).toBeCloseTo(900, 6);
    expect(view.scrollLeft).toBe(0);
    expect(isFitted(view, MINUTE)).toBe(true);
  });

  it("knows a short trace is already fitted, so nothing offers to fit it", () => {
    expect(isFitted({ zoom: 1, scrollLeft: 0 }, SECOND)).toBe(true);
    expect(isFitted({ zoom: 1, scrollLeft: 0 }, MINUTE)).toBe(false);
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
