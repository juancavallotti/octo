import { z } from "zod";
import { parse as parseYaml } from "yaml";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "../backend";
import { addFlow, deleteFlow, getFlow, updateFlow } from "../flows";
import { errorResult, guard, jsonResult, textResult } from "../result";

/**
 * The integration-authoring tools: list the catalogue, open one to read its
 * definition, create/update whole definitions, and edit a single flow within one.
 * They delegate straight to the host's injected {@link OctoMcpConfig.store}; the
 * run-control tools live separately (they also need the per-session namespace).
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
      guard(async () => jsonResult(await config.validate(definition))),
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

  /**
   * Write one flow back, having spliced it into the integration's definition. Every
   * mutating flow tool ends here, so they all get the same two properties: the rest of
   * the file is untouched (the splice is an AST edit — see ../flows.ts), and a splice
   * that would produce a config the runtime cannot load is refused rather than saved.
   *
   * Validating here rather than in the caller is what makes the guarantee worth having:
   * these tools exist to let an agent edit a flow without holding the whole file in its
   * head, which means it also cannot see what it just broke elsewhere.
   */
  async function writeSpliced(id: string, definition: string) {
    const { valid, errors } = await config.validate(definition);
    if (!valid) {
      return errorResult(
        `that edit would leave the integration invalid, so nothing was saved:\n• ${errors.join("\n• ")}`,
      );
    }
    return jsonResult(await store.update(id, undefined, definition));
  }

  server.registerTool(
    "get_flow",
    {
      title: "Get one flow",
      description:
        "Read ONE flow's YAML out of a saved integration, exactly as it appears in the file (comments and all). Use this with `update_flow` to change a single flow without reproducing the rest of the definition. Use `list_flows` to discover names.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow's name."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const rec = await store.get(id);
        return textResult(getFlow(rec.definition, flow));
      }),
  );

  server.registerTool(
    "update_flow",
    {
      title: "Update one flow",
      description:
        "Replace ONE flow in a saved integration, in place. `definition` is the YAML of that flow alone (a mapping starting with `name:`), NOT the whole file and NOT a `flows:` list. Everything else in the integration — the other flows, the connectors, the comments — is left exactly as it was, so this is the tool to reach for whenever you are changing a single flow: `update_integration` would make you reproduce the whole file from memory and silently lose whatever you got wrong.\n\nGiving the replacement a different `name` renames the flow where it sits. The edit is validated against the whole spliced definition before it is saved, and rejected if it would leave the integration unloadable — so a broken edit costs you nothing. Errors when no flow answers to `flow`.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The name of the flow to replace."),
        definition: z
          .string()
          .min(1)
          .describe("The flow's new YAML — that flow alone, not the whole definition."),
      },
    },
    ({ id, flow, definition }) =>
      guard(async () => {
        const rec = await store.get(id);
        return writeSpliced(id, updateFlow(rec.definition, flow, definition));
      }),
  );

  server.registerTool(
    "add_flow",
    {
      title: "Add a flow",
      description:
        "Append ONE new flow to a saved integration. `definition` is the YAML of that flow alone (a mapping starting with `name:`), NOT the whole file. The rest of the integration is left exactly as it was. Errors when a flow of that name already exists — use `update_flow` to change an existing one. The result is validated before saving, so a bad flow cannot break the integration.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        definition: z
          .string()
          .min(1)
          .describe("The new flow's YAML — that flow alone, not the whole definition."),
      },
    },
    ({ id, definition }) =>
      guard(async () => {
        const rec = await store.get(id);
        return writeSpliced(id, addFlow(rec.definition, definition));
      }),
  );

  server.registerTool(
    "delete_flow",
    {
      title: "Delete a flow",
      description:
        "Remove ONE flow from a saved integration by name, leaving the rest of the file untouched. Errors when no flow answers to that name. The result is validated before saving, so deleting a flow another one still references is refused rather than saved.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The name of the flow to remove."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const rec = await store.get(id);
        return writeSpliced(id, deleteFlow(rec.definition, flow));
      }),
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
