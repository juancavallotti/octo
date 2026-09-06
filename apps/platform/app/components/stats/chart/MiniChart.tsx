"use client";

import { useId } from "react";
import {
  axisDecimals,
  binaryStep,
  downsample,
  endTicks,
  extent,
  linearScale,
  niceStep,
  pathFor,
  plotExtent,
  unionExtent,
  type Point,
  type Reading,
} from "./scale";

/**
 * One metric, small, on a single axis.
 *
 * The grid version of LineChart: same geometry, one unit instead of two, and no
 * legend or hover — at this size the card's title and current value carry that.
 * Fixed width via a viewBox rather than a measured container, because fifty of
 * these on one page would be fifty ResizeObservers to draw something the grid
 * has already sized.
 *
 * Points are downsampled to roughly the pixel width. That is not only speed: a
 * hundred and eight series of three hundred points is thirty thousand path
 * commands for a chart two hundred pixels wide, where all but a few hundred of
 * them land on a pixel that is already dark.
 */

export interface MiniSeries {
  key: string;
  times: number[];
  values: Reading[];
}

const WIDTH = 320;
const HEIGHT = 96;
const PAD = { top: 6, right: 6, bottom: 4, left: 46 } as const;

/** Beyond this, lines are drawn thinner and dimmer: a card carrying a hundred
 * of them is a shape rather than a set of readable series. */
const CROWDED = 12;

export default function MiniChart({
  series,
  fromMs,
  toMs,
  format,
  binary = false,
  anchorZero = true,
}: {
  series: MiniSeries[];
  fromMs: number;
  toMs: number;
  /** How an axis value is written, from the metric's inferred unit. */
  format: (value: number) => string;
  /** Step the axis in powers of 1024. Bytes are quoted in binary units, so a
   * decimal gridline reads as "95 MiB" where 100 MB was meant. */
  binary?: boolean;
  /** Whether zero is a meaningful floor. It is not for a unix timestamp. */
  anchorZero?: boolean;
}) {
  const clip = useId();
  const plotWidth = WIDTH - PAD.left - PAD.right;
  const plotHeight = HEIGHT - PAD.top - PAD.bottom;

  const reduced = series.map((s) => ({
    key: s.key,
    ...downsample(s.times, s.values, plotWidth),
  }));

  const domain = plotExtent(unionExtent(reduced.map((s) => extent(s.values))), anchorZero);
  const x = linearScale({ min: fromMs, max: toMs }, [PAD.left, PAD.left + plotWidth]);
  const y = linearScale(domain, [PAD.top + plotHeight, PAD.top]);

  const crowded = reduced.length > CROWDED;
  const decimals = axisDecimals(niceStep((domain.max - domain.min) / 2));

  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-24 w-full"
      preserveAspectRatio="none"
      aria-hidden
    >
      <clipPath id={clip}>
        <rect x={PAD.left} y={PAD.top} width={plotWidth} height={plotHeight} />
      </clipPath>

      {endTicks(domain.min, domain.max, binary ? binaryStep : niceStep).map((at) => (
        <g key={at}>
          <line
            x1={PAD.left}
            x2={PAD.left + plotWidth}
            y1={y(at)}
            y2={y(at)}
            className="stroke-black/[0.06] dark:stroke-white/10"
            vectorEffect="non-scaling-stroke"
          />
          <text
            x={PAD.left - 5}
            y={y(at)}
            textAnchor="end"
            dominantBaseline="middle"
            className="fill-zinc-400 text-[9px]"
          >
            {label(at, format, decimals)}
          </text>
        </g>
      ))}

      <g
        clipPath={`url(#${clip})`}
        fill="none"
        strokeLinecap="round"
        strokeLinejoin="round"
        className={crowded ? "stroke-sky-500/40" : "stroke-sky-500"}
      >
        {reduced.map((s) => (
          <path
            key={s.key}
            d={project(s, x, y)}
            strokeWidth={crowded ? 0.6 : 1.25}
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </g>
    </svg>
  );
}

/**
 * An axis label. Formatted by the metric's unit, except at zero — "0 B" and
 * "0µs" are noise on a two-tick axis where the reader only needs the top number
 * to have a unit at all.
 */
function label(at: number, format: (value: number) => string, decimals: number): string {
  if (at === 0) return "0";
  const written = format(at);
  return written === "" ? at.toFixed(decimals) : written;
}

function project(
  s: { times: number[]; values: Reading[] },
  x: (v: number) => number,
  y: (v: number) => number,
): string {
  const points: Point[] = s.times.map((time, i) => {
    const value = s.values[i];
    return { x: x(time), y: value === null ? null : y(value) };
  });
  return pathFor(points);
}
