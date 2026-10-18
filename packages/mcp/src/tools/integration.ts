import { z } from "zod";
import { parse as parseYaml } from "yaml";
import { isConfigFileName } from "@octo/editor/runtime";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import type { IntegrationRecord, OctoMcpConfig, StoredFile } from "../backend";
import { addFlow, deleteFlow, flowNameOf, flowNames, getFlow, updateFlow } from "../flows";
import { renameFlowMeta } from "./meta";
import { errorResult, guard, jsonResult, textResult } from "../result";

/**
 * The integration-authoring tools: list the catalogue, open one to read its files,
 * create one, read and write its config files, and edit a single flow wherever it
 * lives. An integration is a set of config files the runtime merges into one
 * application — a flow in one may use a connection declared in another — so every
 * write is validated against the whole set before it is saved. They delegate straight
 * to the host's injected {@link OctoMcpConfig.store}; the run-control tools live
 * separately (they also need the per-session namespace).
 */
export function registerIntegrationTools(
  server: McpServer,
  config: OctoMcpConfig,
): void {
  const { store } = config;

  const filesSchema = z
    .array(
      z.object({
        path: z
          .string()
          .min(1)
          .describe("The file's name at the integration root, e.g. `octo.yaml` or `orders.yaml`."),
        content: z.string().describe("The file's runtime YAML."),
      }),
    )
    .min(1);

  server.registerTool(
    "list_integrations",
    {
      title: "List integrations",
      description:
        "List every saved integration as { id, name }. Use `open_integration` to read one's files.",
      inputSchema: {},
    },
    () => guard(async () => jsonResult(await store.list())),
  );

  server.registerTool(
    "open_integration",
    {
      title: "Open integration",
      description:
        "Read one integration by id, returning { id, name, files: [{ path, content }] }. The files are the runtime YAML the integration runs: the runtime merges them into one application, so a flow in one file may use a connection, processor or flow declared in another.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) => guard(async () => jsonResult(await store.get(id))),
  );

  server.registerTool(
    "list_flows",
    {
      title: "List flows",
      description:
        "List the flows declared across a saved integration's files as { name, source, file } — `source` is the flow's trigger type (e.g. http, cron, queue, events) or null for a sourceless flow callable by name, and `file` is the file that declares it. Use this to discover flow names for `invoke_flow`.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rec = await store.get(id);
        return jsonResult(rec.files.flatMap((f) => listFlows(f.content).map((flow) => ({ ...flow, file: f.path }))));
      }),
  );

  server.registerTool(
    "validate_project",
    {
      title: "Validate config files",
      description:
        "Validate a draft set of config files, merged as the runtime merges them, WITHOUT saving it, returning descriptive errors. Use this while authoring to check a draft before create_integration/update_integration. Best-effort: a clean result still isn't a guarantee the runtime will load it (see get_run_logs after a run).",
      inputSchema: { files: filesSchema.describe("The integration's config files.") },
    },
    ({ files }) =>
      guard(async () => {
        const named = misnamed(files);
        if (named) return named;
        return jsonResult(await config.validate(files));
      }),
  );

  server.registerTool(
    "create_integration",
    {
      title: "Create integration",
      description:
        "Create a new integration from a name and its config files. Call `getSchema` first to know the valid blocks and connectors (and `getExamples` for idiomatic usage). A small integration is one file; a larger one reads best as `octo.yaml` holding `service`, `env`, `resources` and the shared connectors, with flows split by domain into sibling files. Returns the created { id, name, files }.",
      inputSchema: {
        name: z.string().min(1).describe("Display name for the integration."),
        files: filesSchema.describe("The config files (service, env, connectors, flows), at least one."),
      },
    },
    ({ name, files }) =>
      guard(async () => {
        const refused = await refuse(files);
        if (refused) return refused;
        return jsonResult(await store.create(name, files));
      }),
  );

  server.registerTool(
    "update_integration",
    {
      title: "Update integration",
      description:
        "Replace an existing integration's whole set of config files (and optionally rename it): a file it had that is not given is removed. Prefer `write_file` for one file and `update_flow` for one flow. Returns the updated { id, name, files } — the id may change if a rename re-slugs it.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id to update."),
        name: z.string().min(1).optional().describe("New display name; omit to keep the current name."),
        files: filesSchema.describe("Every config file the integration should have."),
      },
    },
    ({ id, name, files }) =>
      guard(async () => {
        const refused = await refuse(files, id);
        if (refused) return refused;
        return jsonResult(await store.update(id, name, files));
      }),
  );

  server.registerTool(
    "list_files",
    {
      title: "List config files",
      description:
        "List a saved integration's config files by path, with the flows each declares.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rec = await store.get(id);
        return jsonResult(rec.files.map((f) => ({ path: f.path, flows: flowNames(f.content) })));
      }),
  );

  server.registerTool(
    "get_file",
    {
      title: "Get one config file",
      description: "Read one of an integration's config files, exactly as stored (comments and all).",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        path: z.string().min(1).describe("The file's path, e.g. `orders.yaml`."),
      },
    },
    ({ id, path }) =>
      guard(async () => {
        const rec = await store.get(id);
        return textResult(fileOf(rec, path).content);
      }),
  );

  server.registerTool(
    "write_file",
    {
      title: "Write one config file",
      description:
        "Create or overwrite ONE config file of a saved integration, leaving the others as they are. The integration's files are validated together before saving, so a file that breaks a reference from another — or declares a name another file already has — is refused rather than saved.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        path: z
          .string()
          .min(1)
          .describe("The file's name at the integration root, ending in .yaml or .yml and not in _test."),
        content: z.string().describe("The file's runtime YAML."),
      },
    },
    ({ id, path, content }) =>
      guard(async () => {
        const rec = await store.get(id);
        return writeFiles(id, withFile(rec.files, path, content));
      }),
  );

  server.registerTool(
    "delete_file",
    {
      title: "Delete one config file",
      description:
        "Remove ONE config file from a saved integration. Refused when it is the last file, or when the files left behind would not validate — a flow-ref or connection the removed file declared that another still uses.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        path: z.string().min(1).describe("The file's path."),
      },
    },
    ({ id, path }) =>
      guard(async () => {
        const rec = await store.get(id);
        fileOf(rec, path);
        const rest = rec.files.filter((f) => f.path !== path);
        if (rest.length === 0) return errorResult("an integration needs at least one config file");
        return writeFiles(id, rest);
      }),
  );

  /** A set of files that cannot be saved, as the error result saying why; null when it can. */
  async function refuse(files: StoredFile[], id?: string): Promise<CallToolResult | null> {
    const named = misnamed(files);
    if (named) return named;
    const { valid, errors } = await config.validate(files, id);
    if (!valid) {
      return errorResult(
        `that would leave the integration invalid, so nothing was saved:\n• ${errors.join("\n• ")}`,
      );
    }
    return null;
  }

  /**
   * Save an integration's whole set of files, having validated it. Every mutating tool
   * ends here, so all of them get the same property: a write producing a project the
   * runtime cannot load is refused rather than saved. The validation belongs here
   * because a caller editing one file or one flow cannot see what it broke elsewhere.
   */
  async function writeFiles(id: string, files: StoredFile[]) {
    const refused = await refuse(files, id);
    if (refused) return refused;
    return jsonResult(await store.update(id, undefined, files));
  }

  server.registerTool(
    "get_flow",
    {
      title: "Get one flow",
      description:
        "Read ONE flow's YAML out of a saved integration, exactly as it appears in its file (comments and all), whichever file declares it. Use this with `update_flow` to change a single flow without reproducing the rest of the file. Use `list_flows` to discover names.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The flow's name."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const rec = await store.get(id);
        return textResult(getFlow(fileOfFlow(rec, flow).content, flow));
      }),
  );

  server.registerTool(
    "update_flow",
    {
      title: "Update one flow",
      description:
        "Replace ONE flow in a saved integration, in place, in whichever file declares it. `definition` is the YAML of that flow alone (a mapping starting with `name:`), NOT the whole file and NOT a `flows:` list. Everything else in the integration — the other flows, the connectors, the comments, the other files — is left exactly as it was, so this is the tool to reach for whenever you are changing a single flow: `write_file` would make you reproduce the whole file from memory and silently lose whatever you got wrong.\n\nGiving the replacement a different `name` renames the flow where it sits, and the editor's saved test inputs, mocks and spies move with it — block addresses begin with the flow's name, so they are re-rooted too. The edit is validated against the whole spliced definition before it is saved, and rejected if it would leave the integration unloadable — so a broken edit costs you nothing. Errors when no flow answers to `flow`.",
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
        const file = fileOfFlow(rec, flow);
        const result = await writeFiles(
          id,
          withFile(rec.files, file.path, updateFlow(file.content, flow, definition)),
        );
        if (result.isError) return result;
        return withRenamedMeta(id, flow, flowNameOf(definition), result);
      }),
  );

  /**
   * Carry the editor's bookkeeping across a rename, once the definition is safely saved.
   * A block address opens with its flow's name, so a rename orphans the meta entry and
   * every mock and spy address in it, leaving them listed and firing for nothing.
   *
   * The definition is already written by the time this runs, so a failing meta store must
   * not turn a successful edit into an error a caller might undo: it reports as a second
   * content block saying what did not happen.
   */
  async function withRenamedMeta(
    id: string,
    before: string,
    after: string,
    result: CallToolResult,
  ): Promise<CallToolResult> {
    if (!config.metaStore || before === after) return result;
    try {
      await renameFlowMeta(config.metaStore, id, before, after);
      return result;
    } catch (err) {
      return {
        ...result,
        content: [
          ...result.content,
          {
            type: "text" as const,
            text: `The flow was renamed, but the editor's saved inputs, mocks and spies for "${before}" could not be moved to "${after}": ${(err as Error).message}. They still name the old flow, so they will not fire — re-place them with set_mock/set_spy, or retry the rename.`,
          },
        ],
      };
    }
  }

  server.registerTool(
    "add_flow",
    {
      title: "Add a flow",
      description:
        "Append ONE new flow to a saved integration. `definition` is the YAML of that flow alone (a mapping starting with `name:`), NOT the whole file. `file` names the config file it goes in — an existing one, or a new one, which is created; it may be omitted when the integration has a single file. The rest of the integration is left exactly as it was. Errors when a flow of that name already exists in any file — use `update_flow` to change an existing one. The result is validated before saving, so a bad flow cannot break the integration.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        definition: z
          .string()
          .min(1)
          .describe("The new flow's YAML — that flow alone, not the whole definition."),
        file: z
          .string()
          .min(1)
          .optional()
          .describe("The config file to add it to, e.g. `orders.yaml`; required when the integration has several."),
      },
    },
    ({ id, definition, file }) =>
      guard(async () => {
        const rec = await store.get(id);
        const name = flowNameOf(definition);
        const holder = rec.files.find((f) => flowNames(f.content).includes(name));
        if (holder) {
          return errorResult(
            `a flow named "${name}" already exists in ${holder.path} — use update_flow to change it, or rename this one`,
          );
        }
        const path = file ?? (rec.files.length === 1 ? rec.files[0].path : undefined);
        if (!path) {
          return errorResult(
            `this integration has several config files (${rec.files.map((f) => f.path).join(", ")}) — say which one the flow goes in with \`file\``,
          );
        }
        // A new file starts as an empty flow list, which is what addFlow appends to.
        const current = rec.files.find((f) => f.path === path)?.content ?? "flows: []\n";
        return writeFiles(id, withFile(rec.files, path, addFlow(current, definition)));
      }),
  );

  server.registerTool(
    "delete_flow",
    {
      title: "Delete a flow",
      description:
        "Remove ONE flow from a saved integration by name, from whichever file declares it, leaving the rest of that file untouched. Errors when no flow answers to that name. The result is validated before saving, so deleting a flow another one still references is refused rather than saved.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id."),
        flow: z.string().min(1).describe("The name of the flow to remove."),
      },
    },
    ({ id, flow }) =>
      guard(async () => {
        const rec = await store.get(id);
        const file = fileOfFlow(rec, flow);
        return writeFiles(id, withFile(rec.files, file.path, deleteFlow(file.content, flow)));
      }),
  );
}

