import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { resolveRuntimeSchema, type OctoMcpConfig } from "../backend";
import { errorResult, guard, jsonResult } from "../result";
import { EXAMPLES, getExample } from "../examples";
import {
  CEL_FUNCTIONS,
  CEL_VARIABLES,
  celLibraries,
  getCelFunctionsNamed,
  getCelLibrary,
} from "../cel";

/**
 * Documentation tools: serve the runtime schema and the worked examples on demand,
 * one element at a time, as **tools** rather than resources. Some MCP clients
 * (notably Claude Desktop) only surface tools, not resources/prompts, so an author
 * there is blind to the `octo://runtime/schema` + `octo://examples` resources.
 * These tools expose the same catalogues focusedly — `getSchema()` lists the block
 * and connector types, `getSchema(elementName)` returns one full spec, and the
 * example pair mirrors it — so the client never has to pull (or the model wade
 * through) the whole schema blob to author a single block. The resources stay
 * registered too (see resource.ts) for clients that do consume them.
 * `getCelFunctions` similarly serves the CEL variables/functions in scope for
 * message expressions (which have no resource equivalent at all).
 */
export function registerDocsTools(
  server: McpServer,
  config: OctoMcpConfig,
): void {
  server.registerTool(
    "getSchema",
    {
      title: "Get runtime schema",
      description:
        "The catalogue of blocks and connectors the runtime supports. Call with no argument for a compact index ({ type, kind, label, category, description }); pass `elementName` (a block or connector type, e.g. \"log\" or \"http\") to get that one element's full spec including its fields. Read this before authoring a definition instead of guessing type names or fields. For a composite block (if, switch, foreach, fork, handle-errors, ai-agent, …), the spec also carries `addressBranches` — the branch names an `invoke_flow` breakpoint/spy/mock address must use to descend into it, e.g. `handle-errors[process].<block>`.",
      inputSchema: {
        elementName: z
          .string()
          .optional()
          .describe(
            "A block or connector type to fetch the full spec for; omit for the index.",
          ),
      },
    },
    ({ elementName }) =>
      guard(async () => {
        const schema = asRuntimeSchema(await resolveRuntimeSchema(config.runtimeSchema));
        if (!elementName) return jsonResult(schemaIndex(schema));
        const element = findElement(schema, elementName);
        if (!element) {
          return errorResult(
            `Unknown element "${elementName}". Call getSchema with no argument to list valid block and connector types.`,
          );
        }
        return jsonResult(element);
      }),
  );

  server.registerTool(
    "getExamples",
    {
      title: "Get worked examples",
      description:
        "Worked integration definitions demonstrating idiomatic block usage. Call with no argument for the index ({ slug, title, summary, blocks }); pass `exampleName` (a slug, e.g. \"slack-bot\") to get that example's full runtime YAML. Adapt an example rather than inventing syntax.",
      inputSchema: {
        exampleName: z
          .string()
          .optional()
          .describe("An example slug to fetch its full YAML; omit for the index."),
      },
    },
    ({ exampleName }) =>
      guard(async () => {
        if (!exampleName) {
          return jsonResult(
            EXAMPLES.map((e) => ({
              slug: e.slug,
              title: e.title,
              summary: e.summary,
              blocks: e.blocks,
            })),
          );
        }
        const example = getExample(exampleName);
        if (!example) {
          return errorResult(
            `Unknown example "${exampleName}". Call getExamples with no argument to list valid slugs.`,
          );
        }
        return jsonResult({
          slug: example.slug,
          title: example.title,
          summary: example.summary,
          blocks: example.blocks,
          definition: example.definition,
        });
      }),
  );

  server.registerTool(
    "getCelFunctions",
    {
      title: "Get CEL functions",
      description:
        "The CEL variables and functions available to Octo message expressions (log messages, payloads, if/switch conditions, connector fields). Call with no argument for { variables, functions, libraries }; pass `functionName` (e.g. \"fromFormData\" or \"math.round\") for one function's signature, summary, and example, or `library` (octo, strings, lists, encoders, math, comprehensions, sets, regex, optional) for that library's functions. A name two libraries declare on different receivers (reverse) comes back as { name, entries }. Use this to write expressions instead of guessing what's in scope. The CEL standard library (size, has, map, filter, startsWith, timestamp, …) is always available on top of these and is not listed here.",
      inputSchema: {
        functionName: z
          .string()
          .optional()
          .describe("A CEL function name to fetch one entry; omit for the full catalogue."),
        library: z
          .string()
          .optional()
          .describe("A library name to fetch just its functions; omit for all of them."),
      },
    },
    ({ functionName, library }) =>
      guard(async () => {
        if (functionName) {
          // A name can be declared by two libraries on different receivers
          // (reverse), so answer with every entry rather than an arbitrary one.
          const fns = getCelFunctionsNamed(functionName);
          if (fns.length === 0) {
            return errorResult(
              `Unknown CEL function "${functionName}". Call getCelFunctions with no argument to list the available functions.`,
            );
          }
          return jsonResult(fns.length === 1 ? fns[0] : { name: functionName, entries: fns });
        }
        if (library) {
          const functions = getCelLibrary(library);
          if (functions.length === 0) {
            return errorResult(
              `Unknown CEL library "${library}". Available: ${celLibraries().join(", ")}.`,
            );
          }
          return jsonResult({ library, functions });
        }
        return jsonResult({
          variables: CEL_VARIABLES,
          functions: CEL_FUNCTIONS,
          libraries: celLibraries(),
        });
      }),
  );
}

/**
 * The runtime schema is injected by the host as `unknown` (mcp stays decoupled
 * from `@octo/editor`), so narrow it structurally here — just the shape these
 * tools index. Unknown extra fields on each spec pass through untouched.
 */
interface SchemaField {
  name: string;
  [key: string]: unknown;
}
interface SchemaElement {
  type: string;
  label?: string;
  category?: string;
  description?: string;
  fields?: SchemaField[];
  settings?: SchemaField[];
  sources?: unknown[];
  [key: string]: unknown;
}
interface RuntimeSchema {
  blocks?: SchemaElement[];
  connectors?: SchemaElement[];
}

function asRuntimeSchema(raw: unknown): RuntimeSchema {
  return (raw ?? {}) as RuntimeSchema;
}

/** The compact index: every block and connector tagged with its kind. */
function schemaIndex(schema: RuntimeSchema): Array<{
  type: string;
  kind: "block" | "connector";
  label?: string;
  category?: string;
  description?: string;
}> {
  const entry = (el: SchemaElement, kind: "block" | "connector") => ({
    type: el.type,
    kind,
    label: el.label,
    category: el.category,
    description: el.description,
  });
  return [
    ...(schema.blocks ?? []).map((b) => entry(b, "block")),
    ...(schema.connectors ?? []).map((c) => entry(c, "connector")),
  ];
}

/** Find one block or connector by type, tagging the result with its kind. */
function findElement(
  schema: RuntimeSchema,
  name: string,
): (SchemaElement & { kind: "block" | "connector" }) | undefined {
  const block = (schema.blocks ?? []).find((b) => b.type === name);
  if (block) return { kind: "block", ...block };
  const connector = (schema.connectors ?? []).find((c) => c.type === name);
  if (connector) return { kind: "connector", ...connector };
  return undefined;
}
