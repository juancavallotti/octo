"use client";

import { bytes } from "@/app/components/stats/Stat";
import { formatClock, shortPod } from "./format";
import { formatCores, type Points } from "./metrics";
import type { PodSeries } from "./LineChart";

/**
 * The values under the cursor, or the most recent ones when it is elsewhere.
 *
 * Above the chart rather than in a floating tooltip beside the pointer. With two
 * units and up to four pods there is too much to say in a hover card, and a
 * readout that is always on screen also serves the reader who never moves a
 * mouse — which on this page includes anyone reading it on a phone.
 */
export default function ChartReadout({
  cpu,
  memory,
  at,
  spanMs,
}: {
  cpu: PodSeries[];
  memory: PodSeries[];
  /** The hovered moment, or null for "whatever is latest". */
  at: number | null;
  spanMs: number;
}) {
  const pods = cpu.map((s) => s.pod);
  for (const s of memory) if (!pods.includes(s.pod)) pods.push(s.pod);

  return (
    <div className="mb-1 flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs">
      <span className="tabular-nums text-zinc-500">
        {at === null ? "latest" : formatClock(at, spanMs)}
      </span>
      {pods.map((pod, i) => (
        <span key={pod} className="flex items-baseline gap-1.5">
          <span
            aria-hidden
            className="inline-block h-2 w-2 shrink-0 rounded-full bg-sky-500"
            style={{ opacity: 1 - Math.min(i, 3) * 0.2 }}
          />
          <span className="max-w-40 truncate text-zinc-500" title={pod}>
            {shortPod(pod)}
          </span>
          <span className="tabular-nums text-sky-600 dark:text-sky-400">
            {formatCores(readAt(find(cpu, pod), at))}
          </span>
          <span className="tabular-nums text-violet-600 dark:text-violet-400">
            {byteText(readAt(find(memory, pod), at))}
          </span>
        </span>
      ))}
    </div>
  );
}

function find(all: PodSeries[], pod: string): Points | undefined {
  return all.find((s) => s.pod === pod)?.points;
}

/**
 * The reading nearest `at`, or the last real one when the cursor is away.
 *
 * Nearest rather than interpolated: every point here is a measurement, and
 * inventing one between two of them is the same fiction the null handling
 * everywhere else exists to prevent.
 */
function readAt(points: Points | undefined, at: number | null): number | null {
  if (!points || points.times.length === 0) return null;

  if (at === null) {
    for (let i = points.values.length - 1; i >= 0; i--) {
      const value = points.values[i];
      if (value !== null) return value;
    }
    return null;
  }

  let best = 0;
  let bestGap = Infinity;
  for (let i = 0; i < points.times.length; i++) {
    const gap = Math.abs(points.times[i] - at);
    if (gap < bestGap) {
      bestGap = gap;
      best = i;
    }
  }
  return points.values[best] ?? null;
}

function byteText(value: number | null): string {
  return value === null ? "—" : bytes(value);
}
