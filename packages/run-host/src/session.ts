import { spawn, type ChildProcess } from "node:child_process";
import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { cachedVersion } from "./version";
import { allocatePort, isExposable, releasePort } from "./ports";
import { LogBuffer, type LogLine } from "./logbuffer";
import { ensureReaper } from "./reaper";
import {
  effectiveResourceNames,
  injectDevEnvResource,
  resolveAndStage,
  sameNameSet,
  stageResources,
  type ResourceProvider,
} from "./resources";

/** Options carrying the host's resource provider through a run. */
export interface RunResourceOptions {
  /** Resolves the resource files the run's config declares; omitted → no staging. */
  resources?: ResourceProvider;
}

/**
 * Server-side manager that owns the running `octo` processes for the editor's dev
 * RUN feature. It renders nothing itself: the editor POSTs YAML, this spawns
 * `octo run -config <file> -watch`, captures stdout/stderr as log lines, and lets
 * SSE clients replay the buffer and subscribe to new lines. Editing the document
 * re-writes the same config file so the runner hot-reloads.
 *
 * Runs are keyed by a per-user namespace slug (see namespace.ts) so concurrent
 * editor users don't disturb one another: each namespace owns an independent
 * process, config file, and log buffer. State lives on `globalThis` so it survives
 * Next's dev HMR module reloads (a new module instance would otherwise lose track
 * of the child processes).
 */

/** Grace period before a stop escalates from SIGTERM to SIGKILL. */
const STOP_GRACE_MS = 3000;

/** Default wall-clock budget for a one-shot `invoke`, matching the CLI's own default. */
const INVOKE_DEFAULT_TIMEOUT_MS = 30000;

/** Extra head-room the run-host waits beyond the CLI `-timeout` before force-killing. */
const INVOKE_GRACE_MS = 5000;

/** The line `octo invoke` logs to stderr when the flow filters (drops) the message. */
const DROP_MARKER = "flow dropped the message";

/** The log levels the runtime's LOG_LEVEL understands. */
export type LogLevel = "debug" | "info" | "warn" | "error";

/**
 * The envelope `octo invoke --break-at` prints on stdout (see the CLI's breakOutcome).
 * `reached: false` is a normal outcome, not a failure: the message may simply have
 * taken a branch the addressed block is not on. `error` likewise carries a *flow*
 * failure, which is a debugging result rather than a bad request — both exit 0.
 */
export interface BreakOutcome {
  reached: boolean;
  /** The address the breakpoint was set on, echoed back. */
  block: string;
  /** The message as it looked after the addressed block ran; absent when unreached. */
  message?: unknown;
  /** The flow's own failure, when it failed. */
  error?: string;
}

/** The outcome of a one-shot {@link invoke}: stdout (the flow result) and stderr (logs). */
export interface InvokeResult {
  /** True when the runner exited 0 and wasn't force-killed for exceeding the budget. */
  ok: boolean;
  /** The runner's exit code, or null when it was terminated by a signal. */
  exitCode: number | null;
  /** True when the wall-clock backstop had to kill the runner. */
  timedOut: boolean;
  /**
   * True when the flow filtered the message (a block returned nothing), so there is no
   * result body — distinct from a flow that legitimately produced an empty `output`.
   */
  dropped: boolean;
  /** Everything the runner wrote to stdout (the flow's result body as JSON). */
  output: string;
  /** The runner's stderr, split into lines (its slog output). */
  logs: string[];
  /**
   * The decoded breakpoint envelope — present only for a `breakAt` invoke that ran far
   * enough to print one. Under `breakAt`, stdout carries this envelope *instead of* the
   * flow's result body, so `output` is the raw envelope text rather than a result.
   */
  breakpoint?: BreakOutcome;
}

export interface RunStatus {
  /** Whether a runner binary is configured (OCTO_BIN_PATH set by `task dev`). */
  available: boolean;
  running: boolean;
  /** The runner's `--version` line, probed once; null until known/if unavailable. */
  version: string | null;
  /** Whether the current run declares HTTP_PORT, i.e. is networked/testable. */
  exposable: boolean;
  /** Allocated HTTP listen port for a networked run, null otherwise. */
  port: number | null;
  /** BFF path that proxies to the running networked integration, null otherwise. */
  testPath: string | null;
}

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

/** Per-namespace directory holding that user's rendered config file, under
 * OCTO_RUN_DIR (set by `task dev`) or the system temp dir. */
