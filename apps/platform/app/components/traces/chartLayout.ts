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

import { branchUnder } from "./blockPath";
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

/**
 * How wide one second of trace is drawn at zoom 1.
 *
 * The chart used to stretch whatever it was given to fill the container, which
 * meant a 100ms trace and a 30s trace were drawn identically. Duration read as a
 * ratio between siblings and never as a quantity, and two traces could not be
 * compared by looking at them. At a constant density a 10s trace is exactly ten
 * times the width of a 1s one, which is the claim a time axis is supposed to
 * make.
 */
const BASE_PX_PER_SECOND = 120;
const BASE_SCALE = BASE_PX_PER_SECOND / 1e9;

/**
 * How wide the track is allowed to get.
 *
 * Browsers cope with far more than this; layout and paint do not, and a
 * twenty-minute trace zoomed in would ask for tens of millions of pixels. It
 * clamps the zoom as well as the width — see {@link maxZoom} — because a factor
 * that no longer changes the geometry still has to stop counting up, or zooming
 * back out feels dead for several clicks.
 */
export const MAX_TRACK_PX = 200_000;

export const MIN_ZOOM = 0.02;
export const MAX_ZOOM = 500;

/** Everything the geometry needs that is not the zoom itself. */
export interface ChartContext {
  /** How long the whole trace was, in nanoseconds. */
  spanNs: number;
  /** How much room the track has on screen, in pixels. */
  containerPx: number;
}

/** Where the reader is: how far in, and how far along. */
export interface Viewport {
  zoom: number;
  scrollLeft: number;
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

/**
 * The smallest zoom this trace allows.
 *
 * MIN_ZOOM, unless the zoom that fits the whole trace is smaller — a floor above
 * the fit zoom would leave Fit unable to fit. An hour-long trace needs 0.002 to
 * come back on screen, and a flat 0.02 floor would have refused it and left the
 * button doing nothing.
 */
function minZoom({ spanNs, containerPx }: ChartContext): number {
  if (!(spanNs > 0) || !(containerPx > 0)) return MIN_ZOOM;
  return Math.min(MIN_ZOOM, containerPx / (spanNs * BASE_SCALE));
}

/** The largest zoom that still fits inside {@link MAX_TRACK_PX}. */
export function maxZoom(context: ChartContext): number {
  if (!(context.spanNs > 0)) return MAX_ZOOM;
  return clamp(
    MAX_TRACK_PX / (context.spanNs * BASE_SCALE),
    minZoom(context),
    MAX_ZOOM,
  );
}

export function clampZoom(zoom: number, context: ChartContext): number {
  if (!Number.isFinite(zoom)) return 1;
  return clamp(zoom, minZoom(context), maxZoom(context));
}

/**
 * How wide to draw the whole trace.
 *
 * The `max` against the container is the whole fit-versus-constant rule, and it
 * is a floor rather than a switch: a trace too short to fill the window fills it,
 * and everything longer honours the constant density and overflows. There is no
 * threshold to tune and no discontinuity to cross — with a 900px track the
 * crossover simply falls around 7.5 seconds.
 */
export function trackWidth(zoom: number, { spanNs, containerPx }: ChartContext): number {
  if (!(spanNs > 0)) return Math.max(containerPx, 0);
  const constant = spanNs * BASE_SCALE * clampZoom(zoom, { spanNs, containerPx });
  return clamp(Math.max(constant, containerPx), 0, MAX_TRACK_PX);
}

/** Drawn pixels per nanosecond at this zoom. */
export function pxPerNs(zoom: number, context: ChartContext): number {
  return context.spanNs > 0 ? trackWidth(zoom, context) / context.spanNs : 0;
}

/** Hold a scroll offset inside the track. */
export function clampScroll(
  scrollLeft: number,
  zoom: number,
  context: ChartContext,
): number {
  const overflow = trackWidth(zoom, context) - context.containerPx;
  return clamp(scrollLeft, 0, Math.max(overflow, 0));
}

/** The moment drawn at `px` from the start of the track. */
export function timeAt(px: number, zoom: number, context: ChartContext): number {
  const scale = pxPerNs(zoom, context);
  return scale > 0 ? px / scale : 0;
}

/**
 * Zoom about a point given in pixels across the *visible* track, so whatever the
 * cursor is over stays under the cursor.
 */
export function zoomAt(
  view: Viewport,
  pointerPx: number,
  factor: number,
  context: ChartContext,
): Viewport {
  const at = timeAt(view.scrollLeft + pointerPx, view.zoom, context);
  const zoom = clampZoom(view.zoom * factor, context);
  return {
    zoom,
    scrollLeft: clampScroll(at * pxPerNs(zoom, context) - pointerPx, zoom, context),
  };
}

/** Slide the view by a fraction of what is on screen. */
export function panBy(
  view: Viewport,
  fraction: number,
  context: ChartContext,
): Viewport {
  return {
    ...view,
    scrollLeft: clampScroll(
      view.scrollLeft + fraction * context.containerPx,
      view.zoom,
      context,
    ),
  };
}

/** The view that puts `[from, to]` across the whole visible track. */
export function rangeZoom(from: number, to: number, context: ChartContext): Viewport {
  const width = to - from;
  if (!(width > 0) || !(context.containerPx > 0)) return fitView(context);
  const zoom = clampZoom(context.containerPx / (width * BASE_SCALE), context);
  return { zoom, scrollLeft: clampScroll(from * pxPerNs(zoom, context), zoom, context) };
}

/** The whole trace, across the whole track: what Escape and Fit go back to. */
export function fitView(context: ChartContext): Viewport {
  const { spanNs, containerPx } = context;
  if (!(spanNs > 0) || !(containerPx > 0)) return { zoom: 1, scrollLeft: 0 };
  return { zoom: clampZoom(containerPx / (spanNs * BASE_SCALE), context), scrollLeft: 0 };
}

/** Whether this view is showing the whole trace at once. */
export function isFitted(view: Viewport, context: ChartContext): boolean {
  return (
    view.scrollLeft <= 0.5 &&
    trackWidth(view.zoom, context) <= context.containerPx + 0.5
  );
}

/**
 * Blocks whose branches are the names of tools an agent may call.
 *
 * There is no tool trace kind and no `tool` attribute — the runtime builds each
 * tool as a branch of the agent block, so `orders.assistant[search_docs].fetch`
 * is what a tool call looks like on the wire. A branch alone means nothing (`if`
 * yields `then`/`else`), which is why this has to be read against the block type
 * of the ancestor that owns it.
 */
const TOOL_HOSTS: ReadonlySet<string> = new Set(["ai-agent", "mcp-router"]);

/** One rendered row. */
export interface WaterfallRowModel {
  node: WaterfallNode;
  /** Whether this row can be collapsed — i.e. it holds something. */
  expandable: boolean;
  collapsed: boolean;
  /** Descendants hidden under it right now; 0 unless collapsed. */
  hidden: number;
  /** The agent tool this row ran inside, or null when it ran outside one. */
  tool: string | null;
  /** Whether this row is the top of that tool's subtree, which is where it is named. */
  toolRoot: boolean;
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

