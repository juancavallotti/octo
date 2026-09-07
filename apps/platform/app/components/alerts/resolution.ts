import type { AlertCondition } from "@/app/model/alerts";

/**
 * How often a watch looks, and what a window means because of it.
 *
 * A condition's window is stored as a number of buckets, which is the right unit
 * for the service — the arithmetic is positional — and the wrong one for a form.
 * "Window: 5" is unreadable without knowing the bucket width, which used to be a
 * separate field somewhere else on the page.
 *
 * So the editor never shows buckets. It shows durations, and converts using the
 * width the watch is checked at. That leaves one thing to be careful about, which
 * is the whole reason this file is separate and tested: changing how often a
 * watch is checked changes the width, and the same bucket count would then mean a
 * different span. The durations are what somebody chose, so they are what is
 * preserved.
 */

/** The bucket widths the service will accept from this editor. */
const FINE = 30;
const COARSE = 60;

/**
 * The width a watch checked this often is measured in.
 *
 * Buckets no wider than the gap between checks, so every check has a new one to
 * look at, and no finer than they need to be — a minute is the resolution
 * anything above a minute wants.
 */
export function stepFor(intervalSeconds: number): number {
  return intervalSeconds <= FINE ? FINE : COARSE;
}

/** A duration a select offers, in seconds. */
export interface Duration {
  seconds: number;
  label: string;
}

export const INTERVALS: Duration[] = [
  { seconds: 30, label: "Every 30 seconds" },
  { seconds: 60, label: "Every minute" },
  { seconds: 300, label: "Every 5 minutes" },
  { seconds: 900, label: "Every 15 minutes" },
  { seconds: 3600, label: "Every hour" },
];

export const WINDOWS: Duration[] = [
  { seconds: 30, label: "30 seconds" },
  { seconds: 60, label: "1 minute" },
  { seconds: 300, label: "5 minutes" },
  { seconds: 900, label: "15 minutes" },
  { seconds: 1800, label: "30 minutes" },
  { seconds: 3600, label: "1 hour" },
  { seconds: 21600, label: "6 hours" },
];

export const BASELINES: Duration[] = [
  { seconds: 900, label: "15 minutes" },
  { seconds: 1800, label: "30 minutes" },
  { seconds: 3600, label: "1 hour" },
  { seconds: 21600, label: "6 hours" },
  { seconds: 86400, label: "24 hours" },
];

export const HOLDS: Duration[] = [
  { seconds: 0, label: "Alert straight away" },
  { seconds: 60, label: "…if it lasts a minute" },
  { seconds: 120, label: "…if it lasts 2 minutes" },
  { seconds: 300, label: "…if it lasts 5 minutes" },
  { seconds: 900, label: "…if it lasts 15 minutes" },
  { seconds: 1800, label: "…if it lasts 30 minutes" },
];

export const REPEATS: Duration[] = [
  { seconds: 0, label: "Only once per incident" },
  { seconds: 900, label: "Every 15 minutes while it lasts" },
  { seconds: 3600, label: "Every hour while it lasts" },
  { seconds: 21600, label: "Every 6 hours while it lasts" },
  { seconds: 86400, label: "Once a day while it lasts" },
];

/**
 * The presets, plus whatever is actually set.
 *
 * A watch written over the API can hold a value no preset offers, and a select
 * that did not contain it would move it to whichever option came first the next
 * time somebody saved anything at all.
 */
export function withCurrent(presets: Duration[], seconds: number): Duration[] {
  if (presets.some((p) => p.seconds === seconds)) return presets;
  return [...presets, { seconds, label: describe(seconds) }].sort(
    (a, b) => a.seconds - b.seconds,
  );
}

/** A duration in the plainest words it has. */
export function describe(seconds: number): string {
  if (seconds <= 0) return "off";
  // Under two minutes it stays in seconds: "90 seconds" is what somebody set,
  // and "1.5 minutes" is the same number said worse.
  if (seconds < 120) return `${seconds} seconds`;
  if (seconds < 3600) return plural(seconds / 60, "minute");
  if (seconds < 86400) return plural(seconds / 3600, "hour");
  return plural(seconds / 86400, "day");
}

function plural(value: number, unit: string): string {
  const rounded = Number.isInteger(value) ? value : Number(value.toFixed(1));
  return `${rounded} ${unit}${rounded === 1 ? "" : "s"}`;
}

/** Buckets for a duration, never fewer than one. */
export function bucketsFor(seconds: number, step: number): number {
  return Math.max(1, Math.round(seconds / step));
}

/** The duration a bucket count stands for. */
export function secondsFor(buckets: number, step: number): number {
  return Math.max(1, buckets) * step;
}

/** The window parameters that are expressed in buckets. */
const BUCKET_PARAMS = [
  "windowBuckets",
  "baselineBuckets",
  "forBuckets",
] as const;

/**
 * Re-express every window so it still covers the same span after the bucket
 * width changes.
 *
 * Without this, switching a watch from a minute to thirty seconds would halve
 * every window it has — silently, because the form shows durations and nothing
 * on screen would appear to have moved.
 */
export function rescale(
  conditions: AlertCondition[],
  fromStep: number,
  toStep: number,
): AlertCondition[] {
  if (fromStep === toStep) return conditions;
  return conditions.map((condition) => {
    const params = { ...(condition.params ?? {}) };
    for (const key of BUCKET_PARAMS) {
      const buckets = params[key];
      if (typeof buckets !== "number") continue;
      params[key] = bucketsFor(secondsFor(buckets, fromStep), toStep);
    }
    return { ...condition, params };
  });
}