function namespaceDir(ns: string): string {
  return join(process.env.OCTO_RUN_DIR || tmpdir(), ns);
}

function statusOf(s: Session): RunStatus {
  // The proxy resolves the actual port server-side, so the test path is
  // port-independent — just the namespace.
  const networked = s.proc !== null && s.exposable && s.port !== null;
  return {
    available: !!process.env.OCTO_BIN_PATH,
    running: s.proc !== null,
    version: cachedVersion(),
    exposable: s.exposable,
    port: s.port,
    testPath: networked ? `/editor/runs/${s.namespace}/` : null,
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

/** Atomic write (write sibling temp + rename) so `octo`'s dir watcher sees one event. */
async function writeConfig(path: string, yaml: string): Promise<void> {
  const tmp = `${path}.tmp-${randomUUID()}`;
  await writeFile(tmp, yaml, "utf8");
  await rename(tmp, path);
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
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }

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

  // A networked integration (one that declares HTTP_PORT) gets a real port from
  // the pool, injected as HTTP_PORT so the BFF can proxy to it. HTTP_HOST is the
  // loopback because only the same-pod proxy needs to reach it. Internal-only runs
  // (no HTTP_PORT) get no port and stay unexposed.
  const exposable = isExposable(yaml);
  const port = exposable ? allocatePort() : null;
  s.exposable = exposable;
  s.port = port;

  // Base process env, then the dev env values; HTTP_PORT/HTTP_HOST are applied
  // last so a stray dev value can't clobber the proxy wiring.
  const env: NodeJS.ProcessEnv = { ...process.env, ...(devEnv ?? {}) };
  if (port !== null) {
    env.HTTP_PORT = String(port);
    env.HTTP_HOST = "127.0.0.1";
  }

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
        // Free this generation's port when it exits on its own (crash/exit).
        if (port !== null && s.port === port) {
          releasePort(port);
          s.port = null;
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

/**
 * Run a single named flow once and return its result plus logs, without starting the
 * integration's sources. This shells out to `octo invoke`, which builds an invoke-mode
 * service, calls the flow, prints its result body to stdout, streams slog to stderr,
 * and tears down. It is deliberately session-free: it writes a throwaway config,
 * captures the child's stdout/stderr locally (kept separate — result vs. logs), and
 * never touches the namespace's long-running run or its {@link LogBuffer}/port, so a
 * concurrent `run` in the same namespace is undisturbed.
 *
 * The child's own `-timeout` bounds only the flow call; a wall-clock backstop here
 * force-kills a runner that hangs in startup or teardown so this never blocks forever.
 *
 * With `breakAt`, the runner instead halts at the addressed block and prints a
 * {@link BreakOutcome} envelope on stdout, which is decoded into `breakpoint`.
 */
export async function invoke(
  ns: string,
  yaml: string,
  flow: string,
  opts?: {
    data?: string;
    /** JSON object seeding the message variables (the CLI's `-vars`). */
    vars?: string;
    env?: Record<string, string>;
    timeoutMs?: number;
    resources?: ResourceProvider;
    /** Breakpoint address: run until this block, then stop (the CLI's `--break-at`). */
    breakAt?: string;
    /** Runner log level. Applied as LOG_LEVEL, which `env` can still override. */
    logLevel?: LogLevel;
  },
): Promise<InvokeResult> {
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }

  // A one-shot invoke gets its own subdir under the namespace so its config and
  // staged resources can't clobber a concurrent long-running run's staged files.
  // `octo invoke` roots the resource loader at the config's own dir, so staging
  // beside the config resolves the declared resources.
  const invokeDir = join(namespaceDir(ns), `invoke-${randomUUID()}`);
  await mkdir(invokeDir, { recursive: true });
  await resolveAndStage(invokeDir, yaml, opts?.resources);
  const configPath = join(invokeDir, "octo-invoke.yaml");
  await writeConfig(configPath, injectDevEnvResource(yaml));

  const timeoutMs = opts?.timeoutMs ?? INVOKE_DEFAULT_TIMEOUT_MS;
  const args = [
    "invoke",
    "-config",
    configPath,
    "-flow",
    flow,
    "-timeout",
    `${timeoutMs}ms`,
  ];
  if (opts?.data !== undefined) args.push("-data", opts.data);
  if (opts?.vars !== undefined) args.push("-vars", opts.vars);
  if (opts?.breakAt !== undefined) args.push("--break-at", opts.breakAt);

  // Base process env, then our log level, then the caller's env values. No HTTP_PORT
  // wiring — invoke is not networked; it calls the flow directly and exits. LOG_LEVEL
  // goes *before* the caller's map so an integration that declares LOG_LEVEL itself
  // still wins: a run must not silently disagree with the config it is running.
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    ...(opts?.logLevel ? { LOG_LEVEL: opts.logLevel } : {}),
    ...(opts?.env ?? {}),
  };

  const proc = spawn(bin, args, { stdio: ["ignore", "pipe", "pipe"], env });

  let output = "";
  let errText = "";
  proc.stdout?.setEncoding("utf8");
  proc.stdout?.on("data", (chunk: string) => {
    output += chunk;
  });
  proc.stderr?.setEncoding("utf8");
  proc.stderr?.on("data", (chunk: string) => {
    errText += chunk;
  });

  let timedOut = false;
  let kill: NodeJS.Timeout | undefined;
  let force: NodeJS.Timeout | undefined;
  const result = await new Promise<InvokeResult>((resolve) => {
    const finish = (exitCode: number | null) => {
      if (kill) clearTimeout(kill);
      if (force) clearTimeout(force);
      const logs = splitLines(errText);
      let ok = exitCode === 0 && !timedOut;

      // Under --break-at the runner prints an envelope instead of a result body. It
      // only does so when it got far enough to run the flow: a bad address or an
      // unloadable config exits non-zero with nothing on stdout, which stays a
      // failure here rather than becoming a silent "not reached".
      let breakpoint: BreakOutcome | undefined;
      if (ok && opts?.breakAt !== undefined) {
        breakpoint = parseBreakOutcome(output);
        if (!breakpoint) {
          ok = false;
          logs.push(`✖ could not parse the breakpoint envelope: ${output.trim()}`);
        }
      }

      resolve({
        ok,
        exitCode,
        timedOut,
        dropped: ok && logs.some((l) => l.includes(DROP_MARKER)),
        output,
        logs,
        breakpoint,
      });
    };
    // Wall-clock backstop: give the child its `-timeout` plus head-room, then escalate
    // SIGTERM → SIGKILL exactly like stop() does.
    kill = setTimeout(() => {
      timedOut = true;
      proc.kill("SIGTERM");
      force = setTimeout(() => {
        try {
          proc.kill("SIGKILL");
        } catch {
          // already gone
        }
      }, STOP_GRACE_MS);
    }, timeoutMs + INVOKE_GRACE_MS);
    proc.on("error", (err) => {
      errText += `✖ failed to start runner: ${err.message}\n`;
      finish(null);
    });
    proc.on("exit", (code) => finish(code));
  });

  await rm(invokeDir, { recursive: true, force: true }).catch(() => {});
  return result;
}

/**
 * Decode the envelope `--break-at` prints, or undefined when stdout does not hold one.
 * The runner prints it as a single JSON line, but slog output can only reach stderr, so
 * the last non-blank stdout line is the envelope. `reached` is what identifies it: a
 * plain result body would not carry that key.
 */
function parseBreakOutcome(stdout: string): BreakOutcome | undefined {
  const line = splitLines(stdout).filter((l) => l.trim() !== "").pop();
  if (!line) return undefined;
  try {
    const parsed: unknown = JSON.parse(line);
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      typeof (parsed as BreakOutcome).reached !== "boolean"
    ) {
      return undefined;
    }
    return parsed as BreakOutcome;
  } catch {
    return undefined;
  }
}

