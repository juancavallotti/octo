import type { EditorDocument, FlowDoc } from "./document";

/**
 * Helpers for the document's named identifiers (connection names, flow names).
 * These names are *references*: a flow's source and various block settings point
 * at a connector by name, and flow-ref invokes a flow by name. To keep those
 * references resolvable we slug the names (so they round-trip cleanly to the
 * runtime config) and surface duplicates to the UI.
 */

/**
 * Lower-case kebab slug: alphanumerics kept, every other run (spaces, symbols)
 * becomes a single dash. Only the leading dash is trimmed — a trailing dash is
 * preserved so it can be typed live (e.g. "my-" on the way to "my-db").
 */
export function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+/, "");
}

/** `base` if free, else `base-2`, `base-3`… until it isn't in `taken`. */
export function uniqueSlug(base: string, taken: Set<string>): string {
  const slug = slugify(base) || "item";
  if (!taken.has(slug)) return slug;
  for (let n = 2; ; n++) {
    const candidate = `${slug}-${n}`;
    if (!taken.has(candidate)) return candidate;
  }
}

/** The set of names that occur more than once (empty names ignored). */
export function duplicateNames(names: string[]): Set<string> {
  const seen = new Set<string>();
  const dupes = new Set<string>();
  for (const name of names) {
    if (!name) continue;
    if (seen.has(name)) dupes.add(name);
    else seen.add(name);
  }
  return dupes;
}

/** Every non-empty flow name in the document (top-level and nested in slots). */
export function flowNames(doc: EditorDocument): string[] {
  const names: string[] = [];
  const visit = (flow: FlowDoc) => {
    if (flow.name) names.push(flow.name);
    for (const block of flow.process) {
      if (!block.slots) continue;
      for (const subs of Object.values(block.slots)) {
        for (const sub of subs) visit(sub);
      }
    }
  };
  for (const flow of doc.flows) visit(flow);
  return names;
}
