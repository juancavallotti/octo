import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "./backend";
import { EXAMPLES_INDEX_URI, RUNTIME_SCHEMA_URI } from "./resource";

/**
 * Authoring guidance surfaced as an MCP prompt. `create-integration` walks a
 * consumer LLM through building and testing an integration end to end, pointing it
 * at the runtime-schema and worked-example resources (and the human docs, when the
 * host configures `docsUrl`) rather than guessing syntax.
 */
export function registerPrompts(server: McpServer, config: OctoMcpConfig): void {
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
          content: {
            type: "text",
            text: createIntegrationGuide(goal, config.docsUrl),
          },
        },
      ],
    }),
  );

  server.registerPrompt(
    "write-effective-integrations",
    {
      title: "Write effective Octo integrations",
      description:
        "Design best practices for Octo integrations: how to structure flows, when to use queues vs. topics, simplifying transforms, error handling, concurrency tuning, and visualizing the design for the user.",
      argsSchema: {
        focus: z
          .string()
          .optional()
          .describe("A specific concern to emphasize (woven into the guidance)."),
      },
    },
    ({ focus }) => ({
      messages: [
        {
          role: "user",
          content: {
            type: "text",
            text: effectiveIntegrationsGuide(focus, config.docsUrl),
          },
        },
      ],
    }),
  );
}

function createIntegrationGuide(goal?: string, docsUrl?: string): string {
  const objective = goal?.trim()
    ? `Your objective: ${goal.trim()}\n\n`
    : "";
  const docs = docsUrl?.trim()
    ? `\n\nReference documentation: ${docsUrl.trim()} — CEL expression syntax, the full block & connector reference, and connector configuration. Consult it when the runtime schema or examples don't make a field's meaning clear.`
    : "";
  return `You are authoring an Octo integration through this MCP server. An integration is a runtime-YAML document with these top-level keys:

- service: { name }            — the integration's name (required).
- env: [ { name, default } ]   — env vars; declare one before referencing it as \${NAME}.
- connectors: [ { name, type, settings } ]  — sources and clients (cron, http, logger, http-client, database, llm-*, …).
- processors: [ { name, type, settings } ]  — optional named blocks a flow references by \`ref\`.
- flows: [ { name, source: { connector, type, settings }, process: [ blocks ] } ]
                                — a flow's \`source\` is what triggers it; \`process\` is the ordered blocks that run.

Separately, an integration owns **resources**: \`env\` files (\`.env\`-convention, their KEY=value lines merged into the runtime environment) and \`template\` files (text templates the template-resource block renders, may embed \`{{ env.NAME }}\`/\`{{ body.* }}\`). Manage them with \`list_resources\`, \`open_resource\`, \`create_resource\`, \`update_resource\`, \`delete_resource\`.

${objective}Follow this loop:

1. Call \`getSchema\` (no argument) for the index of block/connector types, then \`getSchema(elementName)\` for the full spec of each type you'll use — do not guess type names or fields. (Clients that support resources can instead read "${RUNTIME_SCHEMA_URI}".) Composite blocks (if/switch/foreach/handle-errors/flow-ref/ai-router) carry their sub-fields at the block top level, not under \`settings\`, and their spec lists the \`addressBranches\` a debug address needs to reach a block nested inside them.
2. Call \`getExamples\` (no argument) for the index of worked examples and the blocks each demonstrates, then \`getExamples(slug)\` for the ones covering the blocks you need and adapt them — don't invent syntax. (Resource equivalents: "${EXAMPLES_INDEX_URI}" and "${EXAMPLES_INDEX_URI}/<slug>".)
3. Draft the definition and call \`validate_definition\` to check it against the runtime schema BEFORE saving; fix the descriptive \`errors\` it returns and re-validate until clean. Then call \`create_integration\` (new) or \`update_integration\` (existing).
4. Call \`can_start_integration\` — a best-effort pre-flight on the saved integration. Fix what it reports under \`errors\`, but treat it (and \`validate_definition\`) as advisory: the runtime is the final judge, so a definition it flags may still run (and a clean one may still fail at load).
5. Before running, call \`list_env_keys\` to see the env vars the integration expects (\`{ name, default, required, source }\`). For every \`required\` var (and any var the run needs) that has no value, DON'T just run and let it fail at load — either ASK THE USER for the value and pass it via the \`env\` argument to \`invoke_flow\`/\`run_integration\`, or persist it as an \`env\` resource with \`create_resource\` (kind \"env\", e.g. \`.env.dev\`). Values passed via \`env\` are per-run; values stored as a resource persist with the integration. A var with a \`default\` needs no value unless you want to override it.
6. Test a single flow fast with \`invoke_flow\` (\`flow\` + optional \`data\`/\`env\`): it runs one flow WITHOUT starting sources and returns its result \`output\` and \`logs\` in one call — the quickest author→test→iterate loop. It accepts a saved \`id\` or an inline \`definition\` (so you can try a draft before saving). Use \`list_flows\` to see the flow names in an integration.
7. Call \`run_integration\` for a full run. If the integration declares an HTTP_PORT (a networked \`http\` connector), the result includes a \`testUrl\` you can curl to exercise its endpoints; otherwise it runs internally (e.g. cron-driven). Read \`get_run_logs\` to see the runtime's own load errors, iterate with \`update_integration\`, and \`stop_integration\` when done.

For DESIGN guidance (how to structure flows, when to use queues vs. topics, simplifying transforms, error handling), read the "write-effective-integrations" prompt.

Tips:
- To make an integration testable over HTTP, add an \`http\` connector and declare HTTP_PORT in \`env\`; use that connector as a flow's source.
- Keep \`service.name\` stable; renaming may change the integration's id.
- CEL expressions (e.g. log messages, payloads) can read body, vars, eventID, and correlationID. Call \`getCelFunctions\` for the exact variables and custom functions (toJson/fromJson, toFormData/fromFormData, templateResource) in scope.${docs}`;
}

