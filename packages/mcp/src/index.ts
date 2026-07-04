/**
 * @octo/mcp — a reusable Model Context Protocol server exposing Octo integration
 * authoring and run control. Hosts (the platform and the standalone app) mount it
 * at `/mcp` by supplying an {@link OctoMcpConfig} (their integration store, a
 * validator, and the runtime schema); this package owns the tools, the
 * runtime-schema resource, the authoring prompts, and the per-session run host
 * wiring. Node-only — never import from a browser bundle.
 */

export type {
  IntegrationRecord,
  IntegrationStore,
  OctoMcpConfig,
  ResourceRecord,
  ResourceStore,
  ValidationOutcome,
} from "./backend";
export type { RunHostPort, RunStatusLike, RunLogLine } from "./run-host";
export { createOctoMcpHandler } from "./handler";
export type { OctoMcpHandlerOptions } from "./handler";
