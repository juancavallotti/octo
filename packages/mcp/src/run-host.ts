/**
 * What the run-control tools need from their host, expressed as a port the host supplies.
 *
 * There is no default. Two hosts mount this server and they run an app in genuinely
 * different ways — one as a child process of itself, one as a pod elsewhere — so a port
 * that quietly fell back to one of them would give the other the wrong runner and no
 * error. Each host declares its own (`app/mcp/run-host.ts` in both apps); tests pass a
 * stub, and never spawn a real `octo` process.
 *
 * The long-running half is addressed by a {@link RunKey} — run-host's own, not a copy of
 * it, because it is the same key both hosts' runners already consume. It carries a
 * namespace (resolved once per MCP session, so a session's run stays its own), the
 * integration, and the calling user; each host reads the halves it keys on. The one-shots
 * still take a bare namespace, because they are local on both hosts and it only names
 * where their files are staged.
 *
 * The asynchronous signatures are the other half of that seam: they are what lets a host
 * answer over the network rather than out of its own heap.
 */

import type {
  MockSpec,
  ResourceProvider,
  RunKey,
  SpyTrace,
  TestRunArgs,
  TestRunOutcome,
} from "@octo/run-host";

export type { RunKey } from "@octo/run-host";

/**
 * What the host can spawn — the fields run-host's `Binaries` exposes.
 *
 * Separate from {@link RunStateLike} because they answer to different owners: a binary is
 * installed on the host, while a run belongs to whichever backend is running it. On a host
 * whose app runs elsewhere the two come apart, and `invoke_flow` still works when
 * `run_integration` cannot.
 */
export interface BinariesLike {
  /** Whether a runner binary is configured (OCTO_BIN_PATH set). */
  available: boolean;
  version: string | null;
}

/** The state of the app a backend is running — the fields run-host's `RunState` exposes. */
export interface RunStateLike {
  running: boolean;
  /** Whether the current run declares HTTP_PORT, i.e. is networked/testable. */
  exposable: boolean;
  port: number | null;
  /**
   * Where to reach the running networked integration, or null when it serves no HTTP.
   * App-relative when the host proxies to its own child process, absolute when the run
   * has a hostname of its own — {@link buildTestUrl} handles both.
   */
  testUrl: string | null;
}

/** One buffered log line from a runner. */
export interface RunLogLine {
  seq: number;
  text: string;
}

/** The envelope a `breakAt` invoke returns: the message at the block, or why not. */
export interface BreakOutcomeLike {
  /** False when the flow never took the branch the block is on — a normal result. */
  reached: boolean;
  block: string;
  /** The message as it looked after the addressed block ran. */
  message?: unknown;
  /** The flow's own failure, reported in-band. */
  error?: string;
}

/** The outcome of a one-shot `invoke`: the flow's stdout result and its stderr logs. */
export interface InvokeResultLike {
  ok: boolean;
  exitCode: number | null;
  timedOut: boolean;
  /** True when the flow filtered the message, so there is no result at all. */
  dropped: boolean;
  /** The flow's result message as JSON text: `{event_id, variables, body}`. */
  output: string;
  logs: string[];
  /** Present only for a `breakAt` invoke: what the flow was carrying at that block. */
  breakpoint?: BreakOutcomeLike;
  /** Present only for a `spies` invoke: every message that crossed each spied block. */
  spies?: SpyTrace[];
}

/** The outcome of a one-shot CEL `evalCel`: the evaluated result or the compile/eval error. */
export interface EvalResultLike {
  /** True when a well-formed envelope came back (whatever the CEL outcome). */
  ok: boolean;
  /** The evaluated value, when the expression succeeded (may itself be false/0/null). */
  result?: unknown;
  /** The compile/eval error message, when the expression (or the runner) failed. */
  error?: string;
  logs: string[];
}

export interface RunHostPort {
  /** Which binaries this host has for its one-shot runs, and their versions. */
  binaries(): BinariesLike;
  /**
   * Start (or restart) the integration as a long-running app.
   *
   * `yaml` and `env` are meaningful only to a host that PUSHES config to its runner. One
   * whose runner pulls the stored definition runs what was saved, and has nowhere to put
   * an ad-hoc `env` — so it refuses one rather than dropping it silently, which would
   * leave an agent debugging a run that never saw the value it set.
   */
  start(
    key: RunKey,
    yaml: string,
    env?: Record<string, string>,
    opts?: { resources?: ResourceProvider },
  ): Promise<RunStateLike>;
  stop(key: RunKey): Promise<RunStateLike>;
  /** Run a single named flow once (sources not started) and return its result + logs. */
  invoke(
    ns: string,
    yaml: string,
    flow: string,
    opts?: {
      data?: string;
      vars?: string;
      env?: Record<string, string>;
      timeoutMs?: number;
      resources?: ResourceProvider;
      /** Run until this block, then stop and report the message it produced. */
      breakAt?: string;
      /** Block addresses to record every message that crosses them. */
      spies?: string[];
      /** Block addresses to stand in for, so the real block never runs. */
      mocks?: Record<string, MockSpec>;
      logLevel?: "debug" | "info" | "warn" | "error";
    },
  ): Promise<InvokeResultLike>;
  /** Evaluate a CEL expression against an ad-hoc object (no flow run, no namespace). */
  evalCel(
    expression: string,
    opts?: {
      data?: string;
      vars?: string;
      env?: Record<string, string>;
      timeoutMs?: number;
    },
  ): Promise<EvalResultLike>;
  /**
   * Run dolphin over one or more test suites against a config, and report what happened.
   *
   * The args and outcome are run-host's own rather than a structural copy: the report is
   * a dozen nested shapes, and a second declaration of them would drift from the one
   * dolphin actually writes — which is the contract this whole seam exists to preserve.
   */
  test(ns: string, args: TestRunArgs): Promise<TestRunOutcome>;
  /**
   * The run's log history, oldest line first — what `get_run_logs` reads.
   *
   * A bounded snapshot rather than a follow: an agent reads logs as a document, and a
   * stream it had to decide when to stop draining would be worse than one it cannot get.
   */
  snapshot(key: RunKey): Promise<RunLogLine[]>;
  /** Mint a fresh, valid namespace slug. */
  newNamespace(): string;
}
