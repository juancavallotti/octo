import { z } from "zod";
import { parse as parseYaml } from "yaml";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig, ResourceRecord } from "../backend";
import { guard, jsonResult } from "../result";

/**
 * The resource-management tools: read an integration's env var keys, and CRUD its
 * stored resources (env files and templates). `list_env_keys` needs only the
 * integration store, so it's always registered; the CRUD tools require a
 * {@link OctoMcpConfig.resourceStore} and are skipped on a host without one.
 *
 * These let an agent supply the credentials/config an integration needs before
 * running it: discover the declared env vars with `list_env_keys`, then either
 * pass values via a run's `env`, or persist them as an `env` resource with
 * `create_resource`.
 */
export function registerResourceTools(
  server: McpServer,
  config: OctoMcpConfig,
): void {
  const { store, resourceStore } = config;

  server.registerTool(
    "list_env_keys",
    {
      title: "List env var keys",
      description:
        "List every environment variable key an integration makes available, so you know what to fill before running it. Returns { name, default, required, source } — `source` is \"declared\" for a var declared in the definition's `env:` list (referenceable as ${NAME} and {{ env.NAME }}), or \"resource:<name>\" for a KEY= line found in one of the integration's `env` resource files. Check this before `run_integration`: if a required var has no value, ask the user for it (pass via the run's `env`) or persist it with `create_resource` (kind \"env\").",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rec = await store.get(id);
        const resources = resourceStore ? await resourceStore.list(id) : [];
        return jsonResult(listEnvKeys(rec.definition, resources));
      }),
  );

  if (!resourceStore) return;

  server.registerTool(
    "list_resources",
    {
      title: "List resources",
      description:
        "List an integration's stored resources as { id, kind, name } — `kind` is \"env\" (a .env file merged into the runtime environment) or \"template\" (a text template, may embed {{ }} expressions). Use `open_resource` to read one's content.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rows = await resourceStore.list(id);
        return jsonResult(
          rows.map(({ id, kind, name }) => ({ id, kind, name })),
        );
      }),
  );

  server.registerTool(
    "open_resource",
    {
      title: "Open resource",
      description:
        "Read one of an integration's resources by id, returning { id, integrationId, kind, name, content }.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        resourceId: z.string().min(1).describe("The resource id."),
      },
    },
    ({ id, resourceId }) =>
      guard(async () => jsonResult(await resourceStore.get(id, resourceId))),
  );

  server.registerTool(
    "create_resource",
    {
      title: "Create resource",
      description:
        "Create a resource on an integration. `kind` is \"env\" (a .env-convention file whose KEY=value lines are merged into the runtime environment) or \"template\" (a text template rendered by the template-resource block, may embed {{ env.NAME }} / {{ body.* }} expressions). `name` is path-like (may contain '/') and unique per integration. Returns the created record.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        kind: z
          .enum(["env", "template"])
          .describe('The resource kind: "env" or "template".'),
        name: z
          .string()
          .min(1)
          .describe("Path-like resource name, e.g. \".env.dev\" or \"templates/welcome.tmpl\"."),
        content: z.string().describe("The file content."),
      },
    },
    ({ id, kind, name, content }) =>
      guard(async () =>
        jsonResult(await resourceStore.create(id, kind, name, content)),
      ),
  );

  server.registerTool(
    "update_resource",
    {
      title: "Update resource",
      description:
        "Update a resource's content, and optionally its kind or name. Omitted kind/name are kept as-is. Returns the updated record.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        resourceId: z.string().min(1).describe("The resource id to update."),
        content: z.string().describe("The new file content."),
        kind: z
          .enum(["env", "template"])
          .optional()
          .describe("New kind; omit to keep the current kind."),
        name: z
          .string()
          .min(1)
          .optional()
          .describe("New path-like name; omit to keep the current name."),
      },
    },
    ({ id, resourceId, content, kind, name }) =>
      guard(async () => {
        // The store replaces the whole record, so fill omitted fields from current.
        const current = await resourceStore.get(id, resourceId);
        return jsonResult(
          await resourceStore.update(
            id,
            resourceId,
            kind ?? current.kind,
            name ?? current.name,
            content,
          ),
        );
      }),
  );

  server.registerTool(
    "delete_resource",
    {
      title: "Delete resource",
      description:
        "Delete a resource from an integration by id. Returns { deleted: true }.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        resourceId: z.string().min(1).describe("The resource id to delete."),
      },
    },
    ({ id, resourceId }) =>
      guard(async () => {
        await resourceStore.remove(id, resourceId);
        return jsonResult({ deleted: true });
      }),
  );
}

/** One environment variable key available to an integration, and where it comes from. */
interface EnvKey {
  name: string;
  /** The declared default, the value in an env resource, or null when neither. */
  default: string | null;
  required: boolean;
  /** "declared" (the definition's `env:` list) or "resource:<name>" (an env file). */
  source: string;
}

/**
 * List every env var key an integration makes available: the vars declared in its
 * definition's top-level `env:` list, UNIONed with the KEY= lines of its `env`
 * resources. Declared vars win on name collision (they carry the schema's
 * default/required); resource-only keys are reported with the value found and
 * `required: false`. Sorted by name.
 */
export function listEnvKeys(
  definition: string,
  resources: ResourceRecord[],
): EnvKey[] {
  const byName = new Map<string, EnvKey>();

  for (const r of resources) {
    if (r.kind !== "env") continue;
    for (const [name, value] of parseDotenv(r.content)) {
      if (!byName.has(name)) {
        byName.set(name, {
          name,
          default: value,
          required: false,
          source: `resource:${r.name}`,
        });
      }
    }
  }

  // Declared vars overwrite any resource-sourced entry of the same name.
  for (const v of declaredEnv(definition)) byName.set(v.name, v);

  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** Parse the definition's top-level `env:` list into declared env keys. */
function declaredEnv(definition: string): EnvKey[] {
  let doc: unknown;
  try {
    doc = parseYaml(definition);
  } catch {
    return [];
  }
  const env = (doc as { env?: unknown } | null)?.env;
  if (!Array.isArray(env)) return [];
  const out: EnvKey[] = [];
  for (const entry of env) {
    if (typeof entry !== "object" || entry === null) continue;
    const e = entry as { name?: unknown; default?: unknown; required?: unknown };
    if (typeof e.name !== "string" || !e.name) continue;
    out.push({
      name: e.name,
      default: typeof e.default === "string" ? e.default : null,
      required: e.required === true,
      source: "declared",
    });
  }
  return out;
}

/**
 * Parse the KEY=value lines of a .env-convention file into [name, value] pairs.
 * Skips blanks and `#` comments, tolerates an `export ` prefix, and strips a
 * single layer of matching surrounding quotes from the value. Best-effort — it
 * only needs to surface the keys, not fully model dotenv escaping.
 */
function parseDotenv(content: string): [string, string][] {
  const out: [string, string][] = [];
  for (const raw of content.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const body = line.startsWith("export ") ? line.slice(7).trim() : line;
    const eq = body.indexOf("=");
    if (eq <= 0) continue;
    const name = body.slice(0, eq).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) continue;
    let value = body.slice(eq + 1).trim();
    if (value.length >= 2 && (value[0] === '"' || value[0] === "'") && value[value.length - 1] === value[0]) {
      value = value.slice(1, -1);
    }
    out.push([name, value]);
  }
  return out;
}
