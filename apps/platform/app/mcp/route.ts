import { withMcpAuth } from "mcp-handler";
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
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import {
  orchestratorIntegrationStore,
  orchestratorMetaStore,
  orchestratorResourceStore,
  orchestratorSuiteStore,
} from "./store-adapter";
import { devRunMcpHost } from "./run-host";
import { verifyMcpToken } from "./verify-token";
import { MCP_ORIGIN, RESOURCE_METADATA_PATH } from "./oauth-config";

/**
 * GET/POST/DELETE /mcp — the platform's Model Context Protocol endpoint
 * (streamable HTTP). It mounts the same `@octo/mcp` handler the standalone app
 * does, but as an OAuth 2.1 *resource server*: every request must carry a valid
 * bearer access token. Tokens are OAuth JWTs minted by the configured OIDC provider
 * (the authorization server) for MCP clients like Claude and ChatGPT, or a legacy
 * `octo_…` API key (see verify-token.ts). `withMcpAuth` extracts the bearer, runs
 * the verifier, and — on a missing/invalid token — returns a spec-correct 401 whose
 * `WWW-Authenticate` points at the protected-resource metadata (RFC 9728), which
 * in turn advertises the provider so the client can register and obtain a token. The
 * OIDC proxy skips `/mcp` (see proxy.ts) precisely because this route owns its
 * own authentication.
 *
 * Integrations come from the orchestrator; definitions are validated with the
 * editor's pre-flight; run control goes to dev-run pods with one-shots staying local
 * (see ./run-host).
 *
 * The verified token is no longer only an authentication boundary: the user id it
 * resolves onto `AuthInfo.extra` is half of a dev run's identity, so a long-running
 * app started here belongs to the caller and is the same run the editor would attach
 * to. Integrations themselves are still not partitioned per user.
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
    store: orchestratorIntegrationStore,
    validate,
    // The runtime generates the capability schema (`octo schema`, probed and
    // cached by @octo/run-host — the same in-process runner the run tools use);
    // serve that so MCP reflects exactly what the binary supports. Falls back to
    // the editor's empty bundled schema when no runner is configured. Same probe
    // `validate` primes itself from.
    runtimeSchema: async () => (await primeCapabilities()) ?? CAPABILITIES,
    // Stage a run's resources from the orchestrator; an inline definition (no id)
    // has none, so a run without a saved integration gets no resources.
    resources: (id) => (id ? orchestratorResourceProvider(id) : undefined),
    // Full CRUD over an integration's stored resources (env files, templates).
    resourceStore: orchestratorResourceStore,
    // The editor's own bookkeeping, so an agent that has just debugged a flow can leave
    // the mocks, spies and test inputs behind for whoever opens the canvas next.
    metaStore: orchestratorMetaStore,
    // The integration's dolphin suites, so an agent can write a test and then run it —
    // the same files the Testing tab shows and CI runs.
    suiteStore: orchestratorSuiteStore,
    // No baseUrl: a dev run's address is a hostname of its own, so there is no origin to
    // resolve it against. Only a host that proxies to a child of itself needs one.
    // Point the authoring prompt at the human docs when configured.
    docsUrl: process.env.OCTO_DOCS_URL,
  },
  {
    // Dev-run pods for the long-running app, local children for the one-shots.
    runHost: devRunMcpHost,
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
