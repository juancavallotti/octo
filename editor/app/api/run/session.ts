import { spawn, type ChildProcess } from "node:child_process";
import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import type { Readable } from "node:stream";
import { cachedVersion } from "./version";

/**
 * Server-side singleton that owns the running `octo` process for the editor's dev
 * RUN feature. It renders nothing itself: the editor POSTs YAML, this spawns
 * `octo run -config <file> -watch`, captures stdout/stderr as log lines, and lets
 * SSE clients replay the buffer and subscribe to new lines. Editing the document
 * re-writes the same config file so the runner hot-reloads.
 *
 * State lives on `globalThis` so it survives Next's dev HMR module reloads (a new
 * module instance would otherwise lose track of the child process).
 */

/** Largest config inputs are tiny; this cap just bounds the in-memory log buffer. */
const MAX_LOG_LINES = 5000;
/** Grace period before a stop escalates from SIGTERM to SIGKILL. */
const STOP_GRACE_MS = 3000;

export interface LogLine {
  /** Monotonic id, used as the SSE event id so clients can order/resume. */
  seq: number;
  text: string;
}

export interface RunStatus {
  /** Whether a runner binary is configured (OCTO_BIN_PATH set by `task dev`). */
  available: boolean;
  running: boolean;
  /** The runner's `--version` line, probed once; null until known/if unavailable. */
  version: string | null;
}

type Listener = (line: LogLine) => void;

interface Session {
  proc: ChildProcess | null;
  /** Resolves when the current process has fully exited (used by stop/restart). */
  exit: Promise<void> | null;
  configPath: string | null;
  logs: LogLine[];
  seq: number;
  listeners: Set<Listener>;
}

const store = globalThis as unknown as {
  __octoRunSession?: Session;
  __octoRunKillHook?: boolean;
};

function session(): Session {
  if (!store.__octoRunSession) {
    store.__octoRunSession = {
      proc: null,
      exit: null,
      configPath: null,
      logs: [],
      seq: 0,
      listeners: new Set(),
    };
  }
  return store.__octoRunSession;
}

function runDir(): string {
  return process.env.OCTO_RUN_DIR || tmpdir();
}

function statusOf(s: Session): RunStatus {
  return {
    available: !!process.env.OCTO_BIN_PATH,
    running: s.proc !== null,
    version: cachedVersion(),
  };
}

export function status(): RunStatus {
  return statusOf(session());
}

/** The config file the running generation is watching (for tests/inspection). */
export function currentConfigPath(): string | null {
  return session().configPath;
}

function pushLine(text: string): void {
  const s = session();
  const line: LogLine = { seq: s.seq++, text };
  s.logs.push(line);
  if (s.logs.length > MAX_LOG_LINES) {
    s.logs.splice(0, s.logs.length - MAX_LOG_LINES);
  }
  for (const listener of s.listeners) {
    try {
      listener(line);
    } catch {
      // A listener whose stream has closed is harmless; it unsubscribes on cancel.
    }
  }
}

/** Split a stream into lines and push each, holding any partial trailing line. */
function pipeLines(stream: Readable | null): void {
  if (!stream) return;
  let buffer = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk: string) => {
    buffer += chunk;
    let nl: number;
    while ((nl = buffer.indexOf("\n")) >= 0) {
      pushLine(buffer.slice(0, nl).replace(/\r$/, ""));
      buffer = buffer.slice(nl + 1);
    }
  });
  stream.on("end", () => {
    if (buffer !== "") pushLine(buffer.replace(/\r$/, ""));
    buffer = "";
  });
}

/** Atomic write (write sibling temp + rename) so `octo`'s dir watcher sees one event. */
async function writeConfig(path: string, yaml: string): Promise<void> {
  const tmp = `${path}.tmp-${randomUUID()}`;
  await writeFile(tmp, yaml, "utf8");
  await rename(tmp, path);
}

/** Start (or restart) the runner with the given rendered config YAML. */
export async function start(yaml: string): Promise<RunStatus> {
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }

  await stop(); // tear down any previous generation first

  const s = session();
  s.logs = []; // fresh buffer per run; seq stays monotonic so clients still dedupe

  const dir = runDir();
  await mkdir(dir, { recursive: true });
  const configPath = join(dir, `octo-editor-${randomUUID()}.yaml`);
  await writeConfig(configPath, yaml);
  s.configPath = configPath;

  pushLine(`▶ starting octo — ${configPath}`);
  const proc = spawn(bin, ["run", "-config", configPath, "-watch"], {
    stdio: ["ignore", "pipe", "pipe"],
  });
  s.proc = proc;
  pipeLines(proc.stdout);
  pipeLines(proc.stderr);

  s.exit = new Promise<void>((resolve) => {
    const finish = () => {
      if (s.proc === proc) s.proc = null;
      resolve();
    };
    proc.on("error", (err) => {
      pushLine(`✖ failed to start runner: ${err.message}`);
      finish();
    });
    // Resolve on "exit" (process gone) rather than "close" (stdio EOF) so stop()
    // stays responsive even if a child inherits and holds the output pipes.
    proc.on("exit", (code, signal) => {
      pushLine(
        `■ runner exited (${signal ? `signal ${signal}` : `code ${code ?? 0}`})`,
      );
      finish();
    });
  });

  ensureKillOnExit();
  return statusOf(s);
}

/** Re-render the config the runner is watching, triggering a hot reload. No-op if stopped. */
export async function sync(yaml: string): Promise<RunStatus> {
  const s = session();
  if (!s.proc || !s.configPath) return statusOf(s);
  await writeConfig(s.configPath, yaml);
  return statusOf(s);
}

/** Stop the runner (SIGTERM, then SIGKILL after a grace period) and remove its config. */
export async function stop(): Promise<RunStatus> {
  const s = session();
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
  if (s.configPath) {
    await rm(s.configPath, { force: true }).catch(() => {});
    s.configPath = null;
  }
  return statusOf(s);
}

/** Replay the current log buffer (oldest first). */
export function snapshot(): LogLine[] {
  return [...session().logs];
}

/** Subscribe to new log lines; returns an unsubscribe function. */
export function subscribe(fn: Listener): () => void {
  const s = session();
  s.listeners.add(fn);
  return () => s.listeners.delete(fn);
}

/** Best-effort: don't leave the runner orphaned when the editor process exits. */
function ensureKillOnExit(): void {
  if (store.__octoRunKillHook) return;
  store.__octoRunKillHook = true;
  process.once("exit", () => {
    const proc = session().proc;
    if (proc) {
      try {
        proc.kill("SIGKILL");
      } catch {
        // nothing we can do on the way out
      }
    }
  });
}
