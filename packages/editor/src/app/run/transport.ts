/**
 * The RUN capability's transport contract: the small surface the RunProvider
 * needs to drive a runner, decoupled from how it is reached. The editor's
 * provider holds all the client-side policy (debounced sync, log dedupe,
 * validation gating); a transport only moves bytes — so the same provider works
 * whether the runner is reached through a platform BFF or a standalone app's
 * local process. The concrete transports live in the apps that embed the editor.
 */

import type { TestRunOutcome, TestRunRequest } from "./testTransport";

// The Testing tab's types live next door — they are as long again as the rest of this
// contract — but they are part of it, so they are re-exported from here.
export * from "./testTransport";

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
  /**
   * Whether the host can run test suites — a *second* binary (dolphin), so this is not
   * implied by `available`. Either can be missing on its own: a host with a runner but
   * no test runner still runs flows, and only the Testing tab's run controls go dead.
   */
  testAvailable: boolean;
  /** dolphin's `version` line, or null when unknown/unavailable. */
  testVersion: string | null;
  /**
   * Whether this run will ever have a {@link testUrl} — i.e. it declares an HTTP_PORT and
   * so is networked. Distinct from `testUrl !== null`: a backend can know a run is exposable
   * before it can hand out the URL, which is exactly what a dev-run pod does — its public
   * endpoint is withheld until the pod is ready, so a networked run reports `exposable:true`
   * with `testUrl:null` for the seconds its image is still pulling. The provider reads it to
   * decide whether a null URL is "coming" (keep polling) or "never" (a run that serves no
   * HTTP), so it neither leaves the endpoint link blank on a slow start nor spins forever on
   * a run that will never publish one.
   */
  exposable: boolean;
  /**
   * Where to reach the running networked integration, or null when it serves no HTTP.
   *
   * App-relative for a host that runs the app itself and proxies to it; absolute for one
   * that runs it elsewhere and gives it its own hostname. Either is a valid input to
   * `new URL(value, origin)` — an absolute value ignores the base — so a consumer needs
   * no branch on which kind it got.
   *
   * Null while an {@link exposable} run is still coming up: a dev-run pod's endpoint is held
   * back until it is ready, so a link offered earlier would answer 502 while the image pulls.
   */
  testUrl: string | null;
  /**
   * Whether the host reloads the running app when the integration is SAVED, rather than
   * from the buffer the editor pushes.
   *
   * True where the runner reads the stored definition itself; false where it runs
   * whatever YAML it was last handed. The provider reads it to decide whether to
   * debounce-push edits at all: pushing a buffer nothing will read is worse than not
   * pushing, because the RUN panel would then imply the running app had changed.
   */
  reloadsOnSave: boolean;
}

/**
 * Which run an operation addresses.
 *
 * The open integration, when it is saved. Every method carries it — not only the ones
 * that need its resources — because a host may key the run itself on it: one that runs
 * the app beside the editor keys on the browser instead and ignores this, while one that
 * runs it elsewhere has nothing else to name it by. A draft has no id and so cannot be
 * addressed by the second kind at all.
 */
export interface RunTarget {
  integrationId?: string;
}

/** Moves RUN requests/streams to a backend; carries no client policy itself. */
export interface RunTransport {
  /** Current availability/running state (used on mount and to reattach). */
  status(target: RunTarget): Promise<RunStatusSnapshot>;
  /**
   * Start a runner for the given config; resolves to the new state. The target's
   * `integrationId` also lets the host resolve the integration's resources (env files,
   * templates, and the dev-env `.env.dev`) from its backend.
   *
   * `yaml` carries the same caveat as {@link sync}'s: a host whose runner pulls the
   * stored definition runs what was SAVED and ignores it, and refuses a draft outright
   * — there is nothing stored to run.
   */
  start(args: RunTarget & { yaml: string }): Promise<RunStatusSnapshot>;
  /** Stop the current runner. */
  stop(target: RunTarget): Promise<void>;
  /**
   * Make the running runner pick up the current definition.
   *
   * `yaml` is meaningful only to a host that PUSHES config: it writes what it is handed.
   * A host whose runner pulls the stored definition ignores it, and there this is an
   * explicit "reload now" rather than the per-edit trigger — see
   * {@link RunStatusSnapshot.reloadsOnSave}, which is how a caller knows which it has.
   */
  sync(args: RunTarget & { yaml: string }): Promise<void>;
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
   *
   * The target is taken here too, for the same reason {@link status} takes it: on a host
   * that runs the app elsewhere, there is no stream to open without knowing which run.
   */
  subscribeLogs(
    onLine: (seq: number, text: string) => void,
    target: RunTarget,
  ): () => void;
  /**
   * Run a flow's dolphin suites and return the report — the Testing tab's Run.
   *
   * Distinct from {@link invoke} in what it asserts, not in what it runs: dolphin drives
   * `octo invoke` once per case, so this is the same debug path with the suite's
   * expectations checked against the result. Availability is its own flag
   * ({@link RunStatusSnapshot.testAvailable}) because dolphin can be absent while octo
   * is present.
   *
   * Two different failures, told apart the same way {@link invoke} tells them apart: it
   * REJECTS when the call could not be made (no runner, no session, a transport error),
   * and resolves with `ok: false` when dolphin ran but produced no report worth reading.
   * A run whose tests merely failed resolves with `ok: true` — that is the report the
   * user came for, and {@link TestTotals} holds the verdict.
   */
  test(req: TestRunRequest): Promise<TestRunOutcome>;
}