/** Default wall-clock budget for a one-shot eval; CEL evaluation is instant, so a
 * tight backstop just guards against a runner that hangs in startup. */
const EVAL_DEFAULT_TIMEOUT_MS = 10000;

/** The outcome of an `octo eval`: the parsed result or the compile/eval error, plus logs. */
export interface EvalResult {
  /** True when the runner produced a well-formed envelope (whatever the CEL outcome). */
  ok: boolean;
  /** The JSON-native value the expression produced, when it evaluated successfully. */
  result?: unknown;
  /** The compile or evaluation error message, when the expression (or the runner) failed. */
  error?: string;
  /** The runner's stderr, split into lines (its slog output / crash detail). */
  logs: string[];
}

/**
 * Evaluate a single CEL message expression against an ad-hoc object, without running a
 * flow. This shells out to `octo eval`, which compiles the expression through the
 * runtime's message-CEL seam and prints a JSON envelope on stdout. It is stateless — no
 * namespace, config, or resource staging — so, unlike {@link invoke}, it never touches a
 * namespace's long-running run and needs no cleanup.
 *
 * `ok` reflects whether a well-formed envelope came back. A CEL compile/eval failure is a
 * normal result surfaced as `{ ok:false, error }` by the CLI; the runner failing to run
 * at all (non-zero exit, unparseable output, missing binary) is also `ok:false`, with the
 * reason in `error`/`logs`. `opts.data` binds the object to `body`, `opts.vars` to `vars`,
 * and `opts.env` to the CEL `env` map.
 */
