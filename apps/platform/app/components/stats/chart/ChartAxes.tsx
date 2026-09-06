"use client";

import { bytes } from "@/app/components/stats/Stat";
import { formatClock } from "./format";
import {
  axisDecimals,
  binaryStep,
  niceStep,
  ticks,
  timeTicks,
  type Extent,
  type Scale,
} from "./scale";

/**
 * The grid and the three axes.
 *
 * Each value axis is labelled in its own colour, matching the lines it governs.
 * That is the only thing keeping a two-unit chart readable: without it a reader
 * has to remember which side "0.4" belongs to, and the number that is easiest to
 * misread is the one that looks plausible on either axis.
 *
 * Only the left axis draws gridlines. Two sets of horizontal rules at different
 * heights would be a lattice, and neither would be worth following.
 */
export default function ChartAxes({
  cores,
  byteRange,
  yCores,
  yBytes,
  x,
  fromMs,
  toMs,
  left,
  plotWidth,
  baseline,
}: {
  cores: Extent;
  byteRange: Extent;
  yCores: Scale;
  yBytes: Scale;
  x: Scale;
  fromMs: number;
  toMs: number;
  left: number;
  plotWidth: number;
  /** Where the time labels sit, in the SVG's own coordinates. */
  baseline: number;
}) {
  const spanMs = toMs - fromMs;

  // The label precision comes from the gap between ticks, not from each value:
  // per-value formatting pairs "0.0000" with "0.250" on the same axis.
  const coreTicks = ticks(cores.min, cores.max, 4);
  const decimals = axisDecimals(niceStep((cores.max - cores.min) / 4));

  return (
    <g>
      {coreTicks.map((at) => (
        <g key={`c${at}`}>
          <line
            x1={left}
            x2={left + plotWidth}
            y1={yCores(at)}
            y2={yCores(at)}
            className="stroke-black/[0.07] dark:stroke-white/10"
          />
          <text
            x={left - 8}
            y={yCores(at)}
            textAnchor="end"
            dominantBaseline="middle"
            className="fill-sky-600 text-[10px] dark:fill-sky-400"
          >
            {at.toFixed(decimals)}
          </text>
        </g>
      ))}

      {ticks(byteRange.min, byteRange.max, 4, binaryStep).map((at) => (
        <text
          key={`b${at}`}
          x={left + plotWidth + 8}
          y={yBytes(at)}
          dominantBaseline="middle"
          className="fill-violet-600 text-[10px] dark:fill-violet-400"
        >
          {bytes(at)}
        </text>
      ))}

      {timeTicks(fromMs, toMs, 5).map((at) => (
        <text
          key={`t${at}`}
          x={x(at)}
          y={baseline}
          textAnchor="middle"
          className="fill-zinc-500 text-[10px]"
        >
          {formatClock(at, spanMs)}
        </text>
      ))}
    </g>
  );
}
