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