/**
 * Files no integration could hold, as the error result saying why: a path the runtime
 * would not load as config, or one given twice. Null when every path is fine.
 */
function misnamed(files: StoredFile[]): CallToolResult | null {
  const bad = files.filter((f) => !isConfigFileName(f.path)).map((f) => f.path);
  if (bad.length > 0) {
    return errorResult(
      `not config file names: ${bad.join(", ")} — a config file is a .yaml or .yml at the integration root that does not end in _test`,
    );
  }
  const seen = new Set<string>();
  const dup = files.find((f) => (seen.has(f.path) ? true : (seen.add(f.path), false)));
  return dup ? errorResult(`"${dup.path}" is given more than once`) : null;
}

/** The file at `path`, or an error naming the files there are. */
function fileOf(rec: IntegrationRecord, path: string): StoredFile {
  const file = rec.files.find((f) => f.path === path);
  if (!file) {
    throw new Error(`no file "${path}" in this integration (it has: ${rec.files.map((f) => f.path).join(", ")})`);
  }
  return file;
}

/** The file that declares the flow `name`, or an error naming the flows there are. */
function fileOfFlow(rec: IntegrationRecord, name: string): StoredFile {
  const file = rec.files.find((f) => flowNames(f.content).includes(name));
  if (!file) {
    const known = rec.files.flatMap((f) => flowNames(f.content)).map((n) => `"${n}"`);
    throw new Error(`no flow named "${name}" in this integration (it has: ${known.join(", ") || "none"})`);
  }
  return file;
}

/** The files with `content` at `path`, added when the path is new. */
function withFile(files: StoredFile[], path: string, content: string): StoredFile[] {
  return files.some((f) => f.path === path)
    ? files.map((f) => (f.path === path ? { path, content } : f))
    : [...files, { path, content }];
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
