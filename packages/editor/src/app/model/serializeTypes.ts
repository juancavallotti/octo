import type { RuntimeEnv } from "./serializeEnv";
import type { RuntimeResources } from "./serializeResources";

/**
 * The runtime config shape (the YAML/JSON the runtime loads — see
 * runtime/types/flow.go). These are the wire types serialize.ts maps the editor
 * document to and from.
 */

export interface RuntimeFlow {
  name?: string;
  source?: RuntimeSource;
  process?: RuntimeBlock[];
  /** The flow-level error path: a bare block chain (root flows only). */
  error?: RuntimeBlock[];
  /** Root-flow concurrency tuning (worker pool / inbound buffer / shared pool). */
  workers?: number;
  buffer?: number;
  pool?: number;
}

export interface RuntimeCase extends RuntimeFlow {
  when?: string;
}

/** One ai-router route: a named, described inline flow. */
export interface RuntimeRoute extends RuntimeFlow {
  description?: string;
}

/** One ai-agent tool: a named, described, schema-bearing inline flow. */
export interface RuntimeTool extends RuntimeFlow {
  description?: string;
  inputSchema?: string;
  // mcp-router-only metadata. The editor round-trips all three so opening and
  // saving an MCP server's flow cannot silently strip them; only outputSchema
  // has a field in the UI, because annotations are four tri-state hints that
  // read better in YAML than as a row of checkboxes.
  title?: string;
  annotations?: Record<string, boolean>;
  outputSchema?: string;
}

/**
 * One ai-agent skill: a named, described binding to a template resource. Unlike a
 * tool it holds no sub-flow, so it serializes as plain data (round-tripped via the
 * block's settings, not a slot).
 */
export interface RuntimeSkill {
  name?: string;
  description?: string;
  resource?: string;
}

/** One mcp-router resource: a template resource advertised as an MCP resource. */
export interface RuntimeMCPResource {
  uri?: string;
  name?: string;
  description?: string;
  mimeType?: string;
  resource?: string;
}

/** One declared argument of an mcp-router prompt. */
export interface RuntimeMCPPromptArg {
  name?: string;
  description?: string;
  required?: boolean;
}

/** One mcp-router prompt: a template resource advertised as an MCP prompt. */
export interface RuntimeMCPPrompt {
  name?: string;
  description?: string;
  arguments?: RuntimeMCPPromptArg[];
  resource?: string;
}

export interface RuntimeSource {
  connector?: string;
  type?: string;
  settings?: Record<string, unknown>;
}

export interface RuntimeBlock {
  type?: string;
  name?: string;
  settings?: Record<string, unknown>;
  // Composite slots (nested flows).
  branches?: RuntimeFlow[];
  then?: RuntimeFlow;
  else?: RuntimeFlow;
  cases?: RuntimeCase[];
  default?: RuntimeFlow;
  body?: RuntimeFlow;
  // ai-router / ai-agent slots (named, described inline flows).
  routes?: RuntimeRoute[];
  tools?: RuntimeTool[];
  // ai-agent skills: plain data (name, description, resource ref), not a slot.
  skills?: RuntimeSkill[];
  // mcp-router resources/prompts: plain data (resource refs), not slots.
  serverName?: unknown;
  resources?: RuntimeMCPResource[];
  prompts?: RuntimeMCPPrompt[];
  // handle-errors / ai-retry slots (bare block chains).
  process?: RuntimeBlock[];
  error?: RuntimeBlock[];
  // Composite scalars.
  condition?: unknown;
  items?: unknown;
  as?: unknown;
  // AI composite scalars.
  connector?: unknown;
  prompt?: unknown;
  guardrail?: unknown;
  maxIterations?: unknown;
  maxAttempts?: unknown;
}

export interface RuntimeConnector {
  name?: string;
  type?: string;
  settings?: Record<string, unknown>;
}

export interface RuntimeConfig {
  env?: RuntimeEnv[];
  resources?: RuntimeResources;
  connectors?: RuntimeConnector[];
  flows?: RuntimeFlow[];
}
