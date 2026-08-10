"use client";

import { useRef, useState } from "react";
import type { Interval } from "./timeSpans";
import { axisTicks, clampView } from "./chartLayout";
import { formatDuration } from "./format";

/**
 * The time ruler above the bars, and the surface you zoom with.
 *
 * Dragging a range is the primary gesture because it is the one that says what
 * you mean: "this part, from here to here". Wheel zoom is offered as a modifier
 * so that an ordinary scroll still scrolls the rows — a chart that hijacks the
 * wheel makes a long trace unreadable. Double-click restores the whole trace.
 *
 * Labels are offsets from the start of the trace rather than wall-clock times.
 * A trace is read as "what happened, in what order, and how long did each part
 * take", and every one of those questions is about elapsed time.
 */
export default function WaterfallAxis({
  view,
  bounds,
  onChange,
}: {
  view: Interval;
  bounds: Interval;
  onChange: (next: Interval) => void;
}) {
  const track = useRef<HTMLDivElement>(null);
  // The drag in progress, as fractions across the track.
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);

  /** Where a pointer is, as a fraction across the track. */
  const fractionAt = (clientX: number): number => {
    const box = track.current?.getBoundingClientRect();
    if (!box || box.width === 0) return 0;
    return Math.min(Math.max((clientX - box.left) / box.width, 0), 1);
  };

  const onPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const at = fractionAt(e.clientX);
    setDrag({ from: at, to: at });
  };

  const onPointerMove = (e: React.PointerEvent) => {
    if (!drag) return;
    setDrag({ ...drag, to: fractionAt(e.clientX) });
  };

  const onPointerUp = () => {
    if (!drag) return;
    setDrag(null);
    const [lo, hi] = [Math.min(drag.from, drag.to), Math.max(drag.from, drag.to)];
    // A drag too short to be a range was a click, and a click that zoomed
    // somewhere arbitrary is worse than a click that does nothing.
    if (hi - lo < 0.01) return;
    const span = view.end - view.start;
    onChange(
      clampView(
        { start: view.start + span * lo, end: view.start + span * hi },
        bounds,
      ),
    );
  };

  const selection = drag
    ? {
        left: `${Math.min(drag.from, drag.to) * 100}%`,
        width: `${Math.abs(drag.to - drag.from) * 100}%`,
      }
    : null;

  return (
    <div
      ref={track}
      role="presentation"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={() => setDrag(null)}
      onDoubleClick={() => onChange({ ...bounds })}
      title="Drag to zoom into a range · double-click to fit the whole trace"
      className="relative h-6 cursor-col-resize select-none border-b border-black/10 dark:border-white/10"
    >
      {axisTicks(view).map((tick) => (
        <span
          key={tick.at}
          style={{ left: `${tick.fraction * 100}%` }}
          className="absolute top-0 h-full border-l border-black/[0.07] pl-1 pt-0.5 font-mono text-[10px] text-zinc-400 dark:border-white/[0.09]"
        >
          {formatDuration(tick.at)}
        </span>
      ))}

      {selection && (
        <div
          style={selection}
          className="pointer-events-none absolute inset-y-0 bg-sky-500/25"
        />
      )}
    </div>
  );
}