export async function evalCel(
  expression: string,
  opts?: {
    data?: string;
    vars?: string;
    env?: Record<string, string>;
    timeoutMs?: number;
  },
): Promise<EvalResult> {
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }

  const args = ["eval", "-expr", expression];
  if (opts?.data !== undefined) args.push("-data", opts.data);
  if (opts?.vars !== undefined) args.push("-vars", opts.vars);
  // The CEL `env` map is passed as a value, not injected into the child's process env:
  // standalone eval has no config to resolve env from, so the caller supplies it directly.
  if (opts?.env !== undefined) args.push("-env", JSON.stringify(opts.env));

  const timeoutMs = opts?.timeoutMs ?? EVAL_DEFAULT_TIMEOUT_MS;
  const proc = spawn(bin, args, { stdio: ["ignore", "pipe", "pipe"], env: process.env });

  let output = "";
  let errText = "";
  proc.stdout?.setEncoding("utf8");
  proc.stdout?.on("data", (chunk: string) => {
    output += chunk;
  });
  proc.stderr?.setEncoding("utf8");
  proc.stderr?.on("data", (chunk: string) => {
    errText += chunk;
  });

  let timedOut = false;
  let kill: NodeJS.Timeout | undefined;
  let force: NodeJS.Timeout | undefined;
  return new Promise<EvalResult>((resolve) => {
    const finish = (exitCode: number | null) => {
      if (kill) clearTimeout(kill);
      if (force) clearTimeout(force);
      const logs = splitLines(errText);
      if (timedOut) {
        resolve({ ok: false, error: `eval timed out after ${timeoutMs}ms`, logs });
        return;
      }
      if (exitCode !== 0) {
        // A usage error (missing/invalid flags) or a crash: the CLI logs the reason to
        // stderr, so surface the last log line as the error when there is one.
        const reason = logs.length > 0 ? logs[logs.length - 1] : `runner exited ${exitCode}`;
        resolve({ ok: false, error: reason, logs });
        return;
      }
      try {
        const env = JSON.parse(output) as { ok: boolean; result?: unknown; error?: string };
        resolve({ ok: env.ok, result: env.result, error: env.error, logs });
      } catch {
        resolve({ ok: false, error: `unexpected eval output: ${output.trim()}`, logs });
      }
    };
    kill = setTimeout(() => {
      timedOut = true;
      proc.kill("SIGTERM");
      force = setTimeout(() => {
        try {
          proc.kill("SIGKILL");
        } catch {
          // already gone
        }
      }, STOP_GRACE_MS);
    }, timeoutMs);
    proc.on("error", (err) => {
      errText += `✖ failed to start runner: ${err.message}\n`;
      finish(null);
    });
    proc.on("exit", (code) => finish(code));
  });
}

/** Split captured stderr into lines, dropping a trailing empty line and CRs. */
function splitLines(text: string): string[] {
  if (text === "") return [];
  return text.replace(/\n$/, "").split("\n").map((l) => l.replace(/\r$/, ""));
}

/** Stop the namespace's runner (SIGTERM, then SIGKILL after a grace period) and remove its config. */
export async function stop(ns: string): Promise<RunStatus> {
  const s = session(ns);
  const proc = s.proc;
  if (proc) {
    proc.kill("SIGTERM");
    const force = setTimeout(() => {
      try {
        proc.kill("SIGKILL");
      } catch {
        // already gone
      }
    }, STOP_GRACE_MS);
    try {
      await s.exit;
    } finally {
      clearTimeout(force);
    }
  }
  s.proc = null;
  s.exit = null;
  if (s.port !== null) {
    releasePort(s.port);
    s.port = null;
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
