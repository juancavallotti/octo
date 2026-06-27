import { createMcpHandler } from "mcp-handler";
import * as defaultRunHost from "@octo/run-host";
import type { OctoMcpConfig } from "./backend";
import type { RunHostPort } from "./run-host";
import { createNamespaceResolver } from "./namespace";
import { registerIntegrationTools } from "./tools/integration";
import { registerRunTools } from "./tools/run";
import {
  registerExampleResources,
  registerRuntimeSchemaResource,
} from "./resource";
import { registerPrompts } from "./prompts";

/** Knobs for the MCP route handler beyond the backend {@link OctoMcpConfig}. */
export interface OctoMcpHandlerOptions {
  /**
   * Base path mcp-handler derives its endpoints from. The streamable HTTP
   * endpoint is `${basePath}/mcp`, so a route at `app/api/mcp/route.ts` needs
   * `basePath: "/api"`, while one at `app/mcp/[transport]/route.ts` needs `"/"`.
   * @default ""
   */
  basePath?: string;
  /**
   * The run host driving the run-control tools. Defaults to `@octo/run-host`;
   * injectable so tests (and alternative hosts) can supply their own.
   */
  runHost?: RunHostPort;
  /** Enable mcp-handler console logging. @default false */
  verboseLogs?: boolean;
  /** Max request duration in seconds. @default 60 */
  maxDuration?: number;
}

/**
 * Build the Next route handler exposing the Octo integration MCP server. The same
 * factory serves both hosts — each supplies its own {@link OctoMcpConfig} (store,
 * validator, runtime schema); the namespace resolver is created once here so a run
 * stays bound to its MCP session across requests. SSE is disabled (the spec
 * deprecates it); only the streamable HTTP transport is served.
 */
export function createOctoMcpHandler(
  config: OctoMcpConfig,
  options: OctoMcpHandlerOptions = {},
): (request: Request) => Promise<Response> {
  const runHost = options.runHost ?? defaultRunHost;
  const resolveNamespace = createNamespaceResolver(runHost);

  return createMcpHandler(
    (server) => {
      registerIntegrationTools(server, config);
      registerRunTools(server, config, runHost, resolveNamespace);
      registerRuntimeSchemaResource(server, config);
      registerExampleResources(server);
      registerPrompts(server);
    },
    {
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
