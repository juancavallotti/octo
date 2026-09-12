import { app } from "electron";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdirSync } from "node:fs";
import { append, closeLog, openLog, recent } from "./log";
import { binary, nodeExecutable, runDir, serverDir, serverEntry } from "./paths";

/**
 * The editor server as a child process.
 *
 * It is the *same* Next standalone server the Docker image runs, spawned with the
 * same environment contract (OCTO_FS_DIR, OCTO_BIN_PATH, DOLPHIN_BIN_PATH,
 * OCTO_RUN_DIR). That equivalence is the point of the whole design: there is no
 * desktop build of the editor to keep in step, and a bug reproduced in Docker is
 * the same bug here.
 *
 * It runs on Electron's own Node (ELECTRON_RUN_AS_NODE), which is why nothing
 * bundles a second Node binary — one fewer thing to ship, sign, and keep aligned
 * with the Next version. *Which* of Electron's executables it runs on is not a
 * detail: see nodeExecutable() in paths.ts.
 */

/** How long to wait for the server to answer /api/health before giving up. */
const READY_TIMEOUT_MS = 30_000;
const POLL_INTERVAL_MS = 100;
/**
 * Ceiling on a single health probe.
 *
 * Without one, a server that accepts the connection and then never answers parks
 * the poll loop inside the await forever: the 30s deadline is only re-checked
 * between probes, so it never fires and the user sits on the splash screen with no
 * error and no way forward.
 */
const PROBE_TIMEOUT_MS = 2000;

/** Grace period before a stop escalates to SIGKILL — the runner's own STOP_GRACE_MS. */
const STOP_GRACE_MS = 3000;

export interface RunningServer {
  url: string;
  port: number;
  vault: string;
  pid: number;
}

let child: ChildProcess | null = null;
let running: RunningServer | null = null;
/** Set while stop() is in flight, so the exit handler knows this death was ours. */
let stopping = false;
/**
 * Whether the current child ever reached readiness.
 *
 * A child that dies before it is ready is start()'s failure to report, not a crash:
 * without this, a server that exited during startup told the user twice — once via
 * the crash dialog (which quits the app) and once via start()'s own error — and the
 * quit made vault.ts's "fall back to the previous folder" recovery unreachable.
 */
let wasReady = false;
/** Notified when the server dies on its own — a crash, not a stop. */
let onCrash: (() => void) | null = null;

export function current(): RunningServer | null {
  return running;
}

export function onServerCrash(fn: () => void): void {
  onCrash = fn;
}

/** The environment the child runs with — the Docker image's contract, verbatim. */
function childEnv(vault: string, port: number): NodeJS.ProcessEnv {
  return {
    ...process.env,
    // Run the Electron binary as plain Node rather than as an app.
    ELECTRON_RUN_AS_NODE: "1",
    NODE_ENV: "production",
    // Loopback only. This server has no auth by design (it is the standalone
    // app), so binding it to anything reachable off-box would publish the user's
    // filesystem to their network.
    HOSTNAME: "127.0.0.1",
    PORT: String(port),
    // A packaged desktop app must not phone home on the user's behalf.
    NEXT_TELEMETRY_DISABLED: "1",
    OCTO_FS_DIR: vault,
    OCTO_BIN_PATH: binary("octo"),
    DOLPHIN_BIN_PATH: binary("dolphin"),
    OCTO_RUN_DIR: runDir(),
  };
}

/** Whether the server is answering yet. */
async function healthy(url: string): Promise<boolean> {
  try {
    // The route sets no-store itself; nothing here should be cached anyway.
    const res = await fetch(`${url}/api/health`, {
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/**
 * Poll until the server answers, it dies, or the ceiling is reached.
 *
 * Probing /api/health rather than / is deliberate: / renders the editor page,
 * which probes the runtime's capability schema by exec'ing `octo schema`. That
 * would make readiness depend on the runner working, so a missing binary would
 * present as a server that never came up.
 */
async function waitForReady(url: string): Promise<void> {
  const deadline = Date.now() + READY_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (child === null || child.exitCode !== null) {
      throw new Error(`The editor server exited before it was ready.\n\n${recent(12).join("\n")}`);
    }
    if (await healthy(url)) return;
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }
  throw new Error(
    `The editor server did not answer within ${READY_TIMEOUT_MS / 1000}s.\n\n${recent(12).join("\n")}`,
  );
}

/** Start the server on `vault` at `port`, and resolve once it is answering. */
export async function start(vault: string, port: number): Promise<RunningServer> {
  if (child) throw new Error("the editor server is already running");
  mkdirSync(runDir(), { recursive: true });
  openLog();

  const url = `http://127.0.0.1:${port}`;
  wasReady = false;
  child = spawn(nodeExecutable(), [serverEntry()], {
    cwd: serverDir(),
    env: childEnv(vault, port),
    stdio: ["ignore", "pipe", "pipe"],
    // Its own process group, so stopping the server also reaps any `octo` it
    // spawned. Next converts SIGTERM into process.exit, which does run the app's
    // own kill hook — this is the belt to that braces.
    detached: process.platform !== "win32",
  });

  child.stdout?.on("data", (d: Buffer) => append(d.toString()));
  child.stderr?.on("data", (d: Buffer) => append(d.toString()));
  child.on("exit", (code, signal) => {
    append(`\n[desktop] editor server exited (code=${code} signal=${signal})\n`);
    child = null;
    running = null;
    // Only a server that had been running can crash; one that never came up is
    // start()'s to report.
    const crashed = !stopping && wasReady;
    wasReady = false;
    if (crashed) onCrash?.();
  });

  // Read before the await: the exit handler sets `child` to null, and TypeScript
  // cannot see that assignment — so reading child.pid afterwards is a null
  // dereference on any server that answers once and dies immediately.
  const pid = child.pid ?? -1;

  try {
    await waitForReady(url);
  } catch (err) {
    await stop();
    throw err;
  }

  wasReady = true;
  running = { url, port, vault, pid };
  return running;
}

/** Ask the child's whole process group to end, then make sure it did. */
function signalGroup(pid: number, sig: NodeJS.Signals): void {
  try {
    if (process.platform === "win32") process.kill(pid, sig);
    // Negative pid targets the group, which is why the child was spawned detached.
    else process.kill(-pid, sig);
  } catch {
    // Already gone, or never had a group. Either way there is nothing to signal.
  }
}

/** Stop the server, escalating to SIGKILL if it does not go quietly. */
export async function stop(): Promise<void> {
  const proc = child;
  if (!proc) return;
  stopping = true;
  const pid = proc.pid;
  const ended = new Promise<void>((resolve) => proc.once("exit", () => resolve()));

  if (pid) signalGroup(pid, "SIGTERM");
  const force = setTimeout(() => pid && signalGroup(pid, "SIGKILL"), STOP_GRACE_MS);
  await ended;
  clearTimeout(force);

  stopping = false;
  wasReady = false;
  child = null;
  running = null;
  closeLog();
}

// A crashed or force-quit app must not leave a server holding the port.
app.on("will-quit", () => {
  const pid = child?.pid;
  if (pid) signalGroup(pid, "SIGKILL");
});
