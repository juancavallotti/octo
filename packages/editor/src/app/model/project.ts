import YAML from "yaml";
import {
  findBlock,
  findFlow,
  type EditorDocument,
  type EnvVar,
  type Resources,
  type TemplateResource,
} from "./document";
import { fromConfig } from "./serialize";
import type { RuntimeConfig } from "./serializeTypes";
import { toFileYaml } from "./runConfig";

/**
 * A project is a folder of config files the runtime merges into one application. The
 * editor holds every file parsed, edits one at a time, and resolves everything that
 * crosses a file boundary — a connection, a flow-ref target, a template, an env
 * variable — against the project as the runtime will see it: its files merged.
 */

/** A config file as a host stores it: its name in the project folder and its YAML. */
export interface StoredFile {
  path: string;
  content: string;
}

/** One config file of a project, as the editor holds it. */
export interface ProjectFile {
  path: string;
  document: EditorDocument;
  /**
   * Whether this file carries the project's `service` identity. At most one file
   * does — the runtime refuses a second — so saving writes it into that file only.
   */
  declaresService: boolean;
  /**
   * The file as it was loaded: the document parsed from it, its text, and the service
   * name it declared. A file whose document is still that one is saved as that text,
   * byte for byte, so a save never reformats — or strips the comments from — a file
   * nobody touched.
   */
  loaded?: { document: EditorDocument; content: string; service?: string };
}

