/**
 * URL <-> selection serialization for the integrations manager, so the selected
 * folder bucket and integration live in the path (not the query string) and are
 * bookmarkable — e.g. a dashboard tile's "Manage" or the editor's "Integrations"
 * button deep-links straight to one. Kept out of the component.
 *
 * The path after `/platform/integrations` encodes two independent dimensions:
 *   - the folder bucket: nothing (the default "all"), `running`, `unfiled`, or
 *     `f/<folderId>`
 *   - the selected integration: nothing, or `i/<integrationId>`
 * so e.g. `/platform/integrations/f/<folderId>/i/<integrationId>` opens an
 * integration with its folder in view. Folder ids are opaque (UUIDs), so the `f`/`i`
 * prefixes (and the `unfiled` sentinel) disambiguate the two id kinds.
 */

import type { Bucket } from "./model";

/** The route prefix the manager's selection path hangs off. */
export const INTEGRATIONS_BASE = "/platform/integrations";

export interface ManagerSelection {
  selectedId: string | null;
  bucket: Bucket;
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
  let rest = segments;
  let bucket: Bucket = "all";
  if (rest[0] === "running") {
    bucket = "running";
    rest = rest.slice(1);
  } else if (rest[0] === "unfiled") {
    bucket = "unfiled";
    rest = rest.slice(1);
  } else if (rest[0] === "f" && rest[1]) {
    bucket = { folder: rest[1] };
    rest = rest.slice(2);
  }
  const selectedId = rest[0] === "i" && rest[1] ? rest[1] : null;
  return { selectedId, bucket };
}

/**
 * Serialize a selection into the path suffix (after INTEGRATIONS_BASE), including a
 * leading slash, or "" for the default (all, nothing selected) so the URL stays
 * clean. Segments are encoded for safe use in a path.
 */
export function buildPath(sel: ManagerSelection): string {
  const segs: string[] = [];
  if (sel.bucket === "running") segs.push("running");
  else if (sel.bucket === "unfiled") segs.push("unfiled");
  else if (typeof sel.bucket === "object") segs.push("f", sel.bucket.folder);
  if (sel.selectedId) segs.push("i", sel.selectedId);
  return segs.length ? `/${segs.map(encodeURIComponent).join("/")}` : "";
}
