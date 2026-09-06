/**
 * The arithmetic behind the metrics charts: extents, scales, ticks, SVG paths and
 * downsampling. Pure — numbers in, numbers out, no React and no DOM — so the part
 * that can be quietly wrong is testable without rendering anything.
 *
 * A sibling of `traces/chartLayout.ts` rather than an extension of it. That module
 * is a viewport algebra for one axis of nanoseconds measured from a trace's own
 * origin, drawn at a constant 120 pixels per second because a ten-second trace
 * *should* look ten times a one-second one. A metrics chart wants the opposite:
 * wall-clock milliseconds fitted to the width available, and a value axis, which
 * that module has no concept of. Sharing them would mean generalizing a working
 * chart to serve a new one; two small modules in the same idiom is cheaper.
 *
 * # Nulls are not zeros, all the way to the path
 *
 * A null reading is a gap in the scrape. Everything here carries it through:
 * extents ignore it, downsampling propagates it, and pathFor lifts the pen rather
 * than drawing a line across it. A chart that closed the gap would report a
 * measurement nobody took.
 */

/** A reading, where null is a gap. Mirrors the model's type without importing it,
 * so this module stays free of the BFF. */
export type Reading = number | null;

/** A closed numeric interval. */
export interface Extent {
  min: number;
  max: number;
}

/** Maps a domain value onto a pixel. */
export type Scale = (value: number) => number;

/** The smallest and largest finite reading, or null when there is none. */
export function extent(values: ReadonlyArray<Reading>): Extent | null {
  let min = Infinity;
  let max = -Infinity;
  for (const value of values) {
    if (value === null || !Number.isFinite(value)) continue;
    if (value < min) min = value;
    if (value > max) max = value;
  }
  return min === Infinity ? null : { min, max };
}

/** The union of several extents, ignoring the empty ones. */
export function unionExtent(parts: ReadonlyArray<Extent | null>): Extent | null {
  let out: Extent | null = null;
  for (const part of parts) {
    if (!part) continue;
    out = out
      ? { min: Math.min(out.min, part.min), max: Math.max(out.max, part.max) }
      : part;
  }
  return out;
}

/**
 * An extent a chart can actually draw.
 *
 * Two adjustments, both for cases that otherwise render blank. A flat series has
 * `min === max`, which makes every scale a division by zero; it is given a band
 * around itself. And a series of non-negative readings is anchored at zero,
 * because a memory line hovering between 118 and 120 MiB drawn full-height reads
 * as a crisis rather than as a flat line.
 */
export function plotExtent(raw: Extent | null): Extent {
  if (!raw) return { min: 0, max: 1 };

  const min = raw.min >= 0 ? 0 : raw.min;
  if (min === raw.max) {
    // A flat zero series still needs a height; a flat non-zero one gets headroom
    // proportional to itself rather than an arbitrary unit. The headroom is
    // always upward — scaling a negative value by 1.25 would put the maximum
    // below the minimum and reverse the whole axis.
    return { min, max: raw.max === 0 ? 1 : raw.max + Math.abs(raw.max) * 0.25 };
  }

  // A little headroom above the peak. Without it the highest reading is drawn
  // flush against the top of the plot, where a stroke half outside the clip
  // reads as a line that was cut off rather than as the maximum.
  return { min, max: raw.max + (raw.max - min) * 0.05 };
}

/** A linear scale from a domain onto a pixel range. A degenerate domain maps
 * everything to the range's start rather than to NaN. */
export function linearScale(domain: Extent, range: readonly [number, number]): Scale {
  const span = domain.max - domain.min;
  if (span === 0) return () => range[0];
  const ratio = (range[1] - range[0]) / span;
  return (value) => range[0] + (value - domain.min) * ratio;
}

/**
 * The nearest 1, 2 or 5 × 10ⁿ at or **above** `raw`.
 *
 * Above, where `traces/chartLayout.ts`'s private equivalent rounds below. Its
 * time axis wants at least the tick count it asked for; a value axis wants at
 * most it — rounding down there turns a request for four gridlines into eight,
 * and eight labelled rules is a lattice rather than a reference.
 */
export function niceStep(raw: number): number {
  if (!Number.isFinite(raw) || raw <= 0) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const normalized = raw / magnitude;
  if (normalized > 5) return 10 * magnitude;
  if (normalized > 2) return 5 * magnitude;
  if (normalized > 1) return 2 * magnitude;
  return magnitude;
}

/**
 * The same idea in powers of 1024, for an axis labelled in bytes.
 *
 * A decimal step makes an axis of "19 MiB, 38 MiB, 57 MiB", which is arithmetic
 * nobody wants to do while reading a chart. Memory is quoted in binary units, so
 * the gridlines should land on them.
 */
export function binaryStep(raw: number): number {
  if (!Number.isFinite(raw) || raw <= 0) return 1;
  const unit = 1024 ** Math.max(0, Math.floor(Math.log(raw) / Math.log(1024)));
  const normalized = raw / unit;
  for (const factor of [1, 2, 4, 8, 16, 32, 64, 128, 256, 512]) {
    if (factor >= normalized) return factor * unit;
  }
  return 1024 * unit;
}

/** How many decimals a label needs to distinguish neighbouring ticks. Derived
 * from the step rather than each value, so one axis reads consistently instead
 * of pairing "0.0000" with "0.250". */
