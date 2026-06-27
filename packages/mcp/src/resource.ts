import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "./backend";
import { EXAMPLES } from "./examples";

/** The URI under which the runtime capability catalogue is published. */
export const RUNTIME_SCHEMA_URI = "octo://runtime/schema";

/** The URI of the worked-examples index. */
export const EXAMPLES_INDEX_URI = "octo://examples";

/** The resource URI of a single example. */
export function exampleUri(slug: string): string {
  return `${EXAMPLES_INDEX_URI}/${slug}`;
}

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

/**
 * Publish the worked examples: an index at `octo://examples` listing each
 * example's slug, title, summary, and the blocks it demonstrates, plus one
 * `octo://examples/<slug>` resource per example carrying the runtime YAML. A
 * consumer LLM scans the index to find an example using the blocks it needs, then
 * reads that example rather than guessing the syntax.
 */
export function registerExampleResources(server: McpServer): void {
  server.registerResource(
    "octo-examples-index",
    EXAMPLES_INDEX_URI,
    {
      title: "Octo integration examples",
      description:
        "Index of worked integration examples — each entry lists the blocks it demonstrates and the octo://examples/<slug> URI to read it.",
      mimeType: "application/json",
    },
    (uri) => ({
      contents: [
        {
          uri: uri.href,
          mimeType: "application/json",
          text: JSON.stringify(
            EXAMPLES.map((e) => ({
              uri: exampleUri(e.slug),
              slug: e.slug,
              title: e.title,
              summary: e.summary,
              blocks: e.blocks,
            })),
            null,
            2,
          ),
        },
      ],
    }),
  );

  for (const example of EXAMPLES) {
    server.registerResource(
      `octo-example-${example.slug}`,
      exampleUri(example.slug),
      {
        title: example.title,
        description: `${example.summary} Demonstrates: ${example.blocks.join(", ")}.`,
        mimeType: "application/yaml",
      },
      (uri) => ({
        contents: [
          {
            uri: uri.href,
            mimeType: "application/yaml",
            // Lead with the summary + block list as comments so the YAML is
            // self-describing when read on its own.
            text: `# ${example.title}\n# ${example.summary}\n# Demonstrates: ${example.blocks.join(", ")}\n\n${example.definition}`,
          },
        ],
      }),
    );
  }
}
