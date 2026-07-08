import { withMcpAuth } from "mcp-handler";
import { createOctoMcpHandler, OCTO_MCP_VERSION } from "@octo/mcp";
import { probeSchema } from "@octo/run-host";
import {
  CAPABILITIES,
  fromDefinitionYaml,
  validateDocument,
} from "@octo/editor/runtime";
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import {
  orchestratorIntegrationStore,
  orchestratorResourceStore,
} from "./store-adapter";
import { verifyMcpToken } from "./verify-token";
import { MCP_ORIGIN, RESOURCE_METADATA_PATH } from "./oauth-config";

/**
 * GET/POST/DELETE /mcp — the platform's Model Context Protocol endpoint
 * (streamable HTTP). It mounts the same `@octo/mcp` handler the standalone app
 * does, but as an OAuth 2.1 *resource server*: every request must carry a valid
 * bearer access token. Tokens are OAuth JWTs minted by eetr (the authorization
 * server) for MCP clients like Claude and ChatGPT, or a legacy `octo_…` API key
 * (see verify-token.ts). `withMcpAuth` extracts the bearer, runs the verifier,
 * and — on a missing/invalid token — returns a spec-correct 401 whose
 * `WWW-Authenticate` points at the protected-resource metadata (RFC 9728), which
 * in turn advertises eetr so the client can register and obtain a token. The
 * OIDC proxy skips `/mcp` (see proxy.ts) precisely because this route owns its
 * own authentication.
 *
 * Integrations come from the orchestrator; definitions are validated with the
 * editor's pre-flight; run-control tools use the in-process `@octo/run-host` (the
 * handler's default — the same runner the editor's RUN feature uses).
 *
 * The verified token is an authentication boundary only: runs stay keyed by MCP
 * session id and integrations are not yet partitioned per user (the resolved
 * user id rides along on `AuthInfo.extra` for when they are).
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
    store: orchestratorIntegrationStore,
    validate,
    // The runtime generates the capability schema (`octo schema`, probed and
    // cached by @octo/run-host — the same in-process runner the run tools use);
    // serve that so MCP reflects exactly what the binary supports. Falls back to
    // the editor's empty bundled schema when no runner is configured.
    runtimeSchema: async () => (await probeSchema()) ?? CAPABILITIES,
    // Stage a run's resources from the orchestrator; an inline definition (no id)
    // has none, so a run without a saved integration gets no resources.
    resources: (id) => (id ? orchestratorResourceProvider(id) : undefined),
    // Full CRUD over an integration's stored resources (env files, templates).
    resourceStore: orchestratorResourceStore,
    // Absolutize a run's test URL when the public origin is known (Auth.js's
    // canonical var); otherwise the bare /editor/runs/<ns>/ path is returned.
    baseUrl: process.env.AUTH_URL,
    // Point the authoring prompt at the human docs when configured.
    docsUrl: process.env.OCTO_DOCS_URL,
  },
  {
    basePath: "", // route lives at /mcp, so the streamable endpoint is /mcp
    serverInfo: {
      name: "octo-platform",
      version: OCTO_MCP_VERSION,
      title: "Octo",
    },
  },
);

/**
 * The MCP handler behind OAuth 2.1 bearer auth. `resourceUrl` is the public
 * origin (Auth.js's canonical var) so the `resource_metadata` challenge URL is
 * correct behind the platform proxy; `resourceMetadataPath` is the path-scoped
 * document the metadata routes serve. `required: true` rejects anonymous calls.
 */
const authed = withMcpAuth(handler, verifyMcpToken, {
  required: true,
  resourceMetadataPath: RESOURCE_METADATA_PATH,
  resourceUrl: MCP_ORIGIN || undefined,
});

export { authed as GET, authed as POST, authed as DELETE };
