/**
 * The RUN capability's transport contract: the small surface the RunProvider
 * needs to drive a runner, decoupled from how it is reached. The editor's
 * provider holds all the client-side policy (debounced sync, log dedupe,
 * validation gating); a transport only moves bytes — so the same provider works
 * whether the runner is reached through a platform BFF or a standalone app's
 * local process. The concrete transports live in the apps that embed the editor.
 */

/** A one-shot CEL evaluation request (no flow run) — the CEL tester's input. */
export interface CelEvalRequest {
  /** The CEL expression to evaluate. */
  expression: string;
  /** Optional JSON object (as a string) bound to `body`. */
  data?: string;
  /** Optional JSON object (as a string) bound to `vars`. */
  vars?: string;
  /** Optional map bound to the CEL `env` variable. */
  env?: Record<string, string>;
}

/** The outcome of a CEL evaluation: the value, or the compile/eval error. */
export interface CelEvalResult {
  /** True when a well-formed envelope came back (whatever the CEL outcome). */
  ok: boolean;
  /** The evaluated value on success (may itself be false/0/null). */
  result?: unknown;
  /** The compile/eval (or runner) error message, when it failed. */
  error?: string;
}

/**
 * One canned outcome of a mocked block, as the RUNNER takes it (the runtime's
 * `core.MockCase`). Unlike the editor's own `MockCase` in meta/types.ts, `body` and `vars`
 * are *values* here rather than JSON text — run/debug.ts is where the one becomes the
 * other.
 *
 * Exactly one of `body`, `error` and `drop` is set: a block either returns a message,
 * fails, or filters it out. `vars` only goes alongside a `body`.
 */
export interface MockCaseSpec {
  /** CEL, evaluated against the message the block RECEIVED. Absent on a default. */
  when?: string;
  body?: unknown;
  vars?: Record<string, unknown>;
  error?: string;
  drop?: boolean;
}

/** What one addressed block is stood in for with (the runtime's `core.MockSpec`). */
export interface MockSpec {
  /** Tried in order; the first whose `when` holds wins. */
  cases?: MockCaseSpec[];
  /** What runs when no case matched. Without one, an unmatched message fails the block. */
  default?: MockCaseSpec;
}

/** One execution of a spied block (the runtime's `core.SpyRecord`). */
export interface SpyRecord {
  /** Orders records across EVERY spy, not just this one: one timeline for the whole run. */
  seq: number;
  at: string;
  /** The message as the block received it, captured before it ran. */
  input: unknown;
  /** Exactly one of these three: what the block produced, or that it dropped, or failed. */
  output?: unknown;
  dropped?: boolean;
  error?: string;
}

/** What one spied block saw, in the order it saw it. */
export interface SpyTrace {
  address: string;
  /** Empty is a result, not a gap: the flow may not have taken the branch the block is on. */
  records: SpyRecord[];
}

/**
 * A request to run ONE flow once, without starting the integration's sources.
 *
 * The three debug features are the runtime's, and they compose: `breakAt` halts the run at
 * a block and reports the message it was carrying (see {@link BreakOutcome}); `spies`
 * records what crosses other blocks without changing anything the flow does; `mocks`
 * stands in for blocks so the real ones never run. All three address a block by the same
 * path grammar (see run/address.ts).
 */
export interface FlowRunRequest {
  /** The config to run — for a breakpoint run, the plan's clone (see planBreakpoint). */
  yaml: string;
  /** The name of the flow to invoke. */
  flow: string;
  /** The open integration, so the host can resolve its resources; absent for a draft. */
  integrationId?: string;
  /** JSON request body bound to the message's `body`. */
  data?: string;
  /** JSON object seeding the message's variables — what a source would normally set. */
  vars?: string;
  /** A breakpoint address (see planBreakpoint): run until this block, then stop. */
  breakAt?: string;
  /** Block addresses to record every message that crosses them. */
  spies?: string[];
  /** Blocks to stand in for, by address; the real block never runs. */
  mocks?: Record<string, MockSpec>;
}

/** What the flow was carrying at the breakpoint, or why it never got there. */
export interface BreakOutcome {
  /** False when the flow never took the branch the block is on — a normal outcome. */
  reached: boolean;
  /** The address the breakpoint was set on. */
  block: string;
  /** The message as it looked after the addressed block ran; absent when unreached. */
  message?: unknown;
  /** The flow's own failure, reported in-band rather than as a transport error. */
  error?: string;
}

/** The outcome of one flow run. */
export interface FlowRunOutcome {
  /** True when the runner completed the call (whatever the flow itself did). */
  ok: boolean;
  /** True when the flow filtered the message, so there is no result at all. */
  dropped: boolean;
  timedOut: boolean;
  /**
   * The flow's result message as JSON text — `{event_id, variables, body}`, the same
   * shape a breakpoint reports — so a finished run shows the variables it built up and
   * not just its body. Empty for a breakpoint run, which reports `breakpoint` instead.
   */
  output: string;
  /** The runner's stderr lines. Manual runs are quiet (error level), so usually empty. */
  logs: string[];
  /** Present only for a `breakAt` run. */
  breakpoint?: BreakOutcome;
  /** Present only for a `spies` run: every message that crossed each spied block. */
  spies?: SpyTrace[];
  /** Why the run could not be made at all (a bad address, an unloadable config). */
  error?: string;
}

/** Point-in-time runner state, as the provider needs it. */
export interface RunStatusSnapshot {
  available: boolean;
  running: boolean;
  /** The runner's `--version` line, or null when unknown/unavailable. */
  version: string | null;
  /** App-relative path that proxies to the running networked integration, or null. */
  testPath: string | null;
}

/** Moves RUN requests/streams to a backend; carries no client policy itself. */
export interface RunTransport {
  /** Current availability/running state (used on mount and to reattach). */
  status(): Promise<RunStatusSnapshot>;
  /**
   * Start a runner for the given config; resolves to the new state. `integrationId`
   * identifies the open integration so the host can resolve its resources (env
   * files, templates, and the dev-env `.env.dev`) from its backend; absent for an
   * unsaved draft.
   */
  start(args: {
    yaml: string;
    integrationId?: string;
  }): Promise<RunStatusSnapshot>;
  /** Stop the current runner. */
  stop(): Promise<void>;
  /** Push a new config to the running runner so it hot-reloads. */
  sync(args: { yaml: string; integrationId?: string }): Promise<void>;
  /**
   * Run one flow once and return what it produced — the editor's debug path, distinct
   * from {@link start}, which boots the whole integration and its sources. Independent
   * of a long-running runner: a flow can be invoked while one is running, or with none
   * at all. The log level is the host's policy, not the caller's.
   */
  invoke(req: FlowRunRequest): Promise<FlowRunOutcome>;
  /**
   * Evaluate a single CEL expression against an ad-hoc object, without running a
   * flow — backs the CEL tester. Stateless; unavailable when the runner is not
   * configured, in which case it resolves to `{ ok:false, error }`.
   */
  evalCel(req: CelEvalRequest): Promise<CelEvalResult>;
  /**
   * Subscribe to the runner's log stream. `onLine` receives each line's monotonic
   * sequence number and text; the returned function unsubscribes. Replays and
   * de-duplication are the provider's concern, not the transport's.
   */
  subscribeLogs(onLine: (seq: number, text: string) => void): () => void;
}
