"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { TriangleAlert } from "lucide-react";
import {
  clampScroll,
  fitView,
  flattenWaterfall,
  isFitted,
  panBy,
  pxPerNs,
  trackWidth,
  zoomAt,
  type ChartContext,
  type Viewport,
} from "./chartLayout";
import type { Waterfall as WaterfallModel, WaterfallNode } from "./types";
import WaterfallAxis from "./WaterfallAxis";
import WaterfallRow, { LABEL_PX } from "./WaterfallRow";
import { rowElementId, useTreegridKeys } from "./useTreegridKeys";

/**
 * The chart: everything that happened on one trace, nested as it ran.
 *
 * Time is drawn at a constant density — a fixed number of pixels per second —
 * rather than stretched to whatever width was available. Stretching meant a
 * 100ms trace and a 30s trace were drawn identically: duration read as a ratio
 * between siblings and never as a quantity, and two traces could not be compared
 * by looking at them. Now a long execution is a long chart, and it scrolls.
 *
 * The one exception is a trace too short to fill the window, which fills it
 * anyway. That is a floor rather than a mode — see trackWidth — so there is no
 * threshold to cross and nothing that changes behaviour partway.
 *
 * The viewport is state and the tree is not: zooming, panning and collapsing are
 * arithmetic over a value that was built once, so none of them refetch.
 *
 * There is no virtualization, here or anywhere else in this app. A trace with
 * thousands of spans is pathological — a runaway foreach, a loop nobody meant to
 * write — and it is exactly the trace someone opens to find out why. The cap is
 * what keeps opening it from being a second incident, and the notice above the
 * rows is what keeps it from looking complete when it is not.
 */

/**
 * What the track is assumed to be until it has been measured.
 *
 * Only reached in a non-visual renderer, where clientWidth is 0 — and a zero
 * there is not "no room", it is "no answer". Left as 0 the scale would be 0 and
 * every bar would come out NaN.
 */
const ASSUMED_TRACK_PX = 800;

