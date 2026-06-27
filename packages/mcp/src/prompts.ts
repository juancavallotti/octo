import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { RUNTIME_SCHEMA_URI } from "./resource";
import { EXAMPLES } from "./examples";

/**
 * Authoring guidance surfaced as MCP prompts. `create-integration` walks a
 * consumer LLM through building and testing an integration end to end;
 * `integration-examples` hands it a couple of worked, runnable definitions.
 */
export function registerPrompts(server: McpServer): void {
  server.registerPrompt(
    "create-integration",
    {
      title: "Create an Octo integration",
      description:
        "Step-by-step guidance for authoring, validating, and running an Octo integration over this MCP server.",
      argsSchema: {
        goal: z
          .string()
          .optional()
          .describe("What the integration should do (woven into the guidance)."),
      },
    },
    ({ goal }) => ({
      messages: [
        {
          role: "user",
          content: { type: "text", text: createIntegrationGuide(goal) },
        },
      ],
    }),
  );

  server.registerPrompt(
    "integration-examples",
    {
      title: "Octo integration examples",
      description:
        "A couple of complete, runnable Octo integration definitions to copy from.",
    },
    () => ({
      messages: [
        { role: "user", content: { type: "text", text: examplesText() } },
      ],
    }),
  );
}

function createIntegrationGuide(goal?: string): string {
  const objective = goal?.trim()
    ? `Your objective: ${goal.trim()}\n\n`
    : "";
  return `You are authoring an Octo integration through this MCP server. An integration is a runtime-YAML document with these top-level keys:

- service: { name }            — the integration's name (required).
- env: [ { name, default } ]   — env vars; declare one before referencing it as \${NAME}.
- connectors: [ { name, type, settings } ]  — sources and clients (cron, http, logger, http-client, db, llm-*, …).
- processors: [ { name, type, settings } ]  — optional named blocks a flow references by \`ref\`.
- flows: [ { name, source: { connector, type, settings }, process: [ blocks ] } ]
                                — a flow's \`source\` is what triggers it; \`process\` is the ordered blocks that run.

${objective}Follow this loop:

1. Read the "${RUNTIME_SCHEMA_URI}" resource to learn the exact block/connector types and their settings — do not guess type names or fields.
2. Read the \`integration-examples\` prompt for two complete, runnable definitions.
3. Draft the definition, then call \`create_integration\` (new) or \`update_integration\` (existing).
4. Call \`can_start_integration\` and fix anything it reports under \`errors\` before running.
5. Call \`run_integration\`. If the integration declares an HTTP_PORT (a networked \`http\` connector), the result includes a \`testUrl\` you can curl to exercise its endpoints; otherwise it runs internally (e.g. cron-driven).
6. Call \`get_run_logs\` to observe behavior, iterate with \`update_integration\` + \`run_integration\`, and \`stop_integration\` when done.

Tips:
- To make an integration testable over HTTP, add an \`http\` connector and declare HTTP_PORT in \`env\`; use that connector as a flow's source.
- Keep \`service.name\` stable; renaming may change the integration's id.
- CEL expressions (e.g. log messages, payloads) can read body, vars, eventID, and correlationID.`;
}

function examplesText(): string {
  const blocks = EXAMPLES.map(
    (e) => `## ${e.title}\n${e.summary}\n\n\`\`\`yaml\n${e.definition}\`\`\``,
  );
  return [
    "Two complete Octo integration definitions to adapt. Read the runtime schema resource for the full catalogue of blocks and connectors.",
    ...blocks,
  ].join("\n\n");
}
