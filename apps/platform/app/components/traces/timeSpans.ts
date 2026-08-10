/**
 * The interval arithmetic a waterfall is drawn from. No domain knowledge, no
 * React — just the handful of operations that turn timestamps into geometry.
 *
 * ## Why nanoseconds, and why relative
 *
 * The runtime measures in nanoseconds and a `transform` block runs in tens of
 * microseconds, so a millisecond is not a small unit here — it is larger than
 * whole blocks. Rounding to one would collapse a chain of fast blocks onto a
 * single instant, and nesting them would come down to a coin flip.
 *
 * Absolute epoch nanoseconds do not fit a JS number — 1.8e18 against a safe
 * integer ceiling of 9e15 — so an instant is kept as whole milliseconds plus the
 * nanoseconds inside that millisecond, and only ever multiplied out **relative to
 * the trace's own origin**. A trace lasting a hundred days still lands inside the
 * safe range once the epoch is subtracted off, and every number in this module
 * and in the waterfall is that offset.
 */

const NS_PER_MS = 1_000_000;

/** An instant, split so its two halves each stay exact in a JS number. */
export interface Instant {
  /** Whole epoch milliseconds. */
  ms: number;
  /** Nanoseconds within that millisecond, 0…999 999. */
  ns: number;
}

/**
 * Read an RFC3339 timestamp, or null when it is not one.
 *
 * `Date.parse` stops at milliseconds, so the sub-millisecond digits are read off
 * the string and kept separately. Postgres hands back microseconds and the
 * runtime measures nanoseconds; both survive this.
 */
export function parseInstant(ts: string): Instant | null {
  const ms = Date.parse(ts);
  if (Number.isNaN(ms)) return null;
  // Digits 4..9 of the fractional second — everything Date.parse discarded.
  const fraction = /[.,](\d+)/.exec(ts)?.[1] ?? "";
  return { ms, ns: Number(fraction.slice(3, 9).padEnd(6, "0")) };
}

/** Order two instants. */
export function compareInstants(a: Instant, b: Instant): number {
  return a.ms - b.ms || a.ns - b.ns;
}

/** The instant `ns` nanoseconds before `at` — how a span's start is derived from
 * the completion the runtime stamped it at. */
export function minusNs(at: Instant, ns: number): Instant {
  const within = at.ns - ns;
  const carry = Math.floor(within / NS_PER_MS);
  return { ms: at.ms + carry, ns: within - carry * NS_PER_MS };
}

/** Nanoseconds from `origin` to `at`. Exact while the two are within about a
 * hundred days of each other, which every trace is. */
export function nsBetween(origin: Instant, at: Instant): number {
  return (at.ms - origin.ms) * NS_PER_MS + (at.ns - origin.ns);
}

/** A half-open span in nanoseconds from the trace origin. */
export interface Interval {
  start: number;
  end: number;
}

/**
 * Whether `outer` encloses `inner`, touching bounds included: a block that starts
 * and ends with the composite around it is still inside it.
 */
export function contains(outer: Interval, inner: Interval): boolean {
  return outer.start <= inner.start && inner.end <= outer.end;
}

/** Whether two spans were ever running at the same time. */
export function overlaps(a: Interval, b: Interval): boolean {
  return a.start < b.end && b.start < a.end;
}

/** The input coalesced into disjoint spans, earliest first. */
export function mergeIntervals(intervals: Interval[]): Interval[] {
  const sorted = [...intervals].sort((a, b) => a.start - b.start);
  const merged: Interval[] = [];
  for (const span of sorted) {
    const last = merged[merged.length - 1];
    if (last && span.start <= last.end) {
      last.end = Math.max(last.end, span.end);
      continue;
    }
    merged.push({ start: span.start, end: span.end });
  }
  return merged;
}

/**
 * How much wall-clock time the spans cover between them.
 *
 * The union rather than the sum, which is the whole point: a fork running four
 * REST calls concurrently for 100ms spent 100ms of the trace's time, not 400ms.
 * Summing durations would let a single branch account for more than the trace
 * lasted, and a percentage over 100 is how a chart admits it is measuring the
 * wrong thing.
 */
export function unionLength(intervals: Interval[]): number {
  return mergeIntervals(intervals).reduce(
    (total, span) => total + (span.end - span.start),
    0,
  );
}

/**
 * The lowest row each span can sit on without touching another on that row,
 * returned in the input's order.
 *
 * Concurrency is otherwise invisible: a `fork` mints the same block path on
 * several goroutines, so its branches arrive as spans that are siblings by
 * address and simultaneous by clock. Anything that ran alone comes back as lane
 * 0, so a sequential chain costs nothing to lay out.
 */
export function packLanes(intervals: Interval[]): number[] {
  const order = intervals
    .map((span, index) => ({ span, index }))
    .sort((a, b) => a.span.start - b.span.start || a.index - b.index);

  const lanes: number[] = new Array<number>(intervals.length).fill(0);
  const laneEnds: number[] = [];
  for (const { span, index } of order) {
    let lane = laneEnds.findIndex((end) => end <= span.start);
    if (lane < 0) lane = laneEnds.push(span.start) - 1;
    laneEnds[lane] = span.end;
    lanes[index] = lane;
  }
  return lanes;
}
