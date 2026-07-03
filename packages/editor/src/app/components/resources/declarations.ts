import type { Resources } from "../../model/document";

/**
 * Pure transforms on the document's declared `resources:` — the bridge between the
 * Resources tab and the main YAML. Creating/including a file adds a declaration;
 * deleting/removing-from-project drops it; renaming moves it (preserving a
 * template alias). The Resources tab dispatches SET_RESOURCES with the result, so
 * the YAML tab reflects the change. `.env.dev` is never declared (run-host
 * auto-injects it), so callers must exclude it before reaching here.
 */

const EMPTY: Resources = { env: [], templates: [] };

/** Add `name` to the matching declared list if not already present. */
export function declareResource(
  resources: Resources | undefined,
  name: string,
  kind: "env" | "template",
): Resources {
  const r = resources ?? EMPTY;
  if (kind === "env") {
    if (r.env.includes(name)) return r;
    return { ...r, env: [...r.env, name] };
  }
  if (r.templates.some((t) => t.resource === name)) return r;
  return { ...r, templates: [...r.templates, { resource: name }] };
}

/** Remove `name` from both declared lists (leaves any stored file untouched). */
export function undeclareResource(
  resources: Resources | undefined,
  name: string,
): Resources {
  const r = resources ?? EMPTY;
  return {
    env: r.env.filter((e) => e !== name),
    templates: r.templates.filter((t) => t.resource !== name),
  };
}

/** Move a declaration from `oldName` to `newName`, preserving a template alias. */
export function renameDeclaration(
  resources: Resources | undefined,
  oldName: string,
  newName: string,
  kind: "env" | "template",
): Resources {
  const r = resources ?? EMPTY;
  const alias = r.templates.find((t) => t.resource === oldName)?.as;
  const without = undeclareResource(r, oldName);
  if (kind === "env") {
    if (without.env.includes(newName)) return without;
    return { ...without, env: [...without.env, newName] };
  }
  if (without.templates.some((t) => t.resource === newName)) return without;
  const entry = alias ? { resource: newName, as: alias } : { resource: newName };
  return { ...without, templates: [...without.templates, entry] };
}
