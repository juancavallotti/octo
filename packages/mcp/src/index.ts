/**
 * @octo/mcp — a reusable Model Context Protocol server exposing Octo integration
 * authoring and run control. A host mounts it by supplying an {@link OctoMcpConfig}:
 * its integration store, a validator, and the runtime schema. This package owns the
 * tools, the runtime-schema resource, the authoring prompts, and the per-session run
 * host wiring. Node-only — never import from a browser bundle.
 */

export type {
  IntegrationRecord,
  IntegrationStore,
  MetaStore,
  OctoMcpConfig,
  ResourceRecord,
  ResourceStore,
  SuiteStore,
  ValidationOutcome,
} from "./backend";
export type {
  RunHostPort,
  BinariesLike,
  RunStateLike,
  RunLogLine,
} from "./run-host";
export { createOctoMcpHandler, OCTO_MCP_VERSION } from "./handler";
export type { OctoMcpHandlerOptions, OctoMcpServerInfo } from "./handler";
