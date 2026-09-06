"use client";

import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import ChartTooltip from "./ChartTooltip";
import { downsample, extent, plotExtent, ticks, binaryStep, unionExtent, type Reading } from "./scale";
import { toRows, type Column } from "./rows";
import { AXIS, dotFor, GRID, LINE, seriesColor } from "./theme";

/**
 * One metric, small, on a single axis.
 *
 * The grid's chart: same library and the same domain rules as the overview, one
 * unit instead of two, and no legend — at this size the card's title and current
 * value carry that, and a hundred and eight legend entries would bury the chart.
 *
 * Points are downsampled before they are handed over. That is not only speed,
 * though a hundred and eight series of three hundred points is thirty thousand
 * of them for a card a few hundred pixels wide: it is also that every point past
 * about one per pixel lands somewhere already drawn.
 */

export interface MiniSeries {
  key: string;
  times: number[];
  values: Reading[];
}

/** Roughly one point per pixel of a card at the grid's widest. */
const BUCKETS = 320;

/** Past this, lines are drawn thinner and dimmer: a card carrying a hundred of
 * them is a shape rather than a set of readable series. */
const CROWDED = 12;

export default function MiniChart({
  series,
  fromMs,
  toMs,
  format,
  labelFormat,
  binary = false,
  anchorZero = true,
}: {
  series: MiniSeries[];
  fromMs: number;
  toMs: number;
  /** How a value is written, from the metric's inferred unit. */
  format: (value: number) => string;
  /** How a moment is written in the tooltip. */
  labelFormat: (at: number) => string;
  /** Step the axis in powers of 1024. Bytes are quoted in binary units, so a
   * decimal gridline reads as "95 MiB" where 100 MB was meant. */
  binary?: boolean;
  /** Whether zero is a meaningful floor. It is not for a unix timestamp. */
  anchorZero?: boolean;
}) {
  const reduced = series.map((s) => ({
    key: s.key,
    ...downsample(s.times, s.values, BUCKETS),
  }));
  const rows = toRows(reduced as Column[]);

  const domain = plotExtent(
    unionExtent(reduced.map((s) => extent(s.values))),
    anchorZero,
  );
  const crowded = reduced.length > CROWDED;

  return (
    <div className="h-24 w-full text-zinc-400">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows} margin={{ top: 6, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid {...GRID} />
          <XAxis dataKey="t" type="number" domain={[fromMs, toMs]} hide />
          <YAxis
            domain={[domain.min, domain.max]}
            ticks={ticks(domain.min, domain.max, 2, binary ? binaryStep : undefined)}
            tickFormatter={(at: number) => (at === 0 ? "0" : format(at))}
            width={52}
            {...AXIS}
          />
          <Tooltip
            cursor={{ stroke: "currentColor", strokeOpacity: 0.3 }}
            content={(props) => (
              <ChartTooltip {...props} format={(v) => format(v)} labelFormat={labelFormat} />
            )}
          />
          {reduced.map((s, i) => (
            <Line
              key={s.key}
              dataKey={s.key}
              name={s.key}
              stroke={seriesColor(crowded ? 0 : i)}
              strokeOpacity={crowded ? 0.45 : 1}
              {...LINE}
              dot={crowded ? false : dotFor(rows.length)}
              strokeWidth={crowded ? 0.75 : 1.25}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
