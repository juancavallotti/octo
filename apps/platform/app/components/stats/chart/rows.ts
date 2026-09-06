/**
 * Turning the API's columns into the rows a chart library wants.
 *
 * `/series` is columnar — parallel `times` and `values` arrays — because a
 * series is thousands of points and every repeated key is paid per point.
 * Recharts is row-oriented: one object per moment, one field per line. This is
 * the seam between them, and it is the only place the two shapes meet.
 *
 * Pods sample independently, so their timestamps do not line up. The rows are
 * the union of every series' moments, and a series with no reading at a moment
 * simply has no field there — which is what `connectNulls={false}` needs to see
 * to leave a gap. Filling those with zero would draw a cliff at every moment one
 * pod happened to scrape and another did not.
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
 * Merge columns into rows, ascending by time.
 *
 * A gap and an absence are both left undefined rather than distinguished. They
 * mean the same thing to a chart — nothing was measured here — and a null would
 * be plotted as zero by some renderers, which is the one outcome that has to be
 * impossible.
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
