import { withMcpAuth } from "mcp-handler";
import { createOctoMcpHandler, OCTO_MCP_VERSION } from "@octo/mcp";
import { probeSchema, validate as validateWithRuntime } from "@octo/run-host";
import {
  CAPABILITIES,
  issueMessages,
  parseProject,
  projectView,
  setCapabilities,
  validateDocument,
  type Capabilities,
  type StoredFile,
} from "@octo/editor/runtime";
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import {
  orchestratorIntegrationStore,
  orchestratorMetaStore,
  orchestratorResourceStore,
  orchestratorSuiteStore,
} from "./store-adapter";
import { devRunMcpHost } from "./run-host";
import { runWithToken } from "@/app/auth/callerToken";
import { verifyMcpToken } from "./verify-token";
import { MCP_ORIGIN, RESOURCE_METADATA_PATH } from "./oauth-config";

/**
 * GET/POST/DELETE /mcp — the platform's Model Context Protocol endpoint
 * (streamable HTTP), mounted as an OAuth 2.1 *resource server*: every request must
 * carry a valid bearer access token, an OAuth JWT minted by the configured OIDC
 * provider (see verify-token.ts). `withMcpAuth`
 * extracts the bearer, runs the verifier, and — on a missing or invalid token —
 * returns a spec-correct 401 whose `WWW-Authenticate` points at the
 * protected-resource metadata (RFC 9728), which in turn advertises the provider so
 * the client can register and obtain a token. This route owns its own
 * authentication, which is why the OIDC proxy skips `/mcp`.
 *
 * Integrations come from the orchestrator; definitions are validated with the
 * editor's pre-flight; run control goes to dev-run pods with one-shots staying local
 * (see ./run-host).
 *
 * The user id the verified token resolves onto `AuthInfo.extra` is half of a dev
 * run's identity, so a long-running app started here belongs to the caller.
 * Integrations themselves are not partitioned per user.
 */

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/**
 * Inject the runner's capability catalogue into the editor's schema registry.
 *
 * `validateDocument` checks block and connector types against the *active*
 * catalogue, and the bundled one is an empty fallback. Skip this and every
 * validation reports "unknown block type" for perfectly good YAML.
 *
 * Cheap to call on every request: `probeSchema` caches the parsed schema, and
 * `setCapabilities` is an idempotent assignment. Not memoized here — a probe that
 * failed because the binary was still building must be free to succeed later.
 */
async function primeCapabilities(): Promise<unknown> {
  const schema = await probeSchema();
  setCapabilities(schema as Capabilities | null);
  return schema;
}

/**
 * Validate an integration's files with the editor's pre-flight over the merged project,
 * then — once that passes, for a saved integration — with `octo validate`, which knows
 * what the editor cannot. A draft is not asked of the runtime: the resources its files
 * declare live with a saved integration, and loading it without them would report every
 * declared template missing.
 */
async function validate(
  files: StoredFile[],
  integrationId?: string,
): Promise<{ valid: boolean; errors: string[] }> {
  await primeCapabilities();
  try {
    const result = validateDocument(projectView(parseProject(files)));
    if (!result.ok) return { valid: false, errors: issueMessages(result) };
  } catch (err) {
    return { valid: false, errors: [(err as Error).message] };
  }
  if (!integrationId) return { valid: true, errors: [] };
  const checked = await validateWithRuntime(MCP_VALIDATE_NS, files, {
    resources: orchestratorResourceProvider(integrationId),
  });
  if (!checked.ok) return { valid: true, errors: [] };
  return {
    valid: checked.valid,
    errors: checked.issues.map((i) => (i.where ? `${i.where}: ${i.message}` : i.message)),
  };
}

/**
 * The run namespace validation stages into. Every check gets a folder of its own
 * beneath it, so concurrent checks never share one; it only has to be a well-formed
 * slug.
 */
const MCP_VALIDATE_NS = "mcpcheck";

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
 * Run everything a tool does as the person who asked, not as this endpoint.
 *
 * There is no session here to read a credential from — the caller arrived with a
 * bearer, which the verifier traded with iam for a platform token — so it is put
 * where the orchestrator client will find it, for the length of this request and
 * no longer.
 *
 * It wraps the handler *inside* withMcpAuth rather than around it, because that is
 * where the credential exists: withMcpAuth verifies the bearer and hangs the result
 * off the request before calling what it wraps.
 */
function asCaller(next: typeof handler) {
  return async (req: Request): Promise<Response> => {
    const token = extraOf(req)?.octoToken;
    if (typeof token !== "string" || token === "") return next(req);
    return runWithToken(token, () => next(req));
  };
}

/** The AuthInfo withMcpAuth attached to the request, read back the way it set it. */
function extraOf(req: Request): Record<string, unknown> | undefined {
  return (req as Request & { auth?: { extra?: Record<string, unknown> } }).auth?.extra;
}

/**
 * The handler behind OAuth 2.1 bearer auth. `resourceUrl` is the public origin so
 * the `resource_metadata` challenge URL is correct behind the platform proxy;
 * `resourceMetadataPath` is the path-scoped document the metadata routes serve.
 * `required: true` rejects anonymous calls.
 */
const authed = withMcpAuth(asCaller(handler), verifyMcpToken, {
  required: true,
  resourceMetadataPath: RESOURCE_METADATA_PATH,
  resourceUrl: MCP_ORIGIN || undefined,
});

export { authed as GET, authed as POST, authed as DELETE };
