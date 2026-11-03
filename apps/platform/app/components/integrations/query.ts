/**
 * URL <-> selection serialization for the integrations manager, so the selected
 * integration lives in the path (not the query string) and is bookmarkable — e.g.
 * a dashboard tile's "Manage" or the editor's "Integrations" button deep-links
 * straight to one. Kept out of the component.
 *
 * The path after `/platform/integrations` is nothing, or `i/<integrationId>`.
 */

/** The route prefix the manager's selection path hangs off. */
export const INTEGRATIONS_BASE = "/platform/integrations";

export interface ManagerSelection {
  selectedId: string | null;
}

/** Parse a full pathname (e.g. from usePathname) into a selection. Segments after
 *  INTEGRATIONS_BASE are decoded, mirroring how buildPath encodes them. */
export function parsePathname(pathname: string): ManagerSelection {
  const tail = pathname.startsWith(INTEGRATIONS_BASE)
    ? pathname.slice(INTEGRATIONS_BASE.length)
    : "";
  const segments = tail.split("/").filter(Boolean).map(decodeURIComponent);
  return readSelection(segments);
}

/** Parse the catch-all route segments into a selection. */
export function readSelection(segments: string[]): ManagerSelection {
  return { selectedId: segments[0] === "i" && segments[1] ? segments[1] : null };
}

/**
 * Serialize a selection into the path suffix (after INTEGRATIONS_BASE), including a
 * leading slash, or "" when nothing is selected so the URL stays clean. Segments
 * are encoded for safe use in a path.
 */
export function buildPath(sel: ManagerSelection): string {
  return sel.selectedId ? `/i/${encodeURIComponent(sel.selectedId)}` : "";
}
