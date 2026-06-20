import type { RuntimeEnv } from "./serializeEnv";

/**
 * The runtime config shape (the YAML/JSON the runtime loads — see
 * runtime/types/flow.go). These are the wire types serialize.ts maps the editor
 * document to and from.
 */

export interface RuntimeFlow {
  name?: string;
  source?: RuntimeSource;
  process?: RuntimeBlock[];
}

export interface RuntimeCase extends RuntimeFlow {
  when?: string;
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
  // handle-errors slots (bare block chains).
  process?: RuntimeBlock[];
  error?: RuntimeBlock[];
  // Composite scalars.
  condition?: unknown;
  items?: unknown;
  as?: unknown;
}

export interface RuntimeConnector {
  name?: string;
  type?: string;
  settings?: Record<string, unknown>;
}

export interface RuntimeConfig {
  env?: RuntimeEnv[];
  connectors?: RuntimeConnector[];
  flows?: RuntimeFlow[];
}
