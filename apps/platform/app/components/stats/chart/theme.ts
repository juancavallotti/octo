/**
 * How the charts are dressed, in one place.
 *
 * Recharts renders SVG it owns, so it cannot be styled with the Tailwind classes
 * everything else here uses. Two consequences shape this file. Colours are
 * literals, because a chart line has to be a colour string rather than a class.
 * Everything else that varies with the theme is `currentColor`, inherited from a
 * wrapper that *is* Tailwind-styled — so axes and gridlines follow light and
 * dark without this module knowing which is which.
 */

/** CPU, and the left axis it is read against. Tailwind sky-500. */
export const CPU_COLOR = "#0ea5e9";

/** Memory, and the right axis. Tailwind violet-500. */
export const MEM_COLOR = "#8b5cf6";

/** One line per pod within a metric, so a scaled deployment is readable. Sky
 * first, because a single-pod deployment is the common case and should match
 * the CPU colour the rest of the page uses. */
export const SERIES_COLORS = [
  "#0ea5e9",
  "#8b5cf6",
  "#10b981",
  "#f59e0b",
  "#ef4444",
  "#14b8a6",
] as const;

/** The colour for the nth series of a metric, wrapping rather than running out. */
export function seriesColor(index: number): string {
  return SERIES_COLORS[index % SERIES_COLORS.length];
}

/** Axis styling shared by every chart. currentColor so the wrapper's Tailwind
 * text colour carries the theme in. */
export const AXIS = {
  stroke: "currentColor",
  strokeOpacity: 0.25,
  tick: { fill: "currentColor", fontSize: 10 },
  tickLine: false,
} as const;

/** Gridlines, faint enough to read past. */
export const GRID = {
  stroke: "currentColor",
  strokeOpacity: 0.1,
  vertical: false,
} as const;

/**
 * Line styling. Animation is off everywhere and that is not a preference: the
 * page polls, so an animated redraw every five seconds is a chart that is never
 * still long enough to read.
 */
export const LINE = {
  type: "linear",
  dot: false,
  activeDot: { r: 3 },
  isAnimationActive: false,
  strokeWidth: 1.5,
  // A gap is a moment nobody measured. Joining across it draws a line through
  // data that does not exist.
  connectNulls: false,
} as const;
