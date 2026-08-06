import { spawn, type ChildProcess } from "node:child_process";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { cachedTestVersion, cachedVersion } from "../version";
import {
  allocateAdminPort,
  allocatePort,
  isExposable,
  releaseAdminPort,
  releasePort,
} from "../ports";
import { LogBuffer, type LogLine } from "../logbuffer";
import { ensureReaper } from "../reaper";
import {
  effectiveResourceNames,
  injectDevEnvResource,
  resolveAndStage,
  sameNameSet,
  stageResources,
} from "../resources";
import { namespaceDir, writeConfig, type RunResourceOptions } from "../staging";
import { octoBin, terminate } from "../child";
import type { AppRunner, LogStreamOptions, RunKey, RunStatus } from "../runner";

/**
 * The local {@link AppRunner}: it owns the `octo` processes running as children of this
 * one. The editor POSTs YAML, this spawns `octo run -config <file> -watch`, captures
 * stdout/stderr as log lines, and lets clients replay the buffer and subscribe to new
 * lines. Editing the document re-writes the same config file so the runner hot-reloads.
 *
 * Runs are keyed by a per-user namespace slug (see namespace.ts) so concurrent editor
 * users don't disturb one another: each namespace owns an independent process, config
 * file, and log buffer. State lives on `globalThis` so it survives Next's dev HMR module
 * reloads (a new module instance would otherwise lose track of the child processes).
 *
 * Everything here is inherently pod-local — a process handle, a port from a local pool, a
 * log buffer in this heap — and that is exactly why it cannot be the only backend: with
 * more than one replica and no session affinity, a request landing on a different one
 * finds none of it. The remote runner exists for that case; this one stays the right
 * answer for standalone and for a single-replica install.
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
  __octoRunKillHook?: boolean;
};

/** The full namespace→session map (used by the reaper to sweep idle runs). */
export function allSessions(): Map<string, Session> {
  if (!store.__octoRunSessions) store.__octoRunSessions = new Map();
  return store.__octoRunSessions;
}

/** Get-or-create the session for a namespace, renewing its activity timestamp so
 * any manager call (status, start, sync, logs, proxy) counts as activity and keeps
 * the idle reaper from collecting it. */
