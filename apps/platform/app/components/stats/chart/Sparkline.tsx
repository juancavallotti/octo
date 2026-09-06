"use client";

import { useId } from "react";
import { downsample, extent, linearScale, pathFor, type Point } from "./scale";
import type { Points } from "./metrics";

/**
 * CPU and memory over a short window, in the space of a word.
 *
 * It answers one question — is this moving — and nothing else, so it has no axes,
 * no grid and no hover. The numbers printed beside it on the card are what say to
 * what; this says whether they are worth reading.
 *
 * The two lines are normalized **independently**, each to its own range within the
 * window. Cores and bytes share no scale, and in 28 pixels with no axis there is
 * nothing to mislabel: the shape is the whole message. That is also why the
 * anchor-at-zero rule the full chart uses is deliberately not applied here — a
 * memory line that varies by 2 MiB on a 120 MiB pod would be a flat line at the
 * top, which is true but says nothing.
 */

/** How many points survive into the path. Five minutes of one-second samples is
 * 300 of them for 120 pixels; the rest are bytes in a DOM attribute. */
const BUCKETS = 60;

export default function Sparkline({
  cpu,
  memory,
  width = 132,
  height = 28,
  label,
}: {
  cpu: Points;
  memory: Points;
  width?: number;
  height?: number;
  /** What a screen reader is told, since the shape itself is not available. */
  label: string;
}) {
  const clip = useId();
  const cpuPath = trace(cpu, width, height);
  const memPath = trace(memory, width, height);

  if (!cpuPath && !memPath) return null;

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      width={width}
      height={height}
      role="img"
      aria-label={label}
      className="overflow-visible"
    >
      <clipPath id={clip}>
        <rect x="0" y="0" width={width} height={height} />
      </clipPath>
      <g clipPath={`url(#${clip})`} fill="none" strokeWidth="1.25"
         strokeLinecap="round" strokeLinejoin="round">
        {memPath && <path d={memPath} className="stroke-violet-500/70" />}
        {cpuPath && <path d={cpuPath} className="stroke-sky-500" />}
      </g>
    </svg>
  );
}

/** Project one column onto the box, normalized to its own range. */
function trace(points: Points, width: number, height: number): string {
  const reduced = downsample(points.times, points.values, BUCKETS);
  const span = extent(reduced.values);
  if (!span) return "";

  // The window's own range, not one anchored at zero: see the note above. A flat
  // series still has to be a line rather than a division by zero, and it belongs
  // in the middle of the box rather than at the top of an invented range.
  const domain = span.min === span.max
    ? { min: span.min - 1, max: span.max + 1 }
    : span;

  const inset = 1.5; // room for the stroke, so a peak is not clipped in half
  const y = linearScale(domain, [height - inset, inset]);
  const last = reduced.times.length - 1;

  const projected: Point[] = reduced.times.map((_, i) => ({
    x: last === 0 ? width / 2 : (i / last) * width,
    y: reduced.values[i] === null ? null : y(reduced.values[i] as number),
  }));
  return pathFor(projected);
}
