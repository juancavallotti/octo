/**
 * URL ↔ selection for the traces view, so what you are looking at lives in the
 * path and is bookmarkable — a trace is the thing people paste to each other.
 *
 * The path after `/platform/traces` encodes three nested choices:
 *
 *     /a/<deploymentId>              one app, every version of it
 *     /a/<deploymentId>/v/<version>  one app at one version
 *     …/t/<traceId>                  with a trace open
 *
 * The version is part of the selection rather than a detail of it because the
 * app list is grouped that way: the trace store reports a (deployment, name,
 * version) triple per row, since a rollout keeps the deployment id and changes
 * the version, and a cost belongs to a version. A row selected by deployment
 * alone would open a list whose totals did not match the row that was clicked.
 *
 * The prefixes disambiguate the id kinds, which are otherwise all opaque strings.
 */

import type { TraceStatus } from "@/app/model/traces";

/** The route prefix the selection path hangs off. */
export const TRACES_BASE = "/platform/traces";

export interface TraceSelection {
  /** The deployment whose traces are listed; null when nothing is selected. */
  deploymentId: string | null;
  /** The version to narrow to, or null for every version of that deployment. */
  appVersion: string | null;
  /** The trace open in the detail pane, or null. */
  traceId: string | null;
}

export const NOTHING_SELECTED: TraceSelection = {
  deploymentId: null,
  appVersion: null,
  traceId: null,
};

/** Parse a full pathname (from usePathname) into a selection. */
export function parsePathname(pathname: string): TraceSelection {
  const tail = pathname.startsWith(TRACES_BASE)
    ? pathname.slice(TRACES_BASE.length)
    : "";
  return readSelection(tail.split("/").filter(Boolean).map(decodeURIComponent));
}

/** Parse the catch-all route's segments into a selection. */
export function readSelection(segments: string[]): TraceSelection {
  let rest = segments;
  const take = (prefix: string): string | null => {
    if (rest[0] !== prefix || !rest[1]) return null;
    const value = rest[1];
    rest = rest.slice(2);
    return value;
  };

  const deploymentId = take("a");
  // Both only mean anything under an app: a version narrows that app's list, and
  // a trace is reached through it. A stray /v or /t alone selects nothing.
  if (deploymentId === null) return NOTHING_SELECTED;
  const appVersion = take("v");
  return { deploymentId, appVersion, traceId: take("t") };
}

/**
 * Serialize a selection into the path suffix after {@link TRACES_BASE}, with a
 * leading slash — or "" when nothing is selected, so the bare route stays clean.
 *
 * A version tag can be spelled with anything a user typed and a trace id comes
 * off the wire, so every segment is encoded.
 */
export function buildPath(selection: TraceSelection): string {
  if (!selection.deploymentId) return "";
  const segments = ["a", selection.deploymentId];
  if (selection.appVersion) segments.push("v", selection.appVersion);
  if (selection.traceId) segments.push("t", selection.traceId);
  return `/${segments.map(encodeURIComponent).join("/")}`;
}

/** The full route for a selection, ready to hand to the router. */
export function buildHref(selection: TraceSelection): string {
  return `${TRACES_BASE}${buildPath(selection)}`;
}

// ---------------------------------------------------------------------------
// Filters, which live in the query string rather than the path.
// ---------------------------------------------------------------------------

/**
 * How far back to look. A preset rather than two timestamps because the window
 * is a question ("was it happening this morning?") and not a value anyone wants
 * to type — and because both the app list and the trace list must be measured
 * over the *same* window or their counts stop being comparable.
 */
export type WindowPreset = "1h" | "24h" | "7d" | "30d";

export const WINDOW_PRESETS: { key: WindowPreset; label: string; hours: number }[] = [
  { key: "1h", label: "Last hour", hours: 1 },
  { key: "24h", label: "Last 24 hours", hours: 24 },
  { key: "7d", label: "Last 7 days", hours: 24 * 7 },
  { key: "30d", label: "Last 30 days", hours: 24 * 30 },
];

export interface TraceFilterValues {
  window: WindowPreset;
  /** Empty means every status; the service rejects one it cannot match. */
  statuses: TraceStatus[];
  /** Keeps only traces that called a model at least once. */
  hasLlm: boolean;
  /** Keeps traces at least this long, by their whole span. */
  minDurationMs: number | null;
  /** Substring over trace id, entry label and root flow. */
  q: string;
}

export const DEFAULT_FILTERS: TraceFilterValues = {
  // 24 hours matches what the service does with an unbounded query, so the first
  // view and the service's own default are the same window.
  window: "24h",
  statuses: [],
  hasLlm: false,
  minDurationMs: null,
  q: "",
};

const STATUSES: TraceStatus[] = ["ok", "dropped", "failed"];

/** Read filters out of a query string, ignoring anything unrecognized. */
export function readFilters(params: URLSearchParams): TraceFilterValues {
  const window = params.get("w");
  const minDuration = Number(params.get("slower"));
  return {
    window: WINDOW_PRESETS.some((p) => p.key === window)
      ? (window as WindowPreset)
      : DEFAULT_FILTERS.window,
    // A status the service would reject is dropped here rather than sent: it
    // would come back as an error page, which reads like an outage.
    statuses: params.getAll("status").filter((s): s is TraceStatus =>
      STATUSES.includes(s as TraceStatus),
    ),
    hasLlm: params.get("llm") === "1",
    minDurationMs: Number.isFinite(minDuration) && minDuration > 0 ? minDuration : null,
    q: params.get("q") ?? "",
  };
}

/** Serialize filters, omitting everything left at its default so a plain view
 * has a clean URL. */
export function writeFilters(filters: TraceFilterValues): string {
  const params = new URLSearchParams();
  if (filters.window !== DEFAULT_FILTERS.window) params.set("w", filters.window);
  for (const status of filters.statuses) params.append("status", status);
  if (filters.hasLlm) params.set("llm", "1");
  if (filters.minDurationMs) params.set("slower", String(filters.minDurationMs));
  if (filters.q) params.set("q", filters.q);
  return params.toString();
}

/**
 * The absolute bounds of a preset, resolved against a fixed `now`.
 *
 * `now` is passed rather than read, because a window recomputed on every render
 * changes on every render — and a changing window is a changing query, which
 * would refetch forever.
 */
export function windowFor(preset: WindowPreset, now: number): { from: string; to: string } {
  const hours = WINDOW_PRESETS.find((p) => p.key === preset)?.hours ?? 24;
  return {
    from: new Date(now - hours * 3_600_000).toISOString(),
    to: new Date(now).toISOString(),
  };
}
