import { createOctoMcpHandler, OCTO_MCP_VERSION } from "@octo/mcp";
import { probeSchema } from "@octo/run-host";
import {
  CAPABILITIES,
  fromDefinitionYaml,
  issueMessages,
  setCapabilities,
  validateDocument,
  type Capabilities,
} from "@octo/editor/runtime";
import { fsResourceProvider } from "../run/resources";
import {
  fsIntegrationStore,
  fsMetaStore,
  fsResourceStore,
  fsSuiteStore,
} from "./store-adapter";

/**
 * GET/POST/DELETE /mcp — the standalone app's Model Context Protocol endpoint
 * (streamable HTTP). It's barebones and unauthenticated, like the rest of the
 * standalone app (local-only); the platform will mount the same handler behind an
 * API key. Integrations come from the local disk store, definitions are validated
 * with the editor's pre-flight, and the runtime catalogue is served as a resource.
 */

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/**
 * Inject the runner's capability catalogue into the editor's schema registry.
 *
 * This route is a host of `@octo/editor` just as the editor page is, and it owes the
 * same injection: `validateDocument` checks block and connector types against the
 * *active* catalogue, and the bundled one is an empty fallback. Skip this and every
 * validation reports "unknown block type" for perfectly good YAML — which would make
 * the flow tools refuse every edit, since they validate before they save.
 *
 * Cheap to call on every request: `probeSchema` caches the parsed schema, and
 * `setCapabilities` is an idempotent assignment. Deliberately not memoized here — a
 * probe that failed because the binary was still building must be free to succeed
 * later.
 */
async function primeCapabilities(): Promise<unknown> {
  const schema = await probeSchema();
  setCapabilities(schema as Capabilities | null);
  return schema;
}

/** Validate a stored definition with the editor's pre-flight (best-effort). */
async function validate(
  definition: string,
): Promise<{ valid: boolean; errors: string[] }> {
  await primeCapabilities();
  try {
    const result = validateDocument(fromDefinitionYaml(definition));
    return { valid: result.ok, errors: issueMessages(result) };
  } catch (err) {
    return { valid: false, errors: [(err as Error).message] };
  }
}

const handler = createOctoMcpHandler(
  {
    store: fsIntegrationStore,
    validate,
    // The runtime generates the capability schema (`octo schema`, probed and
    // cached by @octo/run-host); serve that so MCP reflects exactly what the
    // bundled binary supports. Falls back to the editor's empty bundled schema
    // when no runner is configured. Same probe `validate` primes itself from.
    runtimeSchema: async () => (await primeCapabilities()) ?? CAPABILITIES,
    // Stage a run's resources from the flat flows dir; shared across integrations,
    // so the id is ignored (a run with an inline definition still gets them).
    resources: () => fsResourceProvider,
    // Full CRUD over resources on the flat local-disk store (shared across flows,
    // so the integration id is echoed but not used to locate files).
    resourceStore: fsResourceStore,
    // The editor's own bookkeeping, so an agent that has just debugged a flow can leave
    // the mocks, spies and test inputs behind for whoever opens the canvas next.
    metaStore: fsMetaStore,
    // The dolphin suites beside the flows, so an agent can write a test and then run it —
    // the same files the Testing tab shows and `dolphin test` runs from a terminal.
    suiteStore: fsSuiteStore,
    // Point the authoring prompt at the human docs (CEL, block reference) when
    // configured. Set OCTO_DOCS_URL to your documentation site.
    docsUrl: process.env.OCTO_DOCS_URL,
  },
  {
    basePath: "", // route lives at /mcp, so the streamable endpoint is /mcp
    serverInfo: {
      name: "octo-standalone",
      version: OCTO_MCP_VERSION,
      title: "Octo (Standalone)",
    },
  },
);

export { handler as GET, handler as POST, handler as DELETE };