export default function Waterfall({
  waterfall,
  selectedId,
  onSelect,
}: {
  waterfall: WaterfallModel;
  selectedId: string | null;
  onSelect: (node: WaterfallNode) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [containerPx, setContainerPx] = useState(ASSUMED_TRACK_PX);

  // clientWidth rather than a bounding box, because it already excludes the
  // vertical scrollbar — which is the same reason the ruler has to live inside
  // this scroller rather than above it. Outside, it would keep the container's
  // full width while the rows lost a scrollbar's worth of it, and every bar
  // would sit a few pixels off the tick it is measured against.
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const measure = () => {
      const width = element.clientWidth - LABEL_PX;
      if (width > 0) setContainerPx(width);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const context = useMemo<ChartContext>(
    () => ({ spanNs: waterfall.spanNs, containerPx }),
    [waterfall.spanNs, containerPx],
  );

  // A different trace is a different chart: the viewport and what was folded
  // away belong to the trace they were chosen on. That reset is done by keying
  // this component on the trace (see TraceDetail) rather than by an effect that
  // clears state after the fact — the state simply never carries over.
  const [view, setView] = useState<Viewport>({ zoom: 1, scrollLeft: 0 });
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  const trackPx = trackWidth(view.zoom, context);
  const scale = pxPerNs(view.zoom, context);

  // Where the view is *asked* to be. Applied after layout rather than beside the
  // zoom change, because the scroll extents that clamp it do not exist until the
  // track has been laid out at the new width. Every gesture funnels through here.
  const applyView = useCallback(
    (next: Viewport) => setView({ ...next, scrollLeft: clampScroll(next.scrollLeft, next.zoom, context) }),
    [context],
  );

  useLayoutEffect(() => {
    const element = scroller.current;
    if (element) element.scrollLeft = view.scrollLeft;
  }, [view]);

  const fit = useCallback(() => applyView(fitView(context)), [applyView, context]);

  const { rows, cut, collapsible } = useMemo(
    () => flattenWaterfall(waterfall.roots, collapsed),
    [waterfall.roots, collapsed],
  );

  const toggle = useCallback((id: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }, []);

  // Zoom on a modified wheel only. A bare wheel now pans and scrolls the rows,
  // which is what someone reaching for it on a chart wider than the window wants
  // — and a trackpad's horizontal delta pans for free.
  //
  // Registered by hand rather than with an onWheel prop because React attaches
  // wheel listeners passively: preventDefault() from a synthetic handler is
  // ignored, so the page would zoom *and* scroll at once. A native listener with
  // { passive: false } is the only way to hold the scroll still.
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey && !e.altKey) return;
      e.preventDefault();
      const box = element.getBoundingClientRect();
      const pointer = e.clientX - box.left - LABEL_PX;
      applyView(zoomAt(view, Math.max(pointer, 0), e.deltaY > 0 ? 0.8 : 1.25, context));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, [view, context, applyView]);

  const fitted = isFitted(view, context);

  // Arrow keys walk and fold the tree, shifted arrows pan, Escape fits. Scoped
  // to the chart rather than to the document: Escape belongs to whatever the
  // reader is actually in, and a chart that claims it globally takes it from
  // every dialog on the page.
  const keys = useTreegridKeys(
    rows,
    (row) => onSelect(row.node),
    (row) => toggle(row.node.id),
    useCallback(
      (direction: -1 | 1) => applyView(panBy(view, direction * 0.2, context)),
      [applyView, view, context],
    ),
    fit,
  );

  // Keep the row the keyboard is on in view — vertically only. A row is as wide
  // as the whole track, tens of thousands of pixels on a slow trace, and
  // scrollIntoView on an element wider than the scrollport aligns to its start:
  // unqualified, every ArrowDown would snap the chart back to time zero. Saved
  // and restored rather than passed as `inline: "nearest"`, which browsers still
  // honour on an oversized element.
  useEffect(() => {
    const surface = scroller.current;
    if (!keys.activeId) return;
    const element = document.getElementById(keys.activeId);
    // Optional right through: scrollIntoView is a browser convenience, absent in
    // jsdom, and moving the cursor must not depend on being able to scroll to it.
    if (!element?.scrollIntoView) return;
    const before = surface?.scrollLeft;
    element.scrollIntoView({ block: "nearest" });
    if (surface && before !== undefined) surface.scrollLeft = before;
  }, [keys.activeId]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {waterfall.warnings.length > 0 && (
        <ul className="space-y-1 px-3 py-2">
          {waterfall.warnings.map((warning) => (
            <li
              key={warning.kind}
              className="flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400"
            >
              <TriangleAlert size={12} className="mt-0.5 shrink-0" />
              {warning.message}
            </li>
          ))}
        </ul>
      )}

      <div className="flex items-center gap-2 px-3 pb-1 text-xs text-zinc-500 dark:text-zinc-400">
        <span>
          {waterfall.count} span{waterfall.count === 1 ? "" : "s"}
        </span>
        {cut > 0 && (
          <span className="text-amber-600 dark:text-amber-400">
            {cut} more not drawn — collapse a branch to reach them
          </span>
        )}
        <span className="ml-auto flex items-center gap-2">
          {collapsible.length > 0 && (
            <button
              type="button"
              onClick={() =>
                setCollapsed((prev) => (prev.size ? new Set() : new Set(collapsible)))
              }
              className="rounded px-1.5 py-0.5 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]"
            >
              {collapsed.size ? "Expand all" : "Collapse all"}
            </button>
          )}
          {!fitted && (
            <button
              type="button"
              onClick={fit}
              className="rounded px-1.5 py-0.5 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]"
            >
              Fit (Esc)
            </button>
          )}
        </span>
      </div>

      {/* One scroller for both axes. The ruler is sticky at the top *inside* it
          so it scrolls sideways in lockstep with the bars it measures, and the
          corner above the pinned name column is sticky in both directions so
          neither the ruler nor a row slides out from under it. */}
      <div
        ref={scroller}
        className="min-h-0 flex-1 overflow-auto overscroll-x-contain"
      >
        <div style={{ width: LABEL_PX + trackPx }}>
          <div className="sticky top-0 z-20 flex bg-white dark:bg-zinc-900">
            <div
              style={{ width: LABEL_PX }}
              className="sticky left-0 z-30 shrink-0 border-b border-black/10 bg-white dark:border-white/10 dark:bg-zinc-900"
            />
            <WaterfallAxis
              view={view}
              context={context}
              scale={scale}
              trackPx={trackPx}
              scroller={scroller}
              onChange={applyView}
              onFit={fit}
            />
          </div>

          <div
            role="treegrid"
            aria-label="Trace waterfall"
            aria-rowcount={waterfall.count}
            aria-activedescendant={keys.activeId}
            onKeyDown={keys.onKeyDown}
            tabIndex={0}
            className="outline-none focus-visible:ring-1 focus-visible:ring-sky-500/40"
          >
            {rows.map((row) => (
              <WaterfallRow
                key={row.node.id}
                row={row}
                scale={scale}
                trackPx={trackPx}
                selected={row.node.id === selectedId}
                active={rowElementId(row.node.id) === keys.activeId}
                onSelect={() => onSelect(row.node)}
                onToggle={() => toggle(row.node.id)}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
