/**
 * The RUN capability's transport contract: the small surface the RunProvider
 * needs to drive a runner, decoupled from how it is reached. The editor's
 * provider holds all the client-side policy (debounced sync, log dedupe,
 * validation gating); a transport only moves bytes — so the same provider works
 * whether the runner is reached through a platform BFF or a standalone app's
 * local process. The concrete transports live in the apps that embed the editor.
 */

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
   * files, templates) from its backend; absent for an unsaved draft.
   */
  start(args: {
    yaml: string;
    devEnv: Record<string, string>;
    integrationId?: string;
  }): Promise<RunStatusSnapshot>;
  /** Stop the current runner. */
  stop(): Promise<void>;
  /** Push a new config to the running runner so it hot-reloads. */
  sync(args: { yaml: string; integrationId?: string }): Promise<void>;
  /**
   * Subscribe to the runner's log stream. `onLine` receives each line's monotonic
   * sequence number and text; the returned function unsubscribes. Replays and
   * de-duplication are the provider's concern, not the transport's.
   */
  subscribeLogs(onLine: (seq: number, text: string) => void): () => void;
}
