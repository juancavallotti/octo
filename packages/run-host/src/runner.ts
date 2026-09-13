import type { ResourceProvider } from "./resources";

/**
 * The port for the app being run: start it, stop it, ask after it, make it pick up a
 * change, read its logs.
 *
 * The interface only — no implementation. A backend may run the app as a child of the
 * host process, taking each edit as it is pushed, or somewhere else entirely, pulling the
 * saved definition itself; either way it belongs to the host that runs that way, and this
 * package holds what every host shares regardless.
 *
 * The one-shot operations (`invoke`, `evalCel`, `test`) are not part of this port and do
 * stay in this package: they spawn a child, wait for it, and return — no port, no buffer,
 * no long-lived process — so they need no backend chosen.
 */

/**
 * One line of a run's output.
 *
 * Declared beside the port that yields it rather than with any buffer that holds one:
 * backends differ in where the lines come from and agree only on this shape. `seq` is
 * monotonic within a stream and doubles as an SSE event id, so a client can order lines
 * and drop what a reconnect replays.
 */
export interface LogLine {
  seq: number;
  text: string;
}

/**
 * What identifies a run to a backend: the namespace for a local child process, the
 * (user, integration) pair for a dev run. Every half travels together and each backend
 * reads the one it keys on — so a caller does not have to know which backend it got.
 */
export interface RunKey {
  /** The per-browser namespace slug (see namespace.ts). A local runner's whole key. */
  namespace: string;
  /** The open integration, when it is saved; absent for a draft. */
  integrationId?: string;
  /**
   * The user the run belongs to. A local runner has no use for it — a child process is
   * already scoped to whoever reached it — while a backend that runs elsewhere may own
   * its runs per user and be unable to address one without it.
   */
  userId?: string;
}

/** What a start needs beyond the key. */
export interface StartArgs {
  /**
   * The rendered config to run.
   *
   * Meaningful only to a **pushing** backend, which writes it to the file `octo` watches.
   * A pulling backend fetches the saved definition itself and ignores this, so its running
   * app reflects what was saved rather than what is in a caller's buffer.
   */
  yaml: string;
  /** The editor's "Dev .env" values, injected into the child's environment. Local only. */
  devEnv?: Record<string, string>;
  /** Resolves the resource files the config declares. Local only, for the same reason. */
  resources?: ResourceProvider;
}

/** What a sync needs beyond the key. `yaml` carries the same caveat as {@link StartArgs}. */
export interface SyncArgs {
  yaml: string;
  resources?: ResourceProvider;
}

/** How much of the log history to replay, and when to stop following. */
export interface LogStreamOptions {
  /**
   * Resume after this sequence number, so an SSE reconnect carrying a Last-Event-ID does
   * not replay what the client already showed. Omitted replays everything buffered.
   */
  fromSeq?: number;
  /**
   * Ends the stream. Required rather than optional: a follow has no natural end, and a
   * consumer that merely stops iterating cannot wake a generator parked waiting for the
   * next line — so without this the only way out is to leak it.
   */
  signal: AbortSignal;
}

/**
 * Point-in-time state of the app a backend is running.
 *
 * Only what a backend can actually answer. Whether the host has an `octo` binary, and
 * which version, is not here (see {@link Binaries}): that is a fact about the host, and
 * on a host whose app runs elsewhere the two come apart.
 */
export interface RunState {
  running: boolean;
  /** Whether the current run is networked/testable: it serves an HTTP source at the
   * address the host injects, which an HTTP source alone does not guarantee — a
   * connector that pins its own port or host is out of reach. */
  exposable: boolean;
  /**
   * The listen port of a local networked run, null otherwise — including for a backend
   * whose run owns its own network namespace and so needs no port negotiated here.
   */
  port: number | null;
  /**
   * Where to reach the running networked integration, or null when it serves no HTTP.
   *
   * Relative when the host itself proxies to the run, absolute when the run answers on
   * a host of its own. Both are valid inputs to `new URL(value, origin)`, which is how a
   * client turns either into something to link to.
   */
  testUrl: string | null;
  /**
   * Whether this backend reloads the running app when the integration is SAVED, rather
   * than from the buffer a caller pushes. True for a backend that pulls the stored
   * definition itself; false for one that runs whatever YAML it was last handed. A
   * caller reads it to decide whether pushing edits is worth anything at all.
   */
  reloadsOnSave: boolean;
  /**
   * Why the app is not running, or not usable, when the backend can say so — a pod's
   * ImagePullBackOff, a workload nothing has scheduled. Absent for a healthy run, and
   * always absent from the local backend, whose failures arrive as log lines instead.
   */
  reason?: string;
}

/** Drives one backend's long-running app. */
export interface AppRunner {
  /** Current state, including whether a run is already live (used to reattach). */
  status(key: RunKey): Promise<RunState>;
  /** Start a run, replacing any previous generation. */
  start(key: RunKey, args: StartArgs): Promise<RunState>;
  /** Stop the run and release what it held. */
  stop(key: RunKey): Promise<RunState>;
  /** Make the running app pick up the current definition. */
  sync(key: RunKey, args: SyncArgs): Promise<RunState>;
  /**
   * Replay then follow the run's logs; ends when the caller aborts.
   *
   * An async iterable rather than a subscribe callback because it is what the two
   * backends can both honestly produce — the local one wraps a buffer and a listener, the
   * remote one an HTTP stream — and because it collapses both apps' SSE routes to one
   * shape.
   */
  logs(key: RunKey, opts: LogStreamOptions): AsyncIterable<LogLine>;
}
