"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  clampScroll,
  fitView,
  isFitted,
  panBy,
  pxPerNs,
  trackWidth,
  zoomAt,
  type ChartContext,
  type Viewport,
} from "./chartLayout";
import { LABEL_PX } from "./WaterfallRow";

/**
 * Where the reader is looking at the chart, and how wide the chart is drawn.
 *
 * Everything numeric is in chartLayout; what is here is the part that needs a
 * DOM — measuring the track, and putting the scroll where a zoom said it should
 * go.
 */

/**
 * What the track is assumed to be until it has been measured. A clientWidth of 0
 * is "no answer" rather than "no room"; left at 0 the scale would be 0 and every
 * bar would come out NaN.
 */
const ASSUMED_TRACK_PX = 800;

export interface ChartViewport {
  /** The element both the ruler and the rows scroll inside. */
  scroller: React.RefObject<HTMLDivElement | null>;
  context: ChartContext;
  view: Viewport;
  /** Drawn pixels per nanosecond. */
  scale: number;
  trackPx: number;
  /** Whether the whole trace is on screen, so nothing needs to offer to fit it. */
  fitted: boolean;
  apply: (next: Viewport) => void;
  pan: (direction: -1 | 1) => void;
  fit: () => void;
  /** Bring a row on screen vertically, without moving along the trace. */
  revealRow: (elementId: string) => void;
}

export function useChartViewport(spanNs: number): ChartViewport {
  const scroller = useRef<HTMLDivElement>(null);
  const [containerPx, setContainerPx] = useState(ASSUMED_TRACK_PX);

  // clientWidth rather than a bounding box, because it already excludes the
  // vertical scrollbar — which is the same reason the ruler has to live inside
  // the scroller rather than above it, or every bar would sit a few pixels off
  // the tick it is measured against.
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
    () => ({ spanNs, containerPx }),
    [spanNs, containerPx],
  );

  const [view, setView] = useState<Viewport>({ zoom: 1, scrollLeft: 0 });

  const apply = useCallback(
    (next: Viewport) =>
      setView({ ...next, scrollLeft: clampScroll(next.scrollLeft, next.zoom, context) }),
    [context],
  );

  // Applied after layout rather than beside the change that asked for it: the
  // scroll extents that clamp it do not exist until the track has been laid out
  // at its new width.
  useLayoutEffect(() => {
    const element = scroller.current;
    if (element) element.scrollLeft = view.scrollLeft;
  }, [view]);

  // The DOM owns the offset, not this state: a bare wheel, a scrollbar drag and
  // a trackpad swipe all move it without passing through `apply`, so that
  // panning does not re-render two thousand rows. A gesture therefore reads
  // where the chart actually is before deciding where to put it.
  const live = useCallback(
    (): Viewport => ({
      ...view,
      scrollLeft: scroller.current?.scrollLeft ?? view.scrollLeft,
    }),
    [view],
  );

  // Zoom on a modified wheel only; a bare wheel pans and scrolls the rows.
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
      const pointer = Math.max(e.clientX - box.left - LABEL_PX, 0);
      apply(zoomAt(live(), pointer, e.deltaY > 0 ? 0.8 : 1.25, context));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, [live, context, apply]);

  // Vertically only. A row is as wide as the whole track — tens of thousands of
  // pixels on a slow trace — and scrollIntoView on an element wider than the
  // scrollport aligns to its start, so unqualified every ArrowDown would snap
  // the chart back to time zero. The offset is saved and restored rather than
  // left to `inline: "nearest"`, which browsers ignore on an oversized element.
  const revealRow = useCallback((elementId: string) => {
    const element = document.getElementById(elementId);
    // scrollIntoView is absent in jsdom, and moving the cursor must not depend
    // on being able to scroll to it.
    if (!element?.scrollIntoView) return;
    const surface = scroller.current;
    const before = surface?.scrollLeft;
    element.scrollIntoView({ block: "nearest" });
    if (surface && before !== undefined) surface.scrollLeft = before;
  }, []);

  return {
    scroller,
    context,
    view,
    scale: pxPerNs(view.zoom, context),
    trackPx: trackWidth(view.zoom, context),
    fitted: isFitted(view, context),
    apply,
    pan: useCallback(
      (direction: -1 | 1) => apply(panBy(live(), direction * 0.2, context)),
      [apply, live, context],
    ),
    fit: useCallback(() => apply(fitView(context)), [apply, context]),
    revealRow,
  };
}
