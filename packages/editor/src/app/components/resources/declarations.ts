import type { Resources } from "../../model/document";
import { serviceFile, type ProjectFile } from "../../model/project";

/**
 * Pure transforms on the document's declared `resources:` — the bridge between the
 * Resources tab and the main YAML. Creating/including a file adds a declaration;
 * deleting/removing-from-project drops it; renaming moves it (preserving a
 * template alias). Resources are project-wide, so the Resources tab works through the
 * *InProject helpers below, which say which files change, and dispatches
 * SET_RESOURCES with the result so the YAML view reflects it. `.env.dev` is never declared (run-host
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

/** One file's new `resources:` block — the shape SET_RESOURCES takes. */
export interface ResourcesChange {
  path: string;
  resources: Resources;
}

function declares(resources: Resources | undefined, name: string): boolean {
  return declaresAs(resources, name, "env") || declaresAs(resources, name, "template");
}

/** Whether `resources` declares `name` as a resource of `kind`. */
function declaresAs(
  resources: Resources | undefined,
  name: string,
  kind: "env" | "template",
): boolean {
  if (!resources) return false;
  return kind === "env"
    ? resources.env.includes(name)
    : resources.templates.some((t) => t.resource === name);
}

/**
 * Every name the project's declarations already use: env files, template resources
 * and template aliases. A new resource named after one of them would be shadowed by
 * the earlier declaration, which the runtime keeps first-wins.
 */
export function declaredNames(files: readonly ProjectFile[]): Set<string> {
  const ret = new Set<string>();
  for (const f of files) {
    const r = f.document.resources;
    if (!r) continue;
    r.env.forEach((e) => ret.add(e));
    for (const t of r.templates) {
      ret.add(t.resource);
      if (t.as) ret.add(t.as);
    }
  }
  return ret;
}

/**
 * Where a new declaration goes: the file that carries the project's identity, which
 * is where the recommended layout keeps what every flow shares. A project without
 * one declares in its first file, the same file that would take the identity.
 */
export function declaringHome(files: readonly ProjectFile[]): string | undefined {
  return serviceFile(files);
}

/** Declare `name` in the project's home file, unless some file already declares it as `kind`. */
export function declareInProject(
  files: readonly ProjectFile[],
  name: string,
  kind: "env" | "template",
): ResourcesChange[] {
  if (files.some((f) => declaresAs(f.document.resources, name, kind))) return [];
  const home = files.find((f) => f.path === declaringHome(files));
  if (!home) return [];
  return [{ path: home.path, resources: declareResource(home.document.resources, name, kind) }];
}

/** Declare each of `names` in the project's home file, skipping any already declared as `kind`. */
export function declareAllInProject(
  files: readonly ProjectFile[],
  names: readonly string[],
  kind: "env" | "template",
): ResourcesChange[] {
  const home = files.find((f) => f.path === declaringHome(files));
  if (!home) return [];
  const pending = names.filter(
    (n) => !files.some((f) => declaresAs(f.document.resources, n, kind)),
  );
  if (pending.length === 0) return [];
  const resources = pending.reduce(
    (r, name) => declareResource(r, name, kind),
    home.document.resources,
  );
  return [{ path: home.path, resources: resources ?? EMPTY }];
}

/**
 * Drop `name` from every file that declares it. The runtime merges declarations
 * first-wins, so leaving one in another file would leave the resource declared.
 */
export function undeclareInProject(
  files: readonly ProjectFile[],
  name: string,
): ResourcesChange[] {
  return files
    .filter((f) => declares(f.document.resources, name))
    .map((f) => ({ path: f.path, resources: undeclareResource(f.document.resources, name) }));
}

/** Rename `oldName`'s declaration in every file that declares it. */
export function renameInProject(
  files: readonly ProjectFile[],
  oldName: string,
  newName: string,
  kind: "env" | "template",
): ResourcesChange[] {
  return files
    .filter((f) => declares(f.document.resources, oldName))
    .map((f) => ({
      path: f.path,
      resources: renameDeclaration(f.document.resources, oldName, newName, kind),
    }));
}
