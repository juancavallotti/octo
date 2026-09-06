/**
 * The two views a metrics page offers, and how they survive a page reload.
 *
 * Two, not a ladder of windows, because two is what the storage holds. A pod
 * keeps per-second samples in a live tier and collapsed buckets in a history
 * tier, and every window that is neither — half an hour, a day — has to be
 * resolved to one of them by the service. That resolution is silent and it is
 * lossy in the surprising direction: a window slightly longer than the live tier
 * reaches is answered entirely from buckets, so asking for five minutes of a
 * two-minute live tier returns two points rather than a hundred and twenty.
 *
 * Naming a view after its tier removes the guess. Hourly asks for live and gets
 * per-second data; Weekly asks for history and gets one point per bucket.
 * Neither can fall back to the other, so neither can surprise anybody.
 */

import type { StatsTier } from "@/app/model/stats";

export type View = "hourly" | "weekly";

export interface ViewPreset {
  key: View;
  label: string;
  /** The tier this view reads. Explicit — never `auto`. */
  tier: Exclude<StatsTier, "auto">;
  /**
   * How far back to ask. Deliberately generous: it is the reach of each tier at
   * the shipped defaults, and a tier holding less simply returns less. The chart
   * draws the span it received rather than the one requested, so asking for more
   * than exists costs nothing and hard-coding less would hide data.
   */
  spanMs: number;
  /** How often to re-read while live. */
  refreshMs: number;
}

export const VIEWS: ViewPreset[] = [
  {
    key: "hourly",
    label: "Hourly",
    tier: "live",
    spanMs: 60 * 60_000,
    // The tier samples every second, so this is a genuinely moving picture.
    refreshMs: 5_000,
  },
  {
    key: "weekly",
    label: "Weekly",
    tier: "rollup",
    spanMs: 7 * 24 * 60 * 60_000,
    // A bucket is written once per rollup interval. Polling faster asks
    // repeatedly for an answer that changed once.
    refreshMs: 30_000,
  },
];

export const DEFAULT_VIEW: View = "hourly";

/** The preset for a view. */
export function viewPreset(view: View): ViewPreset {
  return VIEWS.find((v) => v.key === view) ?? VIEWS[0];
}

/**
 * Read the view out of the URL, falling back to the default.
 *
 * An unrecognized value is dropped rather than reported: a stale or hand-edited
 * link should show the default view, not an error page. Nothing is wrong with
 * the install, and an error here reads like an outage.
 */
export function readView(params: URLSearchParams): View {
  const raw = params.get("view");
  return VIEWS.some((v) => v.key === raw) ? (raw as View) : DEFAULT_VIEW;
}

/** The query string for a view, empty at the default so a plain view has a
 * plain URL. */
export function writeView(view: View): string {
  return view === DEFAULT_VIEW ? "" : `view=${view}`;
}

/** The absolute window a view asks for at a given moment, as RFC3339. */
export function windowFor(view: View, now: number): { from: string; to: string } {
  return {
    from: new Date(now - viewPreset(view).spanMs).toISOString(),
    to: new Date(now).toISOString(),
  };
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
