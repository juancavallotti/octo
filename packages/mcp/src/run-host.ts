/**
 * The slice of `@octo/run-host` the run-control tools depend on, expressed as a
 * structural port. The real package satisfies it directly (`import * as runHost`),
 * while tests pass a stub so they never spawn a real `octo` process. Runs are keyed
 * by a namespace slug; the handler resolves one per MCP session.
 */

import type { ResourceProvider } from "@octo/run-host";

/** A run's status snapshot — the fields run-host's `RunStatus` exposes. */
export interface RunStatusLike {
  /** Whether a runner binary is configured (OCTO_BIN_PATH set). */
  available: boolean;
  running: boolean;
  version: string | null;
  /** Whether the current run declares HTTP_PORT, i.e. is networked/testable. */
  exposable: boolean;
  port: number | null;
  /** BFF path proxying to the running networked integration, or null. */
  testPath: string | null;
}

/** One buffered log line from a runner. */
export interface RunLogLine {
  seq: number;
  text: string;
}

/** The outcome of a one-shot `invoke`: the flow's stdout result and its stderr logs. */
export interface InvokeResultLike {
  ok: boolean;
  exitCode: number | null;
  timedOut: boolean;
  /** True when the flow filtered the message, so there is no result body. */
  dropped: boolean;
  output: string;
  logs: string[];
}

export interface RunHostPort {
  status(ns: string): RunStatusLike;
  start(
    ns: string,
    yaml: string,
    env?: Record<string, string>,
    opts?: { resources?: ResourceProvider },
  ): Promise<RunStatusLike>;
  stop(ns: string): Promise<RunStatusLike>;
  /** Run a single named flow once (sources not started) and return its result + logs. */
  invoke(
    ns: string,
    yaml: string,
    flow: string,
    opts?: {
      data?: string;
      env?: Record<string, string>;
      timeoutMs?: number;
      resources?: ResourceProvider;
    },
  ): Promise<InvokeResultLike>;
  snapshot(ns: string): RunLogLine[];
  /** Mint a fresh, valid namespace slug. */
  newNamespace(): string;
}
