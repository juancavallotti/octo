import { createOctoMcpHandler, OCTO_MCP_VERSION } from "@octo/mcp";
import { probeSchema } from "@octo/run-host";
import {
  CAPABILITIES,
  fromDefinitionYaml,
  validateDocument,
} from "@octo/editor/runtime";
import { fsResourceProvider } from "../run/resources";
import { fsIntegrationStore, fsResourceStore } from "./store-adapter";

/**
 * GET/POST/DELETE /mcp — the standalone app's Model Context Protocol endpoint
 * (streamable HTTP). It's barebones and unauthenticated, like the rest of the
 * standalone app (local-only); the platform will mount the same handler behind an
 * API key. Integrations come from the local disk store, definitions are validated
 * with the editor's pre-flight, and the runtime catalogue is served as a resource.
 */

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** Validate a stored definition with the editor's pre-flight (best-effort). */
function validate(definition: string): { valid: boolean; errors: string[] } {
  try {
    const { ok, issues } = validateDocument(fromDefinitionYaml(definition));
    return { valid: ok, errors: issues };
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
    // when no runner is configured.
    runtimeSchema: async () => (await probeSchema()) ?? CAPABILITIES,
    // Stage a run's resources from the flat flows dir; shared across integrations,
    // so the id is ignored (a run with an inline definition still gets them).
    resources: () => fsResourceProvider,
    // Full CRUD over resources on the flat local-disk store (shared across flows,
    // so the integration id is echoed but not used to locate files).
    resourceStore: fsResourceStore,
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