function session(ns: string): Session {
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

function statusOf(s: Session): RunStatus {
  // The proxy resolves the actual port server-side, so the test path is
  // port-independent — just the namespace.
  const networked = s.proc !== null && s.exposable && s.port !== null;
  return {
    available: !!process.env.OCTO_BIN_PATH,
    running: s.proc !== null,
    version: cachedVersion(),
    testAvailable: !!process.env.DOLPHIN_BIN_PATH,
    testVersion: cachedTestVersion(),
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

export function status(ns: string): RunStatus {
  return statusOf(session(ns));
}

/** The config file the namespace's running generation is watching (for tests/inspection). */
export function currentConfigPath(ns: string): string | null {
  return session(ns).configPath;
}

/**
 * Start (or restart) the namespace's runner with the given rendered config YAML.
 * `devEnv` (the editor's "Dev .env" values) is injected into the spawned process's
 * environment below and never written to disk — the `env` object is local to this
 * call. The Go runtime resolves OS env before declared defaults, so this suffices.
 */
export async function start(
  ns: string,
  yaml: string,
  devEnv?: Record<string, string>,
  opts?: RunResourceOptions,
): Promise<RunStatus> {
  const bin = octoBin();

  await stop(ns); // tear down any previous generation first

  const s = session(ns);
  s.logs.reset(); // fresh buffer per run; seq stays monotonic so clients still dedupe

  const dir = namespaceDir(ns);
  await mkdir(dir, { recursive: true });

  // Stage the config's declared resources into the run dir first: the runtime's
  // fs loader roots at the config directory, and the config write below is what
  // triggers its initial load, so the files must already be present.
  const { declared, staged } = await resolveAndStage(dir, yaml, opts?.resources);
  s.stagedResources = staged;
  s.declaredResources = declared;

  const configPath = join(dir, `octo-editor-${randomUUID()}.yaml`);
  await writeConfig(configPath, injectDevEnvResource(yaml));
  s.configPath = configPath;

  // The run's ports, taken together so a failure to get one does not strand the
  // other. Each is recorded on the session as it is taken, which is what lets the
  // rollback below hand it back.
  const exposable = isExposable(yaml);
  let adminPort: number;
  try {
    // A networked integration (one that declares HTTP_PORT) gets a real port from
    // the pool, injected as HTTP_PORT so the BFF can proxy to it. HTTP_HOST is the
    // loopback because only the same-pod proxy needs to reach it. Internal-only runs
    // (no HTTP_PORT) get no port and stay unexposed.
    s.exposable = exposable;
    s.port = exposable ? allocatePort() : null;

    // Every run — networked or not — also gets an admin port for the runtime's
    // observability service, which is on by default and otherwise binds the same
    // fixed :39999 in every run on this host: the first run would take it and every
    // later one would come up without probes or metrics. Loopback, because only this
    // host reaches it.
    adminPort = allocateAdminPort();
    s.adminPort = adminPort;
  } catch (err) {
    // An exhausted pool ends this start, so roll the whole thing back: stop()
    // returns whichever port was taken and removes the config and staged resources
    // (env files hold secrets) instead of leaving them for whoever calls stop next.
    // Repeated failed starts would otherwise eat the pool a port at a time.
    await stop(ns);
    throw err;
  }
  const port = s.port;

  // Base process env, then the dev env values; the port wiring is applied last so
  // a stray dev value can't clobber it.
  const env: NodeJS.ProcessEnv = { ...process.env, ...(devEnv ?? {}) };
  if (port !== null) {
    env.HTTP_PORT = String(port);
    env.HTTP_HOST = "127.0.0.1";
  }
  env.OCTO_OBSERVABILITY_ADDR = `127.0.0.1:${adminPort}`;

  s.logs.push(`▶ starting octo — ${configPath}`);
  if (port !== null) {
    s.logs.push(`🔗 test your integration at /editor/runs/${ns}/`);
  }
  const proc = spawn(bin, ["run", "-config", configPath, "-watch"], {
    stdio: ["ignore", "pipe", "pipe"],
    env,
  });
  s.proc = proc;
  s.logs.pipe(proc.stdout);
  s.logs.pipe(proc.stderr);

  s.exit = new Promise<void>((resolve) => {
    const finish = () => {
      if (s.proc === proc) {
        s.proc = null;
        // Free this generation's ports when it exits on its own (crash/exit).
        if (port !== null && s.port === port) {
          releasePort(port);
          s.port = null;
        }
        if (s.adminPort === adminPort) {
          releaseAdminPort(adminPort);
          s.adminPort = null;
        }
      }
      resolve();
    };
    proc.on("error", (err) => {
      s.logs.push(`✖ failed to start runner: ${err.message}`);
      finish();
    });
    // Resolve on "exit" (process gone) rather than "close" (stdio EOF) so stop()
    // stays responsive even if a child inherits and holds the output pipes.
    proc.on("exit", (code, signal) => {
      s.logs.push(
        `■ runner exited (${signal ? `signal ${signal}` : `code ${code ?? 0}`})`,
      );
      finish();
    });
  });

  ensureKillOnExit();
  return statusOf(s);
}

/**
 * Re-render the config the namespace's runner is watching, triggering a hot reload.
 * No-op if stopped. When the config's declared resource-name set has changed since
 * the last staging and a provider is given, the resources are re-resolved and
 * re-staged (and files no longer declared are removed) before the config is
 * rewritten — an ordinary content-only edit skips the provider entirely.
 */
export async function sync(
  ns: string,
  yaml: string,
  opts?: RunResourceOptions,
): Promise<RunStatus> {
  const s = session(ns);
  if (!s.proc || !s.configPath) return statusOf(s);

  const declared = effectiveResourceNames(yaml);
  if (opts?.resources && !sameNameSet(declared, s.declaredResources)) {
    const dir = namespaceDir(ns);
    const files = await opts.resources(declared);
    const nextStaged = await stageResources(dir, files, declared);
    // Remove files that were staged before but are no longer declared/supplied.
    const keep = new Set(nextStaged);
    for (const path of s.stagedResources) {
      if (!keep.has(path)) await rm(path, { force: true }).catch(() => {});
    }
    s.stagedResources = nextStaged;
    s.declaredResources = declared;
  }

  await writeConfig(s.configPath, injectDevEnvResource(yaml));
  return statusOf(s);
}

/** Stop the namespace's runner (SIGTERM, then SIGKILL after a grace period) and remove its config. */
export async function stop(ns: string): Promise<RunStatus> {
  const s = session(ns);
  const proc = s.proc;
  if (proc) {
    const cancelKill = terminate(proc);
    try {
      await s.exit;
    } finally {
      cancelKill();
    }
  }
  s.proc = null;
  s.exit = null;
  if (s.port !== null) {
    releasePort(s.port);
    s.port = null;
  }
  if (s.adminPort !== null) {
    releaseAdminPort(s.adminPort);
    s.adminPort = null;
  }
  s.exposable = false;
  if (s.configPath) {
    await rm(s.configPath, { force: true }).catch(() => {});
    s.configPath = null;
  }
  // Remove staged resource files (env resources may hold secrets), like the config.
  for (const path of s.stagedResources) {
    await rm(path, { force: true }).catch(() => {});
  }
  s.stagedResources = [];
  s.declaredResources = [];
  return statusOf(s);
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
 * Replay the namespace's buffer and then follow it, as one stream.
 *
 * The subscription is taken BEFORE the snapshot, deliberately. Both happen in one tick
 * today, so nothing can arrive between them — but ordering it this way means that if
 * anything ever does, the line is delivered twice rather than lost, and the sequence
 * cursor below drops the duplicate. A dropped log line is invisible; a repeated one is
 * not.
 *
 * The cursor does double duty: it honours `fromSeq` so an SSE reconnect does not replay
 * what the client already showed, and it makes that handover safe.
 */
async function* followLogs(ns: string, opts: LogStreamOptions): AsyncGenerator<LogLine> {
  let lastSeq = opts.fromSeq ?? -1;

  const pending: LogLine[] = [];
  let wake: (() => void) | null = null;
  const nudge = () => {
    const resume = wake;
    wake = null;
    resume?.();
  };

  const unsubscribe = subscribe(ns, (line) => {
    pending.push(line);
    nudge();
  });
  // An abort has to wake the loop as well as end it: without this the generator would
  // stay parked on the promise below, holding its subscription, until a line happened to
  // arrive for a client that has already gone.
  opts.signal.addEventListener("abort", nudge, { once: true });

  try {
    for (const line of snapshot(ns)) {
      if (line.seq <= lastSeq) continue;
      lastSeq = line.seq;
      yield line;
    }
    while (!opts.signal.aborted) {
      const line = pending.shift();
      if (line === undefined) {
        await new Promise<void>((resolve) => {
          wake = resolve;
        });
        continue;
      }
      if (line.seq <= lastSeq) continue;
      lastSeq = line.seq;
      yield line;
    }
  } finally {
    opts.signal.removeEventListener("abort", nudge);
    unsubscribe();
  }
}

/**
 * The local backend as an {@link AppRunner}. A thin adapter over the module functions
 * above rather than a reimplementation: those functions are the API every existing
 * caller uses, and having two code paths to the same child process is how the two start
 * to disagree.
 */
export const localRunner: AppRunner = {
  status: async (key: RunKey) => status(key.namespace),
  start: (key, args) =>
    start(key.namespace, args.yaml, args.devEnv, { resources: args.resources }),
  stop: (key) => stop(key.namespace),
  sync: (key, args) => sync(key.namespace, args.yaml, { resources: args.resources }),
  logs: (key, opts) => followLogs(key.namespace, opts),
};

/** Best-effort: don't leave any runner orphaned when the editor process exits. */
function ensureKillOnExit(): void {
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
