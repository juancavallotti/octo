import { createOctoMcpHandler, OCTO_MCP_VERSION } from "@octo/mcp";
import {
  CAPABILITIES,
  fromDefinitionYaml,
  validateDocument,
} from "@octo/editor/runtime";
import { verifyApiKey } from "@/app/actions/_client";
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import {
  orchestratorIntegrationStore,
  orchestratorResourceStore,
} from "./store-adapter";

/**
 * GET/POST/DELETE /mcp — the platform's Model Context Protocol endpoint
 * (streamable HTTP). It mounts the same `@octo/mcp` handler the standalone app
 * does, but behind a per-user API key: every request must carry a valid bearer
 * token (Authorization: Bearer <token>), verified against the orchestrator before
 * the request reaches the MCP layer. The OIDC proxy is told to skip `/mcp` (see
 * proxy.ts) precisely because this route owns its own authentication.
 *
 * Integrations come from the orchestrator; definitions are validated with the
 * editor's pre-flight; run-control tools use the in-process `@octo/run-host` (the
 * handler's default — the same runner the editor's RUN feature uses).
 *
 * The verified token is an authentication boundary only: runs stay keyed by MCP
 * session id and integrations are not yet partitioned per user.
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
    runtimeSchema: CAPABILITIES,
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

/** 401 with a Bearer challenge, the response for any missing/invalid key. */
function unauthorized(): Response {
  return new Response(JSON.stringify({ error: "unauthorized" }), {
    status: 401,
    headers: {
      "WWW-Authenticate": "Bearer",
      "Content-Type": "application/json",
    },
  });
}

/** Authenticate the bearer API key, then delegate to the MCP handler. */
async function authed(request: Request): Promise<Response> {
  const header = request.headers.get("authorization") ?? "";
  const token = header.startsWith("Bearer ") ? header.slice(7).trim() : "";
  if (!token) return unauthorized();
  const res = await verifyApiKey(token);
  if (!res.ok) return unauthorized();
  return handler(request);
}

export { authed as GET, authed as POST, authed as DELETE };
