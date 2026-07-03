import { createOctoMcpHandler } from "@octo/mcp";
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
    runtimeSchema: CAPABILITIES,
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
  { basePath: "" }, // route lives at /mcp, so the streamable endpoint is /mcp
);

export { handler as GET, handler as POST, handler as DELETE };