  // The tool context is threaded down the walk rather than recovered per row:
  // this is already the one pass that visits every node with its parent in hand,
  // and a row's tool is a fact about the branch it entered on the way here.
  const walk = (nodes: WaterfallNode[], host: WaterfallNode | null, tool: string | null) => {
    for (const node of nodes) {
      const expandable = node.children.length > 0;
      if (expandable) collapsible.push(node.id);

      // Only asked once per tool: every node under `assistant[search_docs]`
      // still starts with that prefix, so without the `tool === null` guard the
      // whole subtree would each claim to be the row where the tool begins.
      const entered = host && tool === null ? branchUnder(node.path, host.path) : null;
      const inTool = entered ?? tool;

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
        tool: inTool,
        toolRoot: entered !== null,
      });
      if (expandable && !isCollapsed) {
        // A tool's own subtree is not a new host, so an agent nested inside a
        // tool starts its own naming and an ordinary block inside one keeps the
        // tool it is under.
        const nextHost = TOOL_HOSTS.has(node.record?.blockType ?? "") ? node : host;
        walk(node.children, nextHost, nextHost === host ? inTool : null);
      }
    }
  };
  walk(roots, null, null);

  return { rows, cut, collapsible };
}

/** How many nodes sit under this one. */
function descendants(node: WaterfallNode): number {
  return node.children.reduce((total, child) => total + 1 + descendants(child), 0);
}

/**
 * Where a span's bar sits on the track, in pixels from its start.
 *
 * The whole trace is laid out now, so there is nothing to clip and nothing to
 * drop: a bar off *screen* is a scroll position, not a fact about the geometry.
 * What survives is the floor — a 40ns block beside a 3s model call would round
 * to nothing, and invisible is indistinguishable from absent.
 */
export function barRect(
  span: Interval,
  scale: number,
): { left: number; width: number } {
  const left = span.start * scale;
  return { left, width: Math.max((span.end - span.start) * scale, 2) };
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
