import { createMcpHandler } from "mcp-handler";
import type { Implementation } from "@modelcontextprotocol/sdk/types.js";
import type { OctoMcpConfig } from "./backend";
import type { RunHostPort } from "./run-host";
import { createNamespaceResolver } from "./namespace";
import { registerIntegrationTools } from "./tools/integration";
import { registerResourceTools } from "./tools/resource";
import { registerMetaTools } from "./tools/meta";
import { registerSuiteTools } from "./tools/suite";
import { registerRunTools } from "./tools/run";
import { registerDocsTools } from "./tools/docs";
import {
  registerExampleResources,
  registerRuntimeSchemaResource,
} from "./resource";
import { registerPrompts } from "./prompts";
import { OCTO_ICON_DATA_URI } from "./icon";

/**
 * The Octo MCP server version reported to clients in the `initialize` handshake.
 * release-please keeps this in sync with the published release; the trailing
 * annotation marks the line it rewrites (this file is listed under `extra-files`
 * in release-please-config.json). Hosts share it so every deployment reports the
 * same octo release — override per host via {@link OctoMcpServerInfo.version}.
 */
export const OCTO_MCP_VERSION = "0.8.7"; // x-release-please-version

/**
 * Identity reported to clients during the MCP `initialize` handshake. Without a
 * `serverInfo`, `mcp-handler` falls back to its own "mcp-typescript server on
 * vercel" default, so every host must supply one.
 */
export interface OctoMcpServerInfo {
  /** Machine name clients key the connection by, e.g. `octo-platform`. */
  name: string;
  /** Server version, surfaced to clients. @default {@link OCTO_MCP_VERSION} */
  version: string;
  /** Human-facing display name, e.g. `Octo`. */
  title?: string;
  /**
   * Visual identifiers for the server, per the MCP icons extension. Defaults to
   * Octo's logo so clients render it directly instead of falling back to
   * scraping the host app's favicon; a host only needs to set this to brand a
   * deployment differently.
   */
  icons?: Implementation["icons"];
}

/** Default identity when a host doesn't override {@link OctoMcpHandlerOptions.serverInfo}. */
const DEFAULT_SERVER_INFO: OctoMcpServerInfo = {
  name: "octo",
  version: OCTO_MCP_VERSION,
  title: "Octo",
  icons: [{ src: OCTO_ICON_DATA_URI, mimeType: "image/png", sizes: ["256x256"] }],
};

/** Knobs for the MCP route handler beyond the backend {@link OctoMcpConfig}. */
export interface OctoMcpHandlerOptions {
  /**
   * The run host driving the run-control tools. Required, with no fallback: the two hosts
   * run an app in different ways, so defaulting to either would silently give the other
   * the wrong runner. See {@link RunHostPort}.
   */
  runHost: RunHostPort;
  /**
   * Base path mcp-handler derives its endpoints from. The streamable HTTP
   * endpoint is `${basePath}/mcp`, so a route at `app/api/mcp/route.ts` needs
   * `basePath: "/api"`, while one at `app/mcp/[transport]/route.ts` needs `"/"`.
   * @default ""
   */
  basePath?: string;
  /** Enable mcp-handler console logging. @default false */
  verboseLogs?: boolean;
  /** Max request duration in seconds. @default 60 */
  maxDuration?: number;
  /**
   * Identity reported during the MCP `initialize` handshake. Falls back to the
   * generic {@link DEFAULT_SERVER_INFO}; hosts should pass their own so clients
   * can tell deployments apart (e.g. `octo-platform` vs `octo-standalone`).
   */
  serverInfo?: Partial<OctoMcpServerInfo>;
}

/**
 * Build the Next route handler exposing the Octo integration MCP server. The same
 * factory serves both hosts — each supplies its own {@link OctoMcpConfig} (store,
 * validator, runtime schema) and its own {@link RunHostPort}; the namespace resolver is
 * created once here so a run stays bound to its MCP session across requests. SSE is
 * disabled (the spec deprecates it); only the streamable HTTP transport is served.
 */
export function createOctoMcpHandler(
  config: OctoMcpConfig,
  options: OctoMcpHandlerOptions,
): (request: Request) => Promise<Response> {
  const { runHost } = options;
  const resolveNamespace = createNamespaceResolver(runHost);

  return createMcpHandler(
    (server) => {
      registerIntegrationTools(server, config);
      registerResourceTools(server, config);
      registerMetaTools(server, config);
      registerRunTools(server, config, runHost, resolveNamespace);
      registerSuiteTools(server, config, runHost, resolveNamespace);
      registerDocsTools(server, config);
      registerRuntimeSchemaResource(server, config);
      registerExampleResources(server);
      registerPrompts(server, config);
    },
    {
      serverInfo: { ...DEFAULT_SERVER_INFO, ...options.serverInfo },
      capabilities: {
        tools: {},
        resources: {},
        prompts: {},
      },
    },
    {
      basePath: options.basePath ?? "",
      verboseLogs: options.verboseLogs ?? false,
      maxDuration: options.maxDuration ?? 60,
      disableSse: true,
    },
  );
}
