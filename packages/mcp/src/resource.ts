import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "./backend";

/** The URI under which the runtime capability catalogue is published. */
export const RUNTIME_SCHEMA_URI = "octo://runtime/schema";

/**
 * Publish the runtime capability catalogue (the host's `capabilities.json` from
 * `@octo/editor`) as a read-only resource. A consumer LLM reads this first to learn
 * the valid block and connector types and their settings before authoring a
 * definition with `create_integration`.
 */
export function registerRuntimeSchemaResource(
  server: McpServer,
  config: OctoMcpConfig,
): void {
  server.registerResource(
    "octo-runtime-schema",
    RUNTIME_SCHEMA_URI,
    {
      title: "Octo runtime schema",
      description:
        "The catalogue of blocks and connectors the Octo runtime supports, with their configurable fields. Read this before authoring an integration.",
      mimeType: "application/json",
    },
    (uri) => ({
      contents: [
        {
          uri: uri.href,
          mimeType: "application/json",
          text: JSON.stringify(config.runtimeSchema, null, 2),
        },
      ],
    }),
  );
}
