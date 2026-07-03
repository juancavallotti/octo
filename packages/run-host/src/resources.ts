import YAML from "yaml";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname, normalize, resolve, sep } from "node:path";
import { randomUUID } from "node:crypto";

/**
 * Resource staging for editor/MCP dev runs. A run's config may declare resources
 * (env files + templates under a top-level `resources:` section) that the Go
 * runtime loads relative to the config file's own directory. The editor and MCP
 * spawn `octo` through this package, writing the config into a per-namespace dir —
 * so the platform's resources have to be materialized into that same dir before
 * the runner starts. The host supplies a {@link ResourceProvider}; this module
 * parses the declared names, resolves them, and writes the bytes safely under the
 * run dir, mirroring the standalone runtime loader's path containment.
 */

/** A single resolved resource: its declared (path-like) name and its bytes. */
export interface ResourceFile {
  /** The declared resource id, e.g. `.env.dev` or `templates/welcome.tmpl`. May contain `/`. */
  name: string;
  content: string;
}

/**
 * Resolves the resource files a run's config declares. Given the names parsed
 * from the config, returns those the host can supply; names it can't resolve are
 * simply omitted (the runtime reports missing templates itself at load, and skips
 * missing env resources silently). Implemented per host: the standalone app reads
 * the filesystem, the platform fetches from the orchestrator.
 */
export type ResourceProvider = (names: string[]) => Promise<ResourceFile[]>;

/**
 * The dev-env resource. An integration's dev credentials live here (in the host's
 * resource store) instead of the browser, so an editor or MCP run reads them from
 * the store rather than from caller-supplied env. run-host always requests it and
 * declares it in the run config, so a run picks it up transparently.
 */
export const DEV_ENV_RESOURCE = ".env.dev";

interface resourcesDecl {
  resources?: {
    env?: unknown;
    templates?: Array<{ resource?: unknown }> | unknown;
  };
}

/**
 * The resource names a rendered config declares: `resources.env[]` plus each
 * `resources.templates[].resource`, trimmed and de-duplicated (first-seen order).
 * A malformed document yields `[]` — the runtime is the real judge of the config.
 */
export function parseDeclaredResources(yaml: string): string[] {
  let decl: resourcesDecl;
  try {
    decl = (YAML.parse(yaml) ?? {}) as resourcesDecl;
  } catch {
    return [];
  }
  const out: string[] = [];
  const seen = new Set<string>();
  const add = (raw: unknown) => {
    if (typeof raw !== "string") return;
    const name = raw.trim();
    if (name === "" || seen.has(name)) return;
    seen.add(name);
    out.push(name);
  };
  const res = decl.resources;
  if (res && typeof res === "object") {
    if (Array.isArray(res.env)) res.env.forEach(add);
    if (Array.isArray(res.templates)) {
      for (const t of res.templates) {
        if (t && typeof t === "object") add((t as { resource?: unknown }).resource);
      }
    }
  }
  return out;
}

/** Order-insensitive equality over two resource-name lists. */
export function sameNameSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((n) => set.has(n));
}

/**
 * The resource names a run effectively loads: those the config declares plus the
 * always-present {@link DEV_ENV_RESOURCE}. Used both to request files from the
 * provider and to detect a resource-list change on sync.
 */
export function effectiveResourceNames(yaml: string): string[] {
  const declared = parseDeclaredResources(yaml);
  return declared.includes(DEV_ENV_RESOURCE)
    ? declared
    : [...declared, DEV_ENV_RESOURCE];
}

/**
 * Append {@link DEV_ENV_RESOURCE} to a config's `resources.env` (last, so dev
 * values override other env resources and declared defaults) unless already
 * present, returning the re-rendered YAML. The run config is machine-rendered and
 * ephemeral, so re-stringifying (which may drop comments/reorder) is harmless. A
 * malformed config is returned untouched for the runtime to report.
 */
export function injectDevEnvResource(yaml: string): string {
  let doc: Record<string, unknown>;
  try {
    doc = (YAML.parse(yaml) ?? {}) as Record<string, unknown>;
  } catch {
    return yaml;
  }
  if (doc === null || typeof doc !== "object" || Array.isArray(doc)) return yaml;
  const resources = (doc.resources ??= {}) as { env?: unknown };
  const env = Array.isArray(resources.env) ? [...(resources.env as unknown[])] : [];
  if (!env.includes(DEV_ENV_RESOURCE)) env.push(DEV_ENV_RESOURCE);
  resources.env = env;
  return YAML.stringify(doc);
}

/**
 * Map a path-like resource name to an absolute path under `dir`, mirroring the Go
 * standalone loader's `resolve()`: clean the name as an absolute path (so `..`
 * can't climb above root), join it under `dir`, and assert the result stays within
 * `dir`. Throws on any name that escapes.
 */
export function stagedPathFor(dir: string, name: string): string {
  const root = resolve(dir);
  const cleaned = normalize("/" + name); // absolute-clean: strips leading .. segments
  const path = resolve(root, "." + cleaned);
  if (path !== root && !path.startsWith(root + sep)) {
    throw new Error(`resource name escapes the run directory: ${name}`);
  }
  return path;
}

/** Atomic write (sibling temp + rename), matching session.ts writeConfig, so the
 * runtime's directory watcher observes a single event per staged file. */
async function atomicWrite(path: string, content: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  const tmp = `${path}.tmp-${randomUUID()}`;
  await writeFile(tmp, content, "utf8");
  await rename(tmp, path);
}

/**
 * Write each provided file at its relative name under `dir`, creating parent
 * directories as needed. Only names in `declared` are written, so a provider can't
 * smuggle extra files onto disk. Returns the absolute paths written (for cleanup).
 */
export async function stageResources(
  dir: string,
  files: ResourceFile[],
  declared: string[],
): Promise<string[]> {
  const allowed = new Set(declared);
  const written: string[] = [];
  for (const f of files) {
    if (!allowed.has(f.name)) continue;
    const path = stagedPathFor(dir, f.name);
    await atomicWrite(path, f.content);
    written.push(path);
  }
  return written;
}

/**
 * Resolve and stage a run's declared resources into `dir`. Returns the declared
 * name set and the absolute paths written, so the session can track them for
 * re-staging on a resource-list change and for cleanup on stop.
 */
export async function resolveAndStage(
  dir: string,
  yaml: string,
  provider: ResourceProvider | undefined,
): Promise<{ declared: string[]; staged: string[] }> {
  const declared = effectiveResourceNames(yaml);
  if (!provider) return { declared, staged: [] };
  const files = await provider(declared);
  const staged = await stageResources(dir, files, declared);
  return { declared, staged };
}
