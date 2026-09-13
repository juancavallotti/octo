/**
 * Turning `/series`' parallel `times` and `values` arrays into the row-per-moment
 * shape a chart wants — the one place the two shapes meet.
 *
 * Pods sample independently, so their timestamps do not line up. The rows are the
 * union of every series' moments, and a series with no reading at a moment has no
 * field there, which is what leaves a gap rather than a cliff down to zero.
 */

/** A reading, where null is a gap. */
export type Reading = number | null;

/** One named column of points. */
export interface Column {
  key: string;
  times: number[];
  values: Reading[];
}

/** One moment, with whatever was measured at it. */
export type Row = { t: number } & Record<string, number | undefined>;

/**
 * Merge columns into rows, ascending by time. A gap and an absence are both left
 * undefined: they mean the same thing here, and a null is plotted as zero by some
 * renderers.
 */
export function toRows(columns: ReadonlyArray<Column>): Row[] {
  if (columns.length === 0) return [];

  const rows = new Map<number, Row>();
  for (const column of columns) {
    const count = Math.min(column.times.length, column.values.length);
    for (let i = 0; i < count; i++) {
      const at = column.times[i];
      let row = rows.get(at);
      if (!row) {
        row = { t: at };
        rows.set(at, row);
      }
      const value = column.values[i];
      if (value !== null && Number.isFinite(value)) row[column.key] = value;
    }
  }

  return [...rows.values()].sort((a, b) => a.t - b.t);
}
