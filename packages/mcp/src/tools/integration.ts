import { z } from "zod";
import { parse as parseYaml } from "yaml";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "../backend";
import { guard, jsonResult } from "../result";

/**
 * The integration-authoring tools: list the catalogue, open one to read its
 * definition, and create/update definitions. They delegate straight to the host's
 * injected {@link OctoMcpConfig.store}; the run-control tools live separately
 * (they also need the per-session namespace).
 */
export function registerIntegrationTools(
  server: McpServer,
  config: OctoMcpConfig,
): void {
  const { store } = config;

  server.registerTool(
    "list_integrations",
    {
      title: "List integrations",
      description:
        "List every saved integration as { id, name }. Use `open_integration` to read one's definition.",
      inputSchema: {},
    },
    () => guard(async () => jsonResult(await store.list())),
  );

  server.registerTool(
    "open_integration",
    {
      title: "Open integration",
      description:
        "Read one integration by id, returning { id, name, definition }. The definition is the runtime YAML the integration runs.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) => guard(async () => jsonResult(await store.get(id))),
  );

  server.registerTool(
    "list_flows",
    {
      title: "List flows",
      description:
        "List the flows declared in a saved integration as { name, source } — `source` is the flow's trigger type (e.g. http, cron, queue, events) or null for a sourceless flow callable by name. Use this to discover flow names for `invoke_flow`.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rec = await store.get(id);
        return jsonResult(listFlows(rec.definition));
      }),
  );

  server.registerTool(
    "validate_definition",
    {
      title: "Validate a definition",
      description:
        "Validate a draft definition (raw runtime YAML) against the runtime schema WITHOUT saving it, returning descriptive errors. Use this while authoring to check a draft before create_integration/update_integration. Best-effort: a clean result still isn't a guarantee the runtime will load it (see get_run_logs after a run).",
      inputSchema: {
        definition: z
          .string()
          .min(1)
          .describe("The runtime YAML to validate (service, connectors, flows)."),
      },
    },
    ({ definition }) =>
      guard(async () => jsonResult(config.validate(definition))),
  );

  server.registerTool(
    "create_integration",
    {
      title: "Create integration",
      description:
        "Create a new integration from a name and a runtime-YAML definition. Call `getSchema` first to know the valid blocks and connectors (and `getExamples` for idiomatic usage). Returns the created { id, name, definition }.",
      inputSchema: {
        name: z.string().min(1).describe("Display name for the integration."),
        definition: z
          .string()
          .min(1)
          .describe("Runtime YAML (service, connectors, flows)."),
      },
    },
    ({ name, definition }) =>
      guard(async () => jsonResult(await store.create(name, definition))),
  );

  server.registerTool(
    "update_integration",
    {
      title: "Update integration",
      description:
        "Overwrite an existing integration's definition (and optionally rename it). Returns the updated { id, name, definition } — the id may change if a rename re-slugs it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id to update."),
        name: z
          .string()
          .min(1)
          .optional()
          .describe("New display name; omit to keep the current name."),
        definition: z
          .string()
          .min(1)
          .describe("The new runtime YAML definition."),
      },
    },
    ({ id, name, definition }) =>
      guard(async () => jsonResult(await store.update(id, name, definition))),
  );
}

/** A flow's name and the type of its trigger (null when it has no source). */
interface FlowSummary {
  name: string;
  source: string | null;
}

/**
 * Parse a runtime-YAML definition and summarize its top-level flows. A flow's
 * `source.type` (its trigger, e.g. http/cron/queue/events) is reported when present;
 * a sourceless flow (callable by name via flow-ref/invoke) reports `source: null`.
 * Returns [] when the definition has no parseable flows.
 */
export function listFlows(definition: string): FlowSummary[] {
  const doc = parseYaml(definition) as unknown;
  const flows = (doc as { flows?: unknown } | null)?.flows;
  if (!Array.isArray(flows)) return [];
  const out: FlowSummary[] = [];
  for (const flow of flows) {
    if (typeof flow !== "object" || flow === null) continue;
    const f = flow as { name?: unknown; source?: { type?: unknown } | null };
    if (typeof f.name !== "string") continue;
    const type = f.source?.type;
    out.push({ name: f.name, source: typeof type === "string" ? type : null });
  }
  return out;
}
