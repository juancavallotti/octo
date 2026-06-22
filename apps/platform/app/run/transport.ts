/**
 * The RUN capability's transport contract: the small surface the RunProvider
 * needs to drive a runner, decoupled from how it is reached. The editor's
 * provider holds all the client-side policy (debounced sync, log dedupe,
 * validation gating); a transport only moves bytes — so the same provider works
 * whether the runner is reached through the platform's BFF routes or a
 * standalone app's local process. Absence of a transport means RUN is hidden.
 */

/** Point-in-time runner state, as the provider needs it. */
export interface RunStatusSnapshot {
  available: boolean;
  running: boolean;
  /** The runner's `--version` line, or null when unknown/unavailable. */
  version: string | null;
  /** BFF-relative path that proxies to the running networked integration, or null. */
  testPath: string | null;
}

/** Moves RUN requests/streams to a backend; carries no client policy itself. */
export interface RunTransport {
  /** Current availability/running state (used on mount and to reattach). */
  status(): Promise<RunStatusSnapshot>;
  /** Start a runner for the given config; resolves to the new state. */
  start(args: {
    yaml: string;
    devEnv: Record<string, string>;
  }): Promise<RunStatusSnapshot>;
  /** Stop the current runner. */
  stop(): Promise<void>;
  /** Push a new config to the running runner so it hot-reloads. */
  sync(args: { yaml: string }): Promise<void>;
  /**
   * Subscribe to the runner's log stream. `onLine` receives each line's monotonic
   * sequence number and text; the returned function unsubscribes. Replays and
   * de-duplication are the provider's concern, not the transport's.
   */
  subscribeLogs(onLine: (seq: number, text: string) => void): () => void;
}

interface RunStatusResponse {
  available: boolean;
  running: boolean;
  version: string | null;
  testPath: string | null;
}

/**
 * The platform transport: talks to the editor's BFF run routes under `/api/run`
 * and streams logs over SSE. This is exactly the wiring the RunProvider used
 * inline before transports were extracted.
 */
export const bffRunTransport: RunTransport = {
  async status() {
    const r = await fetch("/api/run");
    return (await r.json()) as RunStatusResponse;
  },

  async start({ yaml, devEnv }) {
    const res = await fetch("/api/run/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ yaml, devEnv }),
    });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(body.error ?? `start failed (${res.status})`);
    }
    return body as RunStatusResponse;
  },

  async stop() {
    await fetch("/api/run/stop", { method: "POST" });
  },

  async sync({ yaml }) {
    await fetch("/api/run/sync", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ yaml }),
    });
  },

  subscribeLogs(onLine) {
    const es = new EventSource("/api/run/logs");
    es.onmessage = (ev) => {
      const seq = Number(ev.lastEventId);
      onLine(seq, ev.data);
    };
    return () => es.close();
  },
};
