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
