import type { StoredResource } from "../../providers/ResourceStoreProvider";
import type { Resources } from "../../model/document";

/**
 * The dev-env resource name (mirrors run-host's DEV_ENV_RESOURCE). Inlined rather
 * than imported so this module doesn't pull the Node-only run-host barrel. The
 * Dev .env panel owns this file, so it is excluded from the Resources tab.
 */
export const DEV_ENV_RESOURCE = ".env.dev";

/**
 * The namespace the editor keeps for its own bookkeeping — `.octo/editor-meta.json`
 * and whatever joins it. These are design-time files: no config declares them, and a
 * runtime loads only what its config declares, so they are never read by one.
 */
export const EDITOR_PRIVATE_PREFIX = ".octo/";

/**
 * Whether a resource is owned by something other than the Resources tab, and so must
 * not appear in it: `.env.dev` belongs to the Dev .env panel, and anything under
 * `.octo/` is the editor's own bookkeeping. Showing these would invite the user to
 * edit or "include in project" a file that is not theirs to manage.
 */
export function isHiddenResource(name: string): boolean {
  return name === DEV_ENV_RESOURCE || name.startsWith(EDITOR_PRIVATE_PREFIX);
}

/**
 * How a resource relates to the project's declared `resources:`:
 * - `tracked` — declared *and* present in the store; fully editable.
 * - `out-of-scope` — present but not declared; shown read-only, never mutated
 *   (only "Include in project" is offered). This is the non-destructive default
 *   for files that exist on disk but the YAML never declared.
 * - `declared-missing` — declared but no content in the store yet.
 */
export type ResourceScope = "tracked" | "out-of-scope" | "declared-missing";

export interface ReconciledResource {
  /** Path-like name (may contain `/`). */
  name: string;
  scope: ResourceScope;
  /** Runtime kind — from the stored file, else inferred from the declaration. */
  kind: "env" | "template";
  /** The stored record, present unless the entry is `declared-missing`. */
  stored?: StoredResource;
}

/** The set of names the document declares (env files + template resources). */
function declaredNames(resources: Resources | undefined): {
  env: Set<string>;
  templates: Set<string>;
} {
  return {
    env: new Set(resources?.env ?? []),
    templates: new Set((resources?.templates ?? []).map((t) => t.resource)),
  };
}

/**
 * Reconcile what the store holds with what the document declares into one scoped,
 * name-sorted list. Resources owned elsewhere are excluded (see isHiddenResource). A
 * name declared as both env and template (unusual) is treated as env.
 */
export function reconcileResources(
  stored: StoredResource[],
  declared: Resources | undefined,
): ReconciledResource[] {
  const { env, templates } = declaredNames(declared);
  const byName = new Map<string, StoredResource>();
  for (const r of stored) {
    if (isHiddenResource(r.name)) continue;
    byName.set(r.name, r);
  }

  const out: ReconciledResource[] = [];
  const seen = new Set<string>();

  // Stored files: tracked when declared, else out of scope.
  for (const [name, r] of byName) {
    const isDeclared = env.has(name) || templates.has(name);
    out.push({
      name,
      scope: isDeclared ? "tracked" : "out-of-scope",
      kind: r.kind,
      stored: r,
    });
    seen.add(name);
  }

  // Declared names with no stored content yet.
  for (const name of env) {
    if (isHiddenResource(name) || seen.has(name)) continue;
    out.push({ name, scope: "declared-missing", kind: "env" });
    seen.add(name);
  }
  for (const name of templates) {
    if (isHiddenResource(name) || seen.has(name)) continue;
    out.push({ name, scope: "declared-missing", kind: "template" });
    seen.add(name);
  }

  out.sort((a, b) => a.name.localeCompare(b.name));
  return out;
}
