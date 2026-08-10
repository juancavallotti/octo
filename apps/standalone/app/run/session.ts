import type { ChildProcess } from "node:child_process";
import { LogBuffer } from "./logBuffer";
import { ensureReaper } from "./reaper";
import type { LogLine, RunState } from "@octo/run-host";

/**
 * What a local run *is*, and how you read one.
 *
 * This is the half of the local runner that holds no processes: the session record,
 * the `globalThis` store that survives Next's dev HMR reloads, the per-namespace
 * lock that serialises the operations which do spawn and kill, and the read-only
 * accessors. `localRunner.ts` owns the other half — starting, syncing and stopping —
 * and is the only module that mutates a session's process fields.
 *
 * The split is along that line deliberately. Everything here is safe to call from
 * anywhere at any time: the reaper sweeps the map, the reverse proxy asks for a
 * port, the SSE route tails a buffer. None of them should have to reason about a
 * start that is halfway through.
 */

export interface Session {
  /** The namespace slug this session belongs to (also its key in the map). */
  namespace: string;
  /** Epoch ms of the last activity for this namespace; drives the idle reaper. */
  lastActivity: number;
  proc: ChildProcess | null;
  /** Resolves when the current process has fully exited (used by stop/restart). */
  exit: Promise<void> | null;
  configPath: string | null;
  logs: LogBuffer;
  /** Allocated HTTP port for a networked run; null when not running or internal-only. */
  port: number | null;
  /**
   * Allocated admin port for the runner's observability service (probes and
   * metrics); null when not running. Every run gets one, networked or not — the
   * runtime serves probes whether or not the integration has an HTTP source.
   */
  adminPort: number | null;
  /** Whether the current run declares HTTP_PORT (set on start). */
  exposable: boolean;
  /** Absolute paths of resource files staged for the current run, removed on stop. */
  stagedResources: string[];
  /** The resource names the current run declared, to detect a resource-list change on sync. */
  declaredResources: string[];
}

const store = globalThis as unknown as {
  __octoRunSessions?: Map<string, Session>;
  __octoRunLocks?: Map<string, Promise<unknown>>;
  __octoRunKillHook?: boolean;
};

/** The full namespace→session map (used by the reaper to sweep idle runs). */
export function allSessions(): Map<string, Session> {
  if (!store.__octoRunSessions) store.__octoRunSessions = new Map();
  return store.__octoRunSessions;
}

/** Per-namespace operation locks, on `globalThis` alongside the session map so they
 * survive Next's dev HMR reloads. */
function locks(): Map<string, Promise<unknown>> {
  if (!store.__octoRunLocks) store.__octoRunLocks = new Map();
  return store.__octoRunLocks;
}

/**
 * Run `fn` with exclusive access to a namespace: start, stop and sync all mutate the
 * same child process, the same pooled ports and the same config file, so they must not
 * interleave. A double-clicked Run fires two starts at once; without this the second
 * start's teardown runs before the first has recorded its child — both `octo` processes
 * spawn, and the one whose `s.proc` is overwritten is orphaned with its ports never
 * released (its exit handler sees `s.proc !== proc` and frees nothing). Each call chains
 * onto the namespace's previous one and runs no matter how that one settled.
 *
 * Read-only calls (status, snapshot, logs) intentionally stay off the lock: a slow start
 * must not block a status poll or a log tail, and none of them mutate the run.
 */
export function withNamespaceLock<T>(ns: string, fn: () => Promise<T>): Promise<T> {
  const map = locks();
  const prev = map.get(ns) ?? Promise.resolve();
  const result = prev.then(() => fn());
  // The next waiter chains on completion regardless of this one's outcome, so a thrown
  // start does not wedge the namespace; its value and error stay with `result`.
  const gate = result.then(
    () => {},
    () => {},
  );
  map.set(ns, gate);
  // Drop the entry once it is the settled tail, so the map does not keep one dead
  // promise per namespace for the life of the process.
  void gate.then(() => {
    if (map.get(ns) === gate) map.delete(ns);
  });
  return result;
}

/** Get-or-create the session for a namespace, renewing its activity timestamp so
 * any manager call (status, start, sync, logs, proxy) counts as activity and keeps
 * the idle reaper from collecting it. */
export function session(ns: string): Session {
  const map = allSessions();
  let s = map.get(ns);
  if (!s) {
    s = {
      namespace: ns,
      lastActivity: Date.now(),
      proc: null,
      exit: null,
      configPath: null,
      logs: new LogBuffer(),
      port: null,
      adminPort: null,
      exposable: false,
      stagedResources: [],
      declaredResources: [],
    };
    map.set(ns, s);
    ensureReaper();
  }
  s.lastActivity = Date.now();
  return s;
}

export function statusOf(s: Session): RunState {
  // The proxy resolves the actual port server-side, so the test path is
  // port-independent — just the namespace.
  const networked = s.proc !== null && s.exposable && s.port !== null;
  return {
    running: s.proc !== null,
    exposable: s.exposable,
    port: s.port,
    testUrl: networked ? `/editor/runs/${s.namespace}/` : null,
    // A local runner runs the YAML it was last handed, so a save on its own changes
    // nothing: the editor has to push the buffer for anything to happen.
    reloadsOnSave: false,
  };
}

/** The listen port of a namespace's running networked integration, or null when it
 * is not running or not networked. Used by the reverse proxy to find the target. */
export function runningPort(ns: string): number | null {
  const s = session(ns);
  return s.proc !== null ? s.port : null;
}

export function status(ns: string): RunState {
  return statusOf(session(ns));
}

/** The config file the namespace's running generation is watching (for tests/inspection). */
export function currentConfigPath(ns: string): string | null {
  return session(ns).configPath;
}

/** Replay the namespace's current log buffer (oldest first). */
export function snapshot(ns: string): LogLine[] {
  return session(ns).logs.snapshot();
}

/** Subscribe to the namespace's new log lines; returns an unsubscribe function. */
export function subscribe(ns: string, fn: (line: LogLine) => void): () => void {
  return session(ns).logs.subscribe(fn);
}

/**
 * Best-effort: don't leave any runner orphaned when the editor process exits.
 *
 * It lives here rather than beside `start` because it is a sweep over the whole
 * store, not an operation on one run — the same shape as the idle reaper, and it
 * needs the same private handle to `globalThis`.
 */
export function ensureKillOnExit(): void {
  if (store.__octoRunKillHook) return;
  store.__octoRunKillHook = true;
  process.once("exit", () => {
    for (const s of allSessions().values()) {
      if (s.proc) {
        try {
          s.proc.kill("SIGKILL");
        } catch {
          // nothing we can do on the way out
        }
      }
    }
  });
}
