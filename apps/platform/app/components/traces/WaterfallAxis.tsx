"use client";

import { useEffect, useRef, useState } from "react";
import { axisTicks, rangeZoom, timeAt, type ChartContext, type Viewport } from "./chartLayout";
import { formatDuration } from "./format";

/**
 * The time ruler above the bars, and the surface you zoom with.
 *
 * Dragging a range is the primary gesture because it is the one that says what
 * you mean: "this part, from here to here". Wheel zoom is offered as a modifier
 * so that an ordinary scroll still scrolls — a chart that hijacks the wheel makes
 * a long trace unreadable, and now that the track scrolls sideways an unmodified
 * wheel is how someone pans it. Double-click restores the whole trace.
 *
 * Labels are offsets from the start of the trace rather than wall-clock times.
 * A trace is read as "what happened, in what order, and how long did each part
 * take", and every one of those questions is about elapsed time.
 *
 * The ruler spans the whole track, but ticks are computed for the *visible*
 * window only. At full zoom the track can be 200,000px wide, and a label every
 * 110px would be eighteen hundred of them for a ruler nobody can see more than a
 * screenful of.
 */
export default function WaterfallAxis({
  view,
  context,
  scale,
  trackPx,
  scroller,
  onChange,
  onFit,
}: {
  view: Viewport;
  context: ChartContext;
  /** Drawn pixels per nanosecond, which the parent already computed. */
  scale: number;
  trackPx: number;
  /** The element both the ruler and the rows scroll inside. */
  scroller: React.RefObject<HTMLDivElement | null>;
  onChange: (next: Viewport) => void;
  onFit: () => void;
}) {
  const track = useRef<HTMLDivElement>(null);
  // The drag in progress, in nanoseconds from the start of the trace.
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);

  // Panning must not re-render two thousand rows, so the scroll offset is not
  // state — it is read off the scroller here, where the ticks are the only thing
  // that depends on it, and coalesced to one frame.
  const [scrollLeft, setScrollLeft] = useState(0);
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    let queued = false;
    const onScroll = () => {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        setScrollLeft(element.scrollLeft);
      });
    };
    onScroll();
    element.addEventListener("scroll", onScroll, { passive: true });
    return () => element.removeEventListener("scroll", onScroll);
  }, [scroller]);

  /** Where a pointer is, in nanoseconds — the ruler's own coordinate. */
  const at = (clientX: number): number => {
    const box = track.current?.getBoundingClientRect();
    // getBoundingClientRect already accounts for the scroll, so this reads an
    // absolute position on the track with no offset bookkeeping.
    if (!box) return 0;
    return timeAt(
      Math.min(Math.max(clientX - box.left, 0), box.width),
      view.zoom,
      context,
    );
  };

  const onPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const t = at(e.clientX);
    setDrag({ from: t, to: t });
  };

  const onPointerUp = () => {
    if (!drag) return;
    setDrag(null);
    const [lo, hi] = [Math.min(drag.from, drag.to), Math.max(drag.from, drag.to)];
    // A drag too short to be a range was a click, and a click that zoomed
    // somewhere arbitrary is worse than a click that does nothing.
    if ((hi - lo) * scale < 8) return;
    onChange(rangeZoom(lo, hi, context));
  };

  const visible = {
    start: timeAt(scrollLeft, view.zoom, context),
    end: timeAt(scrollLeft + context.containerPx, view.zoom, context),
  };

  return (
    <div
      ref={track}
      role="presentation"
      style={{ width: trackPx }}
      onPointerDown={onPointerDown}
      onPointerMove={(e) => drag && setDrag({ ...drag, to: at(e.clientX) })}
      onPointerUp={onPointerUp}
      onPointerCancel={() => setDrag(null)}
      onDoubleClick={onFit}
      title="Drag to zoom into a range · double-click to fit the whole trace"
      className="relative h-6 shrink-0 cursor-col-resize select-none border-b border-black/10 dark:border-white/10"
    >
      {axisTicks(visible).map((tick) => (
        <span
          key={tick.at}
          style={{ left: tick.at * scale }}
          className="absolute top-0 h-full border-l border-black/[0.07] pl-1 pt-0.5 font-mono text-[10px] text-zinc-400 dark:border-white/[0.09]"
        >
          {formatDuration(tick.at)}
        </span>
      ))}

      {drag && (
        <div
          style={{
            left: Math.min(drag.from, drag.to) * scale,
            width: Math.abs(drag.to - drag.from) * scale,
          }}
          className="pointer-events-none absolute inset-y-0 bg-sky-500/25"
        />
      )}
    </div>
  );
}
