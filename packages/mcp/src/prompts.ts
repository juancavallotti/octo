import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { EXAMPLES_INDEX_URI, RUNTIME_SCHEMA_URI } from "./resource";

/**
 * Authoring guidance surfaced as an MCP prompt. `create-integration` walks a
 * consumer LLM through building and testing an integration end to end, pointing it
 * at the runtime-schema and worked-example resources rather than guessing syntax.
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
}

function createIntegrationGuide(goal?: string): string {
  const objective = goal?.trim()
    ? `Your objective: ${goal.trim()}\n\n`
    : "";
  return `You are authoring an Octo integration through this MCP server. An integration is a runtime-YAML document with these top-level keys:

- service: { name }            — the integration's name (required).
- env: [ { name, default } ]   — env vars; declare one before referencing it as \${NAME}.
- connectors: [ { name, type, settings } ]  — sources and clients (cron, http, logger, http-client, database, llm-*, …).
- processors: [ { name, type, settings } ]  — optional named blocks a flow references by \`ref\`.
- flows: [ { name, source: { connector, type, settings }, process: [ blocks ] } ]
                                — a flow's \`source\` is what triggers it; \`process\` is the ordered blocks that run.

${objective}Follow this loop:

1. Read the "${RUNTIME_SCHEMA_URI}" resource to learn the exact block/connector types and their settings — do not guess type names or fields. Composite blocks (if/switch/foreach/handle-errors/flow-ref/ai-router) carry their sub-fields at the block top level, not under \`settings\`.
2. Read the "${EXAMPLES_INDEX_URI}" resource: it lists each worked example and the blocks it demonstrates. Read the "${EXAMPLES_INDEX_URI}/<slug>" resource(s) covering the blocks you need and adapt them — don't invent syntax.
3. Draft the definition and call \`validate_definition\` to check it against the runtime schema BEFORE saving; fix the descriptive \`errors\` it returns and re-validate until clean. Then call \`create_integration\` (new) or \`update_integration\` (existing).
4. Call \`can_start_integration\` — a best-effort pre-flight on the saved integration. Fix what it reports under \`errors\`, but treat it (and \`validate_definition\`) as advisory: the runtime is the final judge, so a definition it flags may still run (and a clean one may still fail at load).
5. Call \`run_integration\`. If the integration declares an HTTP_PORT (a networked \`http\` connector), the result includes a \`testUrl\` you can curl to exercise its endpoints; otherwise it runs internally (e.g. cron-driven). Read \`get_run_logs\` to see the runtime's own load errors.
6. Call \`get_run_logs\` to observe behavior, iterate with \`update_integration\` + \`run_integration\`, and \`stop_integration\` when done.

Tips:
- To make an integration testable over HTTP, add an \`http\` connector and declare HTTP_PORT in \`env\`; use that connector as a flow's source.
- Keep \`service.name\` stable; renaming may change the integration's id.
- CEL expressions (e.g. log messages, payloads) can read body, vars, eventID, and correlationID.`;
}
