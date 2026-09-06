/**
 * Go duration strings, which is what the stats service reports a step and a
 * sidecar's intervals as: "1s", "1h0m0s", "1m30s", "500ms".
 *
 * Nothing else in the app parses these, and the two places that need to are not
 * cosmetic. A chart divides a counter's growth by the interval it covers, and the
 * step is the divisor for the first point of a window; a pod's sampleInterval is
 * how the reader is told what resolution they are looking at.
 */

/** The units Go's Duration.String() emits, in milliseconds. */
const UNITS: ReadonlyArray<readonly [string, number]> = [
  ["ns", 1e-6],
  ["us", 1e-3],
  ["µs", 1e-3],
  ["μs", 1e-3], // U+03BC, which some encoders emit in place of U+00B5
  ["ms", 1],
  ["s", 1000],
  ["m", 60_000],
  ["h", 3_600_000],
];

/** One number-and-unit pair; the unit is greedy so "ms" never reads as "m". */
const TOKEN = /(-?\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g;

/**
 * Parse a Go duration to milliseconds, or null when it is not one.
 *
 * Null rather than zero, and the difference matters: zero is a legitimate
 * duration ("0s") and a caller dividing by a step must be able to tell a real
 * zero from a string it failed to read. Both are useless as a divisor, but only
 * one of them is a bug worth noticing.
 */
export function parseGoDuration(input: string): number | null {
  const text = input.trim();
  if (text === "" || text === "0") return null;
  if (text === "0s") return 0;

  const negative = text.startsWith("-");
  const body = negative ? text.slice(1) : text;

  let total = 0;
  let matched = 0;
  TOKEN.lastIndex = 0;
  for (const [whole, amount, unit] of body.matchAll(TOKEN)) {
    const scale = UNITS.find(([name]) => name === unit)?.[1];
    if (scale === undefined) return null;
    total += Number(amount) * scale;
    matched += whole.length;
  }

  // Every character must have belonged to a token, or this is not a duration:
  // "1s and a bit" would otherwise parse as one second.
  if (matched === 0 || matched !== body.length) return null;
  return negative ? -total : total;
}

/**
 * A duration in milliseconds, as short as it can be said.
 *
 * Deliberately coarser than parseGoDuration is precise: this labels an axis and a
 * status line, where "1h" is what the reader wants and "1h0m0s" is what the wire
 * happens to carry.
 */
export function formatStep(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;

  const seconds = ms / 1000;
  if (seconds < 60) return `${trimZero(seconds)}s`;
  const minutes = seconds / 60;
  if (minutes < 60) return `${trimZero(minutes)}m`;
  const hours = minutes / 60;
  if (hours < 24) return `${trimZero(hours)}h`;
  return `${trimZero(hours / 24)}d`;
}

/** One decimal, but only when it says something. */
function trimZero(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}