export function axisDecimals(step: number): number {
  if (!Number.isFinite(step) || step <= 0) return 0;
  return Math.max(0, Math.min(-Math.floor(Math.log10(step)), 8));
}

/** Round values across [min, max], about `target` of them, at nice boundaries.
 * `stepper` chooses the scale those boundaries fall on. */
export function ticks(
  min: number,
  max: number,
  target = 4,
  stepper: (raw: number) => number = niceStep,
): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max) || max <= min) return [min];

  const step = stepper((max - min) / Math.max(target, 1));
  const out: number[] = [];
  // Loop-guarded: a pathological domain must not hang the render.
  for (let at = Math.ceil(min / step) * step; at <= max && out.length < 64; at += step) {
    // Reconstructing from the index avoids the drift that repeated addition
    // accumulates, which is what turns a 0.30000000000000004 into an axis label.
    out.push(round(at, step));
  }
  return out.length > 0 ? out : [min];
}

/** Drop the floating-point noise repeated addition leaves behind. */
function round(value: number, step: number): number {
  const decimals = Math.max(0, -Math.floor(Math.log10(step)) + 1);
  return Number(value.toFixed(Math.min(decimals, 12)));
}

/** The time steps an axis is allowed to land on, in milliseconds. Wall-clock
 * durations rather than powers of ten: nobody reads a 3.2-minute grid. */
const TIME_STEPS = [
  1000, 5000, 15_000, 30_000,
  60_000, 300_000, 900_000, 1_800_000,
  3_600_000, 10_800_000, 21_600_000, 43_200_000,
  86_400_000, 172_800_000, 604_800_000,
] as const;

/** Tick times across [fromMs, toMs], aligned to whole multiples of the step so
 * neighbouring views share gridlines. */
export function timeTicks(fromMs: number, toMs: number, target = 4): number[] {
  if (!(toMs > fromMs)) return [fromMs];

  const wanted = (toMs - fromMs) / Math.max(target, 1);
  const step = TIME_STEPS.find((candidate) => candidate >= wanted)
    ?? TIME_STEPS[TIME_STEPS.length - 1];

  const out: number[] = [];
  for (let at = Math.ceil(fromMs / step) * step; at <= toMs && out.length < 64; at += step) {
    out.push(at);
  }
  return out.length > 0 ? out : [fromMs];
}

/** One projected point. A null y is a gap. */
export interface Point {
  x: number;
  y: number | null;
}

/**
 * An SVG path through the points, lifting the pen at every gap.
 *
 * The resume is an `M`, not an `L`: a line drawn across a gap claims the metric
 * moved smoothly through a stretch nobody sampled. A lone point between two gaps
 * would be invisible as a path, so it is emitted as a zero-length line, which
 * renders as a dot under a round linecap.
 */
export function pathFor(points: ReadonlyArray<Point>): string {
  const parts: string[] = [];
  let open = false;
  let isolated = true;

  for (const { x, y } of points) {
    if (y === null || !Number.isFinite(y) || !Number.isFinite(x)) {
      if (open && isolated) parts.push("l0 0");
      open = false;
      continue;
    }
    if (open) {
      parts.push(`L${fixed(x)} ${fixed(y)}`);
      isolated = false;
    } else {
      parts.push(`M${fixed(x)} ${fixed(y)}`);
      open = true;
      isolated = true;
    }
  }
  if (open && isolated) parts.push("l0 0");
  return parts.join("");
}

/** Two decimals is a tenth of a pixel — past that, the path string is just larger. */
function fixed(value: number): string {
  return String(Math.round(value * 100) / 100);
}

/** A downsampled column. */
export interface Sampled {
  times: number[];
  values: Reading[];
}

/**
 * Reduce a column to at most `buckets` points, keeping the extreme of each.
 *
 * Five minutes of one-second samples is 300 points for a sparkline 120 pixels
 * wide, so most of them are decoration. Averaging would smooth away the spike
 * that is the only reason anyone glances at a sparkline, so each bucket keeps its
 * furthest value from zero instead. A bucket with no reading stays a gap.
 */
export function downsample(
  times: ReadonlyArray<number>,
  values: ReadonlyArray<Reading>,
  buckets: number,
): Sampled {
  const count = Math.min(times.length, values.length);
  if (buckets < 1 || count <= buckets) {
    return { times: times.slice(0, count), values: values.slice(0, count) };
  }

  const out: Sampled = { times: [], values: [] };
  const width = count / buckets;
  for (let bucket = 0; bucket < buckets; bucket++) {
    const start = Math.floor(bucket * width);
    const end = Math.min(Math.floor((bucket + 1) * width), count);

    let peak: Reading = null;
    let peakAt = start;
    for (let i = start; i < end; i++) {
      const value = values[i];
      if (value === null || !Number.isFinite(value)) continue;
      if (peak === null || Math.abs(value) > Math.abs(peak)) {
        peak = value;
        peakAt = i;
      }
    }
    // The peak's own moment, not the bucket's start: the columns have to stay
    // parallel, and a spike reported a few samples early is a spike at the
    // wrong time for anything that reads the times.
    out.times.push(times[peakAt]);
    out.values.push(peak);
  }
  return out;
}
