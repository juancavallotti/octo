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
