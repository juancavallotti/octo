/**
 * The two views a metrics page offers, and how they survive a page reload.
 *
 * Two, not a ladder of windows, because two is what the storage holds. A pod
 * keeps per-second samples in a live tier and collapsed buckets in a history
 * tier, and every window that is neither — half an hour, a day — has to be
 * resolved to one of them by the service. That resolution is silent and lossy in
 * the surprising direction: a window slightly longer than the live tier reaches
 * is answered entirely from buckets, so asking for five minutes of a two-minute
 * live tier returns two points rather than a hundred and twenty.
 *
 * The views are named after the tiers rather than after durations, because the
 * duration is not ours to name. How far the live tier reaches and how wide a
 * bucket is are both per-install settings; a view called "Hourly" would be a lie
 * on any install that configured something else. Live and Historic are true
 * whatever the numbers are, and the header states what those numbers turned out
 * to be.
 */

import type { StatsPod, StatsTier } from "@/app/model/stats";
import { parseGoDuration } from "@/app/components/stats/chart/duration";

export type View = "live" | "historic";

export interface ViewPreset {
  key: View;
  label: string;
  /** The tier this view reads. Explicit — never `auto`. */
  tier: Exclude<StatsTier, "auto">;
  /**
   * How far back to ask before the pods have said how far back there is.
   *
   * Only a first guess. Once /pods has answered, `reachFor` replaces this with
   * the tier's own configured reach — see there for why a constant is the wrong
   * thing to ship.
   */
  askMs: number;
  /** How often to re-read while following. */
  refreshMs: number;
}

export const VIEWS: ViewPreset[] = [
  {
    key: "live",
    label: "Live",
    tier: "live",
    askMs: 24 * 60 * 60_000,
    // The tier samples every second or so, so this is a genuinely moving
    // picture.
    refreshMs: 5_000,
  },
  {
    key: "historic",
    label: "Historic",
    tier: "rollup",
    askMs: 90 * 24 * 60 * 60_000,
    // A bucket is written once per rollup interval. Polling faster asks
    // repeatedly for an answer that changed once.
    refreshMs: 30_000,
  },
];

export const DEFAULT_VIEW: View = "live";

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

/** The absolute window to ask for, as RFC3339. */
export function windowFor(
  view: View,
  now: number,
  askMs = viewPreset(view).askMs,
): { from: string; to: string } {
  return {
    from: new Date(now - askMs).toISOString(),
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

/**
 * How far back a view's tier actually holds, from the pods' own configuration.
 *
 * Deduced rather than assumed. Every duration here is a per-install setting: a
 * week of retention is only the default, the bucket width is tunable, and the
 * live tier's depth follows the bucket. A page that asked for a hard-coded seven
 * days would show nothing beyond it on an install keeping thirty, and would ask
 * for six days of rows that do not exist on one keeping one.
 *
 * The widest pod wins, because pods of a deployment can be rolled at different
 * times and one may still be running an older configuration. A tenth is added on
 * top so the oldest row is not clipped by the boundary it sits on.
 *
 * Falls back to the view's own guess when no pod has reported — there is nothing
 * to deduce from yet.
 */
export function reachFor(view: View, pods: ReadonlyArray<StatsPod>): number {
  const preset = viewPreset(view);

  let widest = 0;
  for (const pod of pods) {
    // The live tier holds one bucket's worth of samples, so the bucket width is
    // its reach. That coupling lives in the sidecar; this only reads it.
    const configured = parseGoDuration(
      preset.tier === "live" ? pod.rollupInterval : pod.retention,
    );
    if (configured !== null && configured > widest) widest = configured;
  }
  return widest > 0 ? Math.round(widest * 1.1) : preset.askMs;
}
