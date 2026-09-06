"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import ChartAxes from "./ChartAxes";
import { formatStep } from "./duration";
import ChartReadout from "./ChartReadout";
import { type Points } from "./metrics";
import {
  extent,
  linearScale,
  pathFor,
  plotExtent,
  unionExtent,
  type Point,
} from "./scale";

/**
 * CPU and memory for one deployment's pods, on one chart with two axes.
 *
 * Two units on one plot is a deliberate trade. It costs a reader the ability to
 * compare the heights of two lines — which they should never do here anyway — and
 * buys the thing the question actually needs: whether the memory climb and the CPU
 * spike happened at the same moment. Two stacked charts make that a saccade; one
 * chart makes it a glance. Cores read against the left axis, bytes against the
 * right, and the colours match the axis labels rather than a legend swatch alone.
 *
 * Hand-rolled SVG because this app has no charting library and one chart is not a
 * reason to adopt one. The geometry lives in `scale.ts`, which is tested without a
 * DOM; what is left here is projection and pointer handling.
 */

/** One pod's column of one metric. */
export interface PodSeries {
  pod: string;
  points: Points;
}

const HEIGHT = 260;
const PAD = { top: 12, right: 64, bottom: 26, left: 56 } as const;

/** Width to draw at before the container has been measured — in a test renderer
 * it never is, and a zero would make every projected point NaN. */
const ASSUMED_WIDTH = 800;

/** How pods are told apart within a metric's colour. Wraps rather than running
 * out; past four pods on one chart the legend is doing the work anyway. */
const DASHES = ["", "5 3", "1 3", "8 3 2 3"] as const;

export default function LineChart({
  cpu,
  memory,
  fromMs,
  toMs,
}: {
  cpu: PodSeries[];
  memory: PodSeries[];
  fromMs: number;
  toMs: number;
}) {
  const box = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(ASSUMED_WIDTH);
  const [hover, setHover] = useState<number | null>(null);
  const clip = useId();

  useEffect(() => {
    const element = box.current;
    if (!element) return;
    const observer = new ResizeObserver(() => {
      // clientWidth rather than the rect, so a scrollbar is not drawn over.
      setWidth(Math.max(element.clientWidth, 240));
    });
    observer.observe(element);
    setWidth(Math.max(element.clientWidth || ASSUMED_WIDTH, 240));
    return () => observer.disconnect();
  }, []);

  const plotWidth = Math.max(width - PAD.left - PAD.right, 1);
  const plotHeight = HEIGHT - PAD.top - PAD.bottom;

  const coreDomain = plotExtent(unionExtent(cpu.map((s) => extent(s.points.values))));
  const byteDomain = plotExtent(unionExtent(memory.map((s) => extent(s.points.values))));

  const x = linearScale({ min: fromMs, max: toMs }, [PAD.left, PAD.left + plotWidth]);
  const yCores = linearScale(coreDomain, [PAD.top + plotHeight, PAD.top]);
  const yBytes = linearScale(byteDomain, [PAD.top + plotHeight, PAD.top]);

  const onMove = useCallback(
    (event: React.PointerEvent<SVGSVGElement>) => {
      const rect = event.currentTarget.getBoundingClientRect();
      const px = event.clientX - rect.left;
      if (px < PAD.left || px > PAD.left + plotWidth) {
        setHover(null);
        return;
      }
      const ratio = (px - PAD.left) / plotWidth;
      setHover(fromMs + ratio * (toMs - fromMs));
    },
    [fromMs, toMs, plotWidth],
  );

  const spanMs = toMs - fromMs;

  return (
    <div ref={box} className="w-full">
      <ChartReadout cpu={cpu} memory={memory} at={hover} spanMs={spanMs} />
      <svg
        viewBox={`0 0 ${width} ${HEIGHT}`}
        width={width}
        height={HEIGHT}
        role="img"
        aria-label={`CPU and memory over the last ${formatStep(spanMs)}`}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        <clipPath id={clip}>
          <rect x={PAD.left} y={PAD.top} width={plotWidth} height={plotHeight} />
        </clipPath>

        <ChartAxes
          cores={coreDomain}
          byteRange={byteDomain}
          yCores={yCores}
          yBytes={yBytes}
          x={x}
          fromMs={fromMs}
          toMs={toMs}
          left={PAD.left}
          plotWidth={plotWidth}
          baseline={HEIGHT - 8}
        />

        {hover !== null && (
          <line
            x1={x(hover)}
            x2={x(hover)}
            y1={PAD.top}
            y2={PAD.top + plotHeight}
            className="stroke-black/25 dark:stroke-white/30"
          />
        )}

        <g clipPath={`url(#${clip})`} fill="none" strokeWidth="1.5"
           strokeLinecap="round" strokeLinejoin="round">
          {memory.map((s, i) => (
            <path
              key={`m${s.pod}`}
              d={project(s.points, x, yBytes)}
              strokeDasharray={DASHES[i % DASHES.length]}
              className="stroke-violet-500"
            />
          ))}
          {cpu.map((s, i) => (
            <path
              key={`c${s.pod}`}
              d={project(s.points, x, yCores)}
              strokeDasharray={DASHES[i % DASHES.length]}
              className="stroke-sky-500"
            />
          ))}
        </g>
      </svg>
    </div>
  );
}

/** Project one column onto the plot. */
function project(points: Points, x: (v: number) => number, y: (v: number) => number): string {
  const projected: Point[] = points.times.map((time, i) => {
    const value = points.values[i];
    return { x: x(time), y: value === null ? null : y(value) };
  });
  return pathFor(projected);
}
