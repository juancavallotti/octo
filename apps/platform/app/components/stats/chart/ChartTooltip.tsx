"use client";


/**
 * The hover card, in the app's own clothes rather than Recharts' default.
 *
 * Two things it does that the default cannot. It formats every value through
 * the metric's own unit, so a tooltip says "127 MiB" and "5 KiB/s" rather than
 * 133173248 and 5017.6. And it caps the rows it lists: one metric on this page
 * carries a hundred and eight series, and a hover card with a hundred and eight
 * lines is a wall that covers the chart it is describing.
 */

const MAX_ROWS = 8;

/** Recharts hands the content renderer its own props; these are the two this
 * one adds. Kept apart so the render prop can spread the first and supply the
 * second. */
export interface TooltipFormatting {
  /** How a value is written. Takes the series key too, because one chart can
   * carry two units — cores on the left axis, bytes on the right. */
  format: (value: number, dataKey: string) => string;
  /** How the moment is written. */
  labelFormat: (at: number) => string;
}

/**
 * One line of the card. Typed structurally rather than imported from Recharts:
 * its tooltip props are generic over the value and name types and resolve
 * through context, which makes them awkward to satisfy from a render prop for
 * no benefit — these four fields are the whole contract.
 */
export interface TooltipEntry {
  // Recharts allows an accessor function here as well as a key; this chart only
  // ever passes strings, and String() below copes either way.
  dataKey?: string | number | ((row: never) => unknown);
  // Widened deliberately: Recharts types a value as possibly an array (for a
  // stacked or range series) and a name as its own union. This chart plots
  // neither, so the rows are filtered to numbers below and the rest is written
  // through String().
  name?: unknown;
  value?: unknown;
  color?: string;
}

export default function ChartTooltip({
  active,
  payload,
  label,
  format,
  labelFormat,
}: {
  active?: boolean;
  payload?: readonly TooltipEntry[];
  label?: string | number;
} & TooltipFormatting) {
  if (!active || !payload?.length) return null;

  const rows = payload.filter((entry) => typeof entry.value === "number");
  if (rows.length === 0) return null;

  const shown = rows.slice(0, MAX_ROWS);
  const hidden = rows.length - shown.length;

  return (
    <div className="rounded-lg border border-black/10 bg-white/95 px-2.5 py-1.5 text-xs shadow-lg backdrop-blur dark:border-white/15 dark:bg-zinc-900/95">
      <p className="mb-1 tabular-nums text-zinc-500">
        {labelFormat(Number(label))}
      </p>
      <ul className="flex flex-col gap-0.5">
        {shown.map((entry) => (
          <li key={key(entry)} className="flex items-baseline gap-2">
            <span
              aria-hidden
              className="inline-block h-2 w-2 shrink-0 rounded-full"
              style={{ background: entry.color }}
            />
            <span className="min-w-0 max-w-48 truncate text-zinc-500">
              {entry.name === undefined ? key(entry) : String(entry.name)}
            </span>
            <span className="ml-auto tabular-nums">
              {format(entry.value as number, key(entry))}
            </span>
          </li>
        ))}
      </ul>
      {hidden > 0 && (
        <p className="mt-1 text-[10px] text-zinc-400">and {hidden} more series</p>
      )}
    </div>
  );
}

/** A series' key as a string, whichever of the two shapes Recharts used. */
function key(entry: TooltipEntry): string {
  return typeof entry.dataKey === "function" ? "" : String(entry.dataKey ?? "");
}
