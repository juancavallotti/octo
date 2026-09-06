/**
 * The window a metrics view reads, and how it survives a page reload.
 *
 * Presets rather than two timestamps, following the traces filters: the window is
 * a question — "what has this been doing lately", "what did it do overnight" —
 * and nobody wants to type the answer twice.
 *
 * The presets straddle the two stored tiers deliberately. The live tier holds one
 * sample per second for one rollup interval, so the short windows read full
 * resolution; the history tier holds one collapsed row per rollup interval for the
 * retention window, so the long ones reach back a week. Which one answers is not
 * chosen here — the service resolves `tier=auto` against each pod's own
 * configuration, and the view reports what it got.
 */

export type RangeKey = "5m" | "30m" | "1h" | "24h" | "7d";

export interface RangePreset {
  key: RangeKey;
  label: string;
  minutes: number;
}

export const RANGE_PRESETS: RangePreset[] = [
  { key: "5m", label: "5m", minutes: 5 },
  { key: "30m", label: "30m", minutes: 30 },
  { key: "1h", label: "1h", minutes: 60 },
  { key: "24h", label: "24h", minutes: 60 * 24 },
  { key: "7d", label: "7d", minutes: 60 * 24 * 7 },
];

export const DEFAULT_RANGE: RangeKey = "5m";

/** Read the range out of the URL, falling back to the default.
 *
 * An unrecognized value is dropped rather than reported. A stale or hand-edited
 * link should show the default view, not an error page — nothing is wrong with
 * the install, and an error here reads like an outage. */
export function readRange(params: URLSearchParams): RangeKey {
  const raw = params.get("range");
  const found = RANGE_PRESETS.find((preset) => preset.key === raw);
  return found ? found.key : DEFAULT_RANGE;
}

/** The query string for a range, empty at the default so a plain view has a
 * plain URL. */
export function writeRange(range: RangeKey): string {
  return range === DEFAULT_RANGE ? "" : `range=${range}`;
}

/** The absolute window a preset means at a given moment, as RFC3339. */
export function windowFor(range: RangeKey, now: number): { from: string; to: string } {
  const preset = RANGE_PRESETS.find((p) => p.key === range) ?? RANGE_PRESETS[0];
  return {
    from: new Date(now - preset.minutes * 60_000).toISOString(),
    to: new Date(now).toISOString(),
  };
}

/**
 * How often a range is worth re-reading.
 *
 * Five seconds while the window is short enough to be answered from the live
 * tier, which samples every second — that is a genuinely moving picture. A
 * window of a day or a week is answered from collapsed buckets that are written
 * once per rollup interval, so polling those every five seconds asks a hundred
 * times for an answer that changed once.
 */
export function refreshMs(range: RangeKey): number {
  return spanMs(range) <= 60 * 60_000 ? 5_000 : 30_000;
}

/**
 * How much less often the full grid is re-read than the overview chart.
 *
 * Fifty charts is two orders of magnitude more data than two, and none of them
 * is wide enough to show five seconds of change: three hundred points across a
 * card is under two pixels of movement per tick. The overview is what a reader
 * watches; the grid is what they scan.
 */
export const GRID_REFRESH_FACTOR = 4;

/** How wide a preset is, in milliseconds — what a chart needs for its axis. */
export function spanMs(range: RangeKey): number {
  const preset = RANGE_PRESETS.find((p) => p.key === range) ?? RANGE_PRESETS[0];
  return preset.minutes * 60_000;
}
