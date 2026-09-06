"use client";

import {
  CartesianGrid,
  Legend,
  Line,
  LineChart as RechartsLineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { bytes } from "@/app/components/stats/Stat";
import ChartTooltip from "./ChartTooltip";
import { formatClock, shortPod } from "./format";
import { formatCores, type Points } from "./metrics";
import { toRows, type Column } from "./rows";
import { binaryStep, extent, plotExtent, ticks, timeTicks, unionExtent } from "./scale";
import { AXIS, CPU_COLOR, dotFor, GRID, LINE, MEM_COLOR } from "./theme";

/**
 * CPU and memory for one deployment's pods, on one chart with two axes.
 *
 * Two units on one plot is a deliberate trade. It costs a reader the ability to
 * compare the heights of two lines — which they should never do here anyway —
 * and buys the thing the question actually needs: whether the memory climb and
 * the CPU spike happened at the same moment. Cores read against the left axis,
 * bytes against the right, and the axis labels are coloured to match the lines
 * they govern, which is the only thing keeping a two-unit chart readable.
 *
 * The domains and the tick positions are still ours rather than Recharts'. Its
 * defaults tick a byte axis decimally, which produces gridlines at 95 MiB where
 * 100 MB was meant, and it does not anchor a magnitude at zero — so a memory
 * line varying by 2 MiB on a 120 MiB pod is drawn full height and reads as a
 * crisis. See scale.ts.
 */

/** One pod's column of one metric. */
export interface PodSeries {
  pod: string;
  points: Points;
}

/** Pods are told apart by dash within a metric's colour, so a three-pod
 * deployment still reads as "CPU and memory" rather than six unrelated lines. */
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
  const columns: Column[] = [
    ...cpu.map((s) => ({ key: `cpu:${s.pod}`, ...s.points })),
    ...memory.map((s) => ({ key: `mem:${s.pod}`, ...s.points })),
  ];
  const rows = toRows(columns);
  const spanMs = toMs - fromMs;

  const cores = plotExtent(unionExtent(cpu.map((s) => extent(s.points.values))));
  const byteRange = plotExtent(unionExtent(memory.map((s) => extent(s.points.values))));

  return (
    <div className="h-64 w-full text-zinc-500 dark:text-zinc-400">
      <ResponsiveContainer width="100%" height="100%">
        <RechartsLineChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid {...GRID} />
          <XAxis
            dataKey="t"
            type="number"
            domain={[fromMs, toMs]}
            ticks={timeTicks(fromMs, toMs, 5)}
            tickFormatter={(at: number) => formatClock(at, spanMs)}
            {...AXIS}
          />
          <YAxis
            yAxisId="cores"
            domain={[cores.min, cores.max]}
            ticks={ticks(cores.min, cores.max, 4)}
            tickFormatter={formatCores}
            width={52}
            {...AXIS}
            tick={{ ...AXIS.tick, fill: CPU_COLOR }}
          />
          <YAxis
            yAxisId="bytes"
            orientation="right"
            domain={[byteRange.min, byteRange.max]}
            ticks={ticks(byteRange.min, byteRange.max, 4, binaryStep)}
            tickFormatter={bytes}
            width={64}
            {...AXIS}
            tick={{ ...AXIS.tick, fill: MEM_COLOR }}
          />
          <Tooltip
            cursor={{ stroke: "currentColor", strokeOpacity: 0.3 }}
            content={(props) => (
              <ChartTooltip
                {...props}
                // Two units on one chart, so the formatting is per series rather
                // than per chart: the card lists cores beside bytes.
                format={(value, dataKey) =>
                  dataKey.startsWith("cpu:") ? `${formatCores(value)} cores` : bytes(value)
                }
                labelFormat={(at) => formatClock(at, spanMs)}
              />
            )}
          />
          <Legend
            wrapperStyle={{ fontSize: 11, paddingTop: 4 }}
            iconSize={8}
            iconType="plainline"
          />

          {memory.map((s, i) => (
            <Line
              key={`mem:${s.pod}`}
              yAxisId="bytes"
              dataKey={`mem:${s.pod}`}
              name={`Memory ${shortPod(s.pod)}`}
              stroke={MEM_COLOR}
              strokeDasharray={DASHES[i % DASHES.length]}
              {...LINE}
              dot={dotFor(rows.length)}
            />
          ))}
          {cpu.map((s, i) => (
            <Line
              key={`cpu:${s.pod}`}
              yAxisId="cores"
              dataKey={`cpu:${s.pod}`}
              name={`CPU ${shortPod(s.pod)}`}
              stroke={CPU_COLOR}
              strokeDasharray={DASHES[i % DASHES.length]}
              {...LINE}
              dot={dotFor(rows.length)}
            />
          ))}
        </RechartsLineChart>
      </ResponsiveContainer>
    </div>
  );
}
