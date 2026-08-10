/**
 * Turning a waterfall tree into rows and rectangles.
 *
 * Kept apart from the components so zooming, panning and collapsing are ordinary
 * functions with ordinary tests, and so none of it refetches: the trace is
 * fetched once and every interaction is arithmetic over what is already here.
 *
 * All times are nanoseconds from the trace's origin, the same axis
 * `buildWaterfall` produced.
 */

import type { Interval } from "./timeSpans";
import type { WaterfallNode } from "./types";

/**
 * How many rows are rendered at once.
 *
 * There is no virtualization anywhere in this app, and a pathological trace — a
 * runaway foreach, a loop nobody meant to write — is exactly the one someone
 * opens to find out why. The cap is what keeps opening it from being a second
 * incident; saying how many rows it hid is what keeps the picture honest.
 */
export const MAX_ROWS = 2000;

/** The narrowest the viewport may be zoomed, so it cannot collapse to nothing. */
const MIN_SPAN_NS = 100;

/** One rendered row. */
export interface WaterfallRowModel {
  node: WaterfallNode;
  /** Whether this row can be collapsed — i.e. it holds something. */
  expandable: boolean;
  collapsed: boolean;
  /** Descendants hidden under it right now; 0 unless collapsed. */
  hidden: number;
}

export interface FlatWaterfall {
  rows: WaterfallRowModel[];
  /** Rows the cap kept out, which are not collapsed and cannot be reached. */
  cut: number;
  /** Every node that holds something, so "collapse all" needs no second walk. */
  collapsible: string[];
}

/**
 * Walk the tree into the rows a treegrid renders, honouring what is collapsed and
 * stopping at the cap.
 */
export function flattenWaterfall(
  roots: WaterfallNode[],
  collapsed: ReadonlySet<string>,
  limit: number = MAX_ROWS,
): FlatWaterfall {
  const rows: WaterfallRowModel[] = [];
  const collapsible: string[] = [];
  let cut = 0;

  const walk = (nodes: WaterfallNode[]) => {
    for (const node of nodes) {
      const expandable = node.children.length > 0;
      if (expandable) collapsible.push(node.id);

      if (rows.length >= limit) {
        cut += 1 + descendants(node);
        continue;
      }
      const isCollapsed = expandable && collapsed.has(node.id);
      rows.push({
        node,
        expandable,
        collapsed: isCollapsed,
        hidden: isCollapsed ? descendants(node) : 0,
      });
      if (expandable && !isCollapsed) walk(node.children);
    }
  };
  walk(roots);

  return { rows, cut, collapsible };
}

/** How many nodes sit under this one. */
function descendants(node: WaterfallNode): number {
  return node.children.reduce((total, child) => total + 1 + descendants(child), 0);
}

/**
 * Where a span's bar sits in the current viewport, as CSS percentages — or null
 * when it is entirely outside and should not be drawn.
 *
 * A bar that runs off the edge is clipped to the viewport rather than dropped:
 * the parent of what you zoomed into almost always starts before the view does,
 * and a row with no bar at all reads as a row with no time in it.
 */
export function barStyle(
  span: Interval,
  view: Interval,
): { left: string; width: string } | null {
  const viewSpan = view.end - view.start;
  if (viewSpan <= 0) return null;
  if (span.end < view.start || span.start > view.end) return null;

  const start = Math.max(span.start, view.start);
  const end = Math.min(span.end, view.end);
  const left = ((start - view.start) / viewSpan) * 100;
  // Never zero: a 40ns block beside a 3s model call would otherwise be invisible,
  // and invisible is indistinguishable from absent. The 2px floor is applied in
  // CSS, since it cannot be expressed in the same unit as the width.
  const width = Math.max(((end - start) / viewSpan) * 100, 0.15);
  return { left: `${left}%`, width: `${Math.min(width, 100 - left)}%` };
}

/** Hold a viewport inside the trace, preserving its width where possible. */
export function clampView(view: Interval, bounds: Interval): Interval {
  const total = bounds.end - bounds.start;
  const width = Math.min(Math.max(view.end - view.start, MIN_SPAN_NS), total);
  if (width >= total) return { ...bounds };

  const start = Math.min(Math.max(view.start, bounds.start), bounds.end - width);
  return { start, end: start + width };
}

/**
 * Zoom by `factor` about a point given as a fraction across the viewport, so the
 * thing under the cursor stays under the cursor. Factors below 1 zoom in.
 */
export function zoomAt(
  view: Interval,
  fraction: number,
  factor: number,
  bounds: Interval,
): Interval {
  const width = view.end - view.start;
  const focus = view.start + width * fraction;
  const next = Math.min(Math.max(width * factor, MIN_SPAN_NS), bounds.end - bounds.start);
  return clampView({ start: focus - next * fraction, end: focus - next * fraction + next }, bounds);
}

/** Slide the viewport by a fraction of its own width. */
export function panBy(view: Interval, fraction: number, bounds: Interval): Interval {
  const shift = (view.end - view.start) * fraction;
  return clampView({ start: view.start + shift, end: view.end + shift }, bounds);
}

/** One labelled gridline. */
export interface AxisTick {
  /** Offset in nanoseconds from the trace origin. */
  at: number;
  /** Position across the viewport, 0…1. */
  fraction: number;
}

/**
 * Gridlines at a round interval, chosen so a label reads as a number someone
 * would say out loud — 1, 2 or 5 times a power of ten.
 */
export function axisTicks(view: Interval, target = 6): AxisTick[] {
  const span = view.end - view.start;
  if (!(span > 0) || target < 1) return [];

  const step = niceStep(span / target);
  const ticks: AxisTick[] = [];
  // Guarded rather than trusted: a step that came out too small for the span
  // would spin here, and the axis is not worth a hung tab.
  for (let at = Math.ceil(view.start / step) * step; at <= view.end; at += step) {
    ticks.push({ at, fraction: (at - view.start) / span });
    if (ticks.length > 64) break;
  }
  return ticks;
}

/** The nearest 1/2/5 × 10ⁿ at or below `raw`. */
function niceStep(raw: number): number {
  if (!(raw > 0)) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const scaled = raw / magnitude;
  const step = scaled >= 5 ? 5 : scaled >= 2 ? 2 : 1;
  return step * magnitude;
}
