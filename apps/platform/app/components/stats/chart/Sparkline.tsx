"use client";

import { Line, LineChart, ResponsiveContainer, YAxis } from "recharts";
import { extent } from "./scale";
import { toRows, type Column } from "./rows";
import { CPU_COLOR, dotFor, LINE, MEM_COLOR } from "./theme";
import type { Points } from "./metrics";

/**
 * CPU and memory over a short window, in the space of a word.
 *
 * It answers one question — is this moving — and nothing else, so it has no
 * axes, no grid, no legend and no tooltip. The numbers printed above it on the
 * card say to what; this says whether they are worth reading. Clicking it opens
 * the page that does have all of those.
 *
 * The two lines are scaled **independently**, each to its own range within the
 * window, which is what the two hidden axes are for. Cores and bytes share no
 * scale, and with no axis drawn there is nothing to mislabel: the shape is the
 * whole message. It is also why the anchor-at-zero rule the real charts use is
 * deliberately not applied — a memory line varying by 2 MiB on a 120 MiB pod
 * would be a flat line at the top, which is true and says nothing.
 */

export default function Sparkline({
  cpu,
  memory,
  label,
  className = "h-14 w-full",
}: {
  cpu: Points;
  memory: Points;
  /** What a screen reader is told, since the shape itself is not available. */
  label: string;
  className?: string;
}) {
  const columns: Column[] = [
    { key: "cpu", ...cpu },
    { key: "mem", ...memory },
  ];
  const rows = toRows(columns);
  if (rows.length === 0) return null;

  return (
    <div className={className} role="img" aria-label={label}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows} margin={{ top: 3, right: 1, bottom: 3, left: 1 }}>
          <YAxis yAxisId="cpu" hide domain={band(cpu)} />
          <YAxis yAxisId="mem" hide domain={band(memory)} />
          <Line yAxisId="mem" dataKey="mem" stroke={MEM_COLOR} strokeOpacity={0.7}
                {...LINE} strokeWidth={1.25} activeDot={false} dot={dotFor(rows.length)} />
          <Line yAxisId="cpu" dataKey="cpu" stroke={CPU_COLOR}
                {...LINE} strokeWidth={1.25} activeDot={false} dot={dotFor(rows.length)} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

/**
 * A series' own range, with a band around a flat one.
 *
 * A flat series has min === max, which Recharts scales to a zero-height domain
 * and draws as nothing at all. It belongs in the middle of the box rather than
 * at the top of an invented range, so the band is symmetric.
 */
function band(points: Points): [number, number] {
  const span = extent(points.values);
  if (!span) return [0, 1];
  if (span.min === span.max) return [span.min - 1, span.max + 1];
  return [span.min, span.max];
}
