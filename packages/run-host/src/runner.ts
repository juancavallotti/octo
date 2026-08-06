import type { ResourceProvider } from "./resources";

/**
 * The port for "the app the editor is running": start it, stop it, ask after it, make it
 * pick up a change, read its logs.
 *
 * There are two backends, and **neither of them is here**. A local one spawns
 * `octo run --watch` as a child of the host process and pushes each edit into the file it
 * watches; a remote one asks the orchestrator for a dev-run pod whose sidecar pulls the
 * saved definition itself. Each lives in the app that runs that way
 * (`apps/standalone/app/run/localRunner.ts`, `apps/platform/app/run/remoteRunner.ts`),
 * because an implementation shared by one caller is not shared code — it is a dependency
 * pointing the wrong way. What belongs to both is this interface, and everything the editor
 * does regardless of which it got: the debounce, the log dedupe, the validation gating.
 *
 * The one-shot operations (`invoke`, `evalCel`, `test`) are deliberately NOT here, and they
 * DO stay in this package. They spawn a child, wait for it, and return — no port, no
 * buffer, no process — so they need no backend selected, and both hosts run them the same
 * way, locally, which is what makes them genuinely shared.
 */

/**
 * One line of a run's output.
 *
 * Declared beside the port that yields it rather than with any buffer that holds one: a
 * local backend's lines come from a ring buffer in its own heap, a remote backend's from a
 * pod's log stream, and the only thing they have in common is this shape. `seq` is
 * monotonic within a stream and doubles as the SSE event id, so a client can order lines
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
  /** The per-browser namespace slug (see namespace.ts). The local runner's whole key. */
  namespace: string;
  /** The open integration, when it is saved. A dev run is keyed by it; absent for a draft. */
  integrationId?: string;
  /**
   * The user the run belongs to, as the orchestrator knows them. The local runner has no
   * use for it — a child process of this replica is already scoped to whoever reached it
   * — while a dev run is owned by it and cannot be addressed without one.
   */
  userId?: string;
}

/** What a start needs beyond the key. */
export interface StartArgs {
  /**
   * The rendered config to run.
   *
   * Meaningful only to a **pushing** backend. The local runner writes this to the file
   * `octo` watches; a dev run's sidecar pulls the saved definition from the orchestrator
   * and ignores it, which is the visible edge of the push/pull difference: on platform
   * the running app reflects what was SAVED, not what is in the editor's buffer.
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
 * which version, is deliberately NOT here (see {@link Binaries}): that is a fact about
 * the host, and on a host whose app runs elsewhere the two come apart — it can be unable
 * to start an app while still running every one-shot perfectly well. The app composes the
 * editor's snapshot from both halves.
 */
export interface RunState {
  running: boolean;
  /** Whether the current run declares HTTP_PORT, i.e. is networked/testable. */
  exposable: boolean;
  /**
   * The listen port of a local networked run, null otherwise — including for every dev
   * run, which owns its own network namespace and so listens on a platform constant that
   * nothing has to negotiate or record.
   */
  port: number | null;
  /**
   * Where to reach the running networked integration, or null when it serves no HTTP.
   *
   * App-relative for a local runner (a BFF path that proxies to the child's port), and
   * absolute for a dev run (its own public host). Both are valid inputs to
   * `new URL(value, origin)`, which is how a client turns either into something to link
   * to — an absolute value ignores the base.
   */
  testUrl: string | null;
  /**
   * Whether this backend reloads the running app when the integration is SAVED, rather
   * than from the buffer the editor pushes.
   *
   * True for a dev run, whose sidecar pulls the stored definition; false for a local
   * child process, which runs whatever YAML it was last handed. The editor reads it to
   * decide whether to debounce-push edits at all — pushing a buffer that nothing will
   * read would be worse than not pushing, because the RUN panel would imply the running
   * app had changed.
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