function effectiveIntegrationsGuide(focus?: string, docsUrl?: string): string {
  const emphasis = focus?.trim()
    ? `Pay special attention to: ${focus.trim()}.\n\n`
    : "";
  const docs = docsUrl?.trim()
    ? `\n\nReference documentation: ${docsUrl.trim()} — the full block & connector reference, CEL syntax, and the processing-pipeline model (workers/buffer/pool). Consult it for exact field names.`
    : "";
  return `Best practices for designing effective Octo integrations. These are DESIGN principles — for exact block/connector syntax call \`getSchema\`/\`getSchema(elementName)\` and adapt the worked examples via \`getExamples\`/\`getExamples(slug)\` (or, on resource-capable clients, the "${RUNTIME_SCHEMA_URI}" and "${EXAMPLES_INDEX_URI}/<slug>" resources) rather than inventing fields. Validate drafts with \`validate_definition\` and test single flows fast with \`invoke_flow\` as you go.

${emphasis}Principles:

1. Split functionality across small, single-purpose flows. Give each flow one clear job. Extract logic shared by several flows into a sourceless flow (a flow with no \`source\`, callable by name) and invoke it with a \`flow-ref\` block — two-way by default (you get the called flow's result back), or set \`oneWay: true\` for fire-and-forget. This keeps flows readable and reusable instead of one sprawling chain.

2. Choose queues vs. topics by delivery semantics.
   - QUEUE (competing consumers): a \`queue-dispatch\` block sends to a subject; a flow with a \`queue\` source consumes it, and each message is handled by exactly ONE replica. Use for load-balancing/decoupling work across replicas, or offloading slow work from a request path. Set \`awaitReply: true\` for request/reply, omit it for fire-and-forget. See the queue-load-balance example.
   - TOPIC (broadcast pub/sub): a \`publish-event\` block broadcasts to a subject; EVERY flow with an \`events\` source on that subject receives the message. Use for fan-out — notifying multiple independent handlers of the same event. See the events example.
   Rule of thumb: one worker should handle it → queue; many interested parties should all see it → topic.

3. Simplify transformation chains with \`multi-transform\`. Instead of stringing together several \`set-payload\`/\`set-variable\` blocks, use one \`multi-transform\` with an ordered \`transforms\` list (\`setBody: <CEL>\` to reshape the body, or \`setVar: <name>\` + \`value: <CEL>\` to stash a variable). Later steps see the results of earlier ones, so a whole compute-then-reshape sequence lives in a single readable block. See the multi-transform example.

4. Handle errors deliberately, at the right scope.
   - Inline boundary: wrap a risky chain in a \`handle-errors\` block — its \`process\` is the protected chain and its \`error\` chain runs on failure with \`vars.error\` ({ message, flow, block }) available to build a fallback/degraded response.
   - Whole-flow fallback: add a flow-level \`error:\` chain (a sibling of \`process\` on a root flow) to recover from any failure in the flow; a successful recovery becomes the flow's result.
   Don't leave failures implicit — decide per risky step whether to recover locally or fall through. See the error-handling example.

5. Use composite blocks for control flow instead of inflating a flow. \`if\`/\`switch\` for routing on a CEL condition, \`foreach\` to iterate an array (binds each element, default \`item\`), \`fork\` to run branches in parallel (each on its own clone; joins before returning), and \`enrich\` to compute derived data on an isolated clone without polluting the main message. Reach for these before duplicating logic or building deeply nested flows.

6. Tune concurrency only when a real need appears. Root flows expose \`workers\` (per-flow worker pool, default 8; set 1 for strict FIFO/no cross-message parallelism), \`buffer\` (source→worker queue depth, default 64; raise to absorb bursts), and \`pool\` (shared pool for fan-out composites like fork, default 8; size it to your widest fan-out to avoid exhaustion). Start with defaults and change a knob only when ordering or throughput demands it.

7. Show the design back to the user as a Mermaid diagram. As you design (and again once it settles), render the flow(s) as a Mermaid \`flowchart\` so the user can eyeball the topology before running it: the source as the entry node, the \`process\` blocks in order, branches for composites (\`if\`/\`switch\`/\`foreach\`/\`fork\`), \`flow-ref\` calls as edges into the referenced flow, and queue/topic hops as edges to/from the subject. Template to adapt:

\`\`\`mermaid
flowchart TD
  src["cron: every 30s"] --> greet["log: greet"]
  greet --> route{"if: body.vip"}
  route -->|true| vip["flow-ref: notify-vip"]
  route -->|false| std["queue-dispatch: work"]
  std -.->|queue: work| worker["flow: worker"]
\`\`\`

Then verify a flow end to end with \`invoke_flow\` and iterate. To check a single CEL expression in isolation before wiring it into a block (a transform, an \`if\`/\`switch\` condition, a connector field), use \`evaluate_cel\`: it evaluates the expression against a sample object (\`data\`→\`body\`, plus optional \`vars\`/\`env\`) and returns the result or the compile/eval error.${docs}`;
}