/** Order files the way the runtime merges them: by path. */
export function byPath<T extends { path: string }>(files: readonly T[]): T[] {
  return [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}

/**
 * Parse a project's config files. Each file's connections are read first, across the
 * whole project, so that a source in one file bound to a connection declared in
 * another parses as that binding rather than as a bare type.
 */
export function parseProject(files: readonly StoredFile[]): ProjectFile[] {
  const parsed = byPath(files).map((f) => ({
    path: f.path,
    content: f.content,
    config: (YAML.parse(f.content) ?? {}) as RuntimeConfig & { service?: { name?: unknown } },
  }));
  const connectors = new Map<string, string>();
  for (const { config } of parsed) {
    for (const c of config.connectors ?? []) {
      if (c.name && !connectors.has(c.name)) connectors.set(c.name, c.type ?? "");
    }
  }
  return parsed.map(({ path, content, config }) => {
    const document = fromConfig(config, connectors);
    const service = config.service?.name;
    return {
      path,
      document,
      declaresService: config.service != null,
      loaded: { document, content, service: typeof service === "string" ? service : undefined },
    };
  });
}

/**
 * The project as the runtime sees it: every file's flows, connections and processors
 * together, and its env and resources folded the way the runtime folds them — the
 * first declaration, by file name, wins. Node ids are unique across files, so a node
 * found in the view can be traced back to its file with {@link fileOfFlow}.
 */
export function projectView(files: readonly ProjectFile[]): EditorDocument {
  const ordered = byPath(files).map((f) => f.document);
  return {
    flows: ordered.flatMap((d) => d.flows),
    connectors: ordered.flatMap((d) => d.connectors),
    processors: ordered.flatMap((d) => d.processors),
    env: firstByKey(ordered.flatMap((d) => d.env), (e: EnvVar) => e.name),
    resources: mergeResources(ordered.map((d) => d.resources)),
  };
}

function mergeResources(all: (Resources | undefined)[]): Resources {
  return {
    env: firstByKey(all.flatMap((r) => r?.env ?? []), (id) => id),
    templates: firstByKey(
      all.flatMap((r) => r?.templates ?? []),
      (t: TemplateResource) => t.as || t.resource,
    ),
  };
}

function firstByKey<T>(items: T[], key: (item: T) => string): T[] {
  const seen = new Set<string>();
  return items.filter((item) => {
    const k = key(item);
    if (seen.has(k)) return false;
    seen.add(k);
    return true;
  });
}

/**
 * The path of the file holding the node `id` — a flow at any depth, a block, or a
 * connection — if any file does. Ids are minted per node and never shared, so the
 * answer is unique.
 */
export function fileOfNode(files: readonly ProjectFile[], id: string): string | undefined {
  return files.find(
    ({ document: d }) =>
      d.connectors.some((c) => c.id === id) ||
      findFlow(d, id) !== undefined ||
      findBlock(d, id) !== undefined,
  )?.path;
}

/**
 * Which file carries the project's `service` identity: the one that declares it, else
 * the first by name, so a project always ends up with exactly one.
 */
export function serviceFile(files: readonly ProjectFile[]): string | undefined {
  return (files.find((f) => f.declaresService) ?? byPath(files)[0])?.path;
}

/**
 * Serialize every file of a project. `name` is the project's service name, written
 * into {@link serviceFile} and nowhere else; each file's sources resolve against the
 * whole project's connections.
 */
export function serializeProject(files: readonly ProjectFile[], name: string): StoredFile[] {
  return byPath(files).map((f) => ({ path: f.path, content: serializeFile(files, f.path, name) }));
}

/**
 * Serialize one file of a project, as {@link serializeProject} would. A file left as
 * it was loaded comes back as the text it was loaded from — unless it carries the
 * service identity and the project has been renamed since.
 */
export function serializeFile(files: readonly ProjectFile[], path: string, name: string): string {
  const file = files.find((f) => f.path === path);
  if (!file) return "";
  const owner = path === serviceFile(files);
  const { loaded } = file;
  if (loaded && loaded.document === file.document && (!owner || loaded.service === name)) {
    return loaded.content;
  }
  return toFileYaml(file.document, {
    connectors: projectView(files).connectors,
    service: owner ? name : undefined,
  });
}

/**
 * Whether two readings of a project are the same: the same files, each holding the
 * same document. Every edit produces a fresh document, so identity is the test.
 */
export function sameFiles(a: readonly ProjectFile[], b: readonly ProjectFile[]): boolean {
  if (a.length !== b.length) return false;
  const byName = new Map(a.map((f) => [f.path, f.document]));
  return b.every((f) => byName.get(f.path) === f.document);
}

/** Nothing worth persisting yet: no flow has a source or a step, and nothing is declared. */
export function isEmptyProject(files: readonly ProjectFile[]): boolean {
  return files.every(
    ({ document: d }) =>
      d.flows.every((f) => !f.source && f.process.length === 0) &&
      d.connectors.length === 0 &&
      d.env.length === 0,
  );
}

/** Whether `path` names a file the runtime would load as config. */
export function isConfigFileName(path: string): boolean {
  return /^[^/\\]+\.ya?ml$/i.test(path) && !/_test\.ya?ml$/i.test(path);
}

/**
 * A project's files merged into one config, at the YAML level, by the rules the runtime
 * merges a folder by: connectors, processors and flows concatenated in file order; env
 * variables, env resources and templates folded, the first declaration winning; one
 * `service`. For readers that want the project as one definition — a run, a listing —
 * without the editor's model in between. Duplicates are kept, for the runtime to refuse
 * by name. A single file comes back verbatim.
 */
export function mergeConfigFiles(files: readonly StoredFile[]): string {
  const ordered = byPath(files);
  if (ordered.length === 1) return ordered[0].content;
  const out: Record<string, unknown> = {};
  const lists: Record<string, unknown[]> = { connectors: [], processors: [], flows: [] };
  const env: unknown[] = [];
  const envRes: unknown[] = [];
  const templates: unknown[] = [];
  for (const f of ordered) {
    const doc = (YAML.parse(f.content) ?? {}) as Record<string, unknown>;
    for (const [key, value] of Object.entries(doc)) {
      if (key in lists) lists[key].push(...asList(value));
      else if (key === "env") env.push(...asList(value));
      else if (key === "resources") {
        const r = (value ?? {}) as { env?: unknown; templates?: unknown };
        envRes.push(...asList(r.env));
        templates.push(...asList(r.templates));
      } else if (!(key in out)) out[key] = value;
    }
  }
  const named = (v: unknown, k: string) => String((v as Record<string, unknown> | null)?.[k] ?? "");
  if (env.length) out.env = firstByKey(env, (e) => named(e, "name"));
  const resources: Record<string, unknown> = {};
  if (envRes.length) resources.env = firstByKey(envRes, String);
  if (templates.length) resources.templates = firstByKey(templates, (t) => named(t, "as") || named(t, "resource"));
  if (Object.keys(resources).length) out.resources = resources;
  for (const [key, items] of Object.entries(lists)) if (items.length) out[key] = items;
  return YAML.stringify(out);
}

function asList(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}
