import { app, type WebContents } from "electron";
import {
  accessSync,
  chmodSync,
  constants,
  mkdirSync,
  readlinkSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { spawn, type IPty } from "node-pty";
import { current } from "./server";
import { binary } from "./settings";
import { launch, shellTitle, startupFiles } from "./terminalEnv";

/**
 * The editor's terminal sessions: one pty per shell, owned by the page that opened
 * it. A session belongs to that page rather than to the app because nothing else can
 * draw it or type into it — when the page reloads or closes, its shells end with it.
 *
 * Shells start in the open folder. Opening another folder reloads the page, so it
 * ends them too.
 */

interface Session {
  pty: IPty;
  owner: WebContents;
  /** Output not yet sent, coalesced so a chatty command is not one IPC per write. */
  pending: string;
  timer: ReturnType<typeof setTimeout> | null;
}

/** How long output may wait to be batched with what follows it. */
const FLUSH_MS = 5;

const sessions = new Map<string, Session>();
const watched = new WeakSet<WebContents>();
let nextId = 1;

/**
 * Links to the binaries the editor runs, put first on each shell's PATH. Links rather
 * than the bundle's own bin/, because Settings can point the editor at another binary
 * and the terminal has to run the same one; checked per session for that reason.
 *
 * A changed link is replaced by renaming a new one over it, never by removing it
 * first: shells already running resolve `octo` through it, and must not find it gone.
 */
function shimDir(): string {
  const dir = path.join(app.getPath("userData"), "term-bin");
  mkdirSync(dir, { recursive: true });
  for (const name of ["octo", "dolphin"] as const) {
    const link = path.join(dir, name);
    const target = binary(name);
    let existing: string | null = null;
    try {
      existing = readlinkSync(link);
    } catch {
      // Not there yet.
    }
    if (existing === target) continue;
    const fresh = `${link}.${process.pid}.new`;
    rmSync(fresh, { force: true });
    symlinkSync(target, fresh);
    renameSync(fresh, link);
  }
  return dir;
}

let startupWritten = false;

/** The startup files zsh and bash are pointed at; written once per run of the app. */
function startupDir(): string {
  const dir = path.join(app.getPath("userData"), "term-shell");
  if (startupWritten) return dir;
  for (const [file, content] of Object.entries(startupFiles(dir))) {
    mkdirSync(path.dirname(path.join(dir, file)), { recursive: true });
    writeFileSync(path.join(dir, file), content);
  }
  startupWritten = true;
  return dir;
}

/**
 * node-pty 1.1.0 publishes its macOS spawn-helper without the execute bit, and every
 * spawn fails with "posix_spawnp failed" until it has one. The packaged app gets it
 * from build/after-pack.cjs, before signing; this is the checkout's copy.
 */
function ensureSpawnHelper(): void {
  if (app.isPackaged || process.platform !== "darwin") return;
  const dir = path.dirname(require.resolve("node-pty/package.json"));
  const helper = path.join(dir, "prebuilds", `darwin-${process.arch}`, "spawn-helper");
  try {
    accessSync(helper, constants.X_OK);
  } catch {
    chmodSync(helper, 0o755);
  }
}

function flush(id: string, session: Session): void {
  session.timer = null;
  if (!session.pending || session.owner.isDestroyed()) return;
  session.owner.send("octo:term:data", id, session.pending);
  session.pending = "";
}

/** End every session a page owns, once it can no longer show them. */
function watch(owner: WebContents): void {
  if (watched.has(owner)) return;
  watched.add(owner);
  const end = () => {
    for (const [id, session] of sessions) if (session.owner === owner) kill(id);
  };
  owner.on("destroyed", end);
  // A reload or a navigation is a new page, which knows nothing of these sessions.
  owner.on("did-navigate", end);
}

export function open(owner: WebContents, cols: number, rows: number): { id: string; title: string } {
  ensureSpawnHelper();
  const { file, args, env } = launch(process.env, {
    shimDir: shimDir(),
    startupDir: startupDir(),
    version: app.getVersion(),
  });
  const pty = spawn(file, args, {
    name: "xterm-256color",
    cols,
    rows,
    cwd: current()?.vault ?? homedir(),
    env,
  });

  const id = `term-${nextId++}`;
  const session: Session = { pty, owner, pending: "", timer: null };
  sessions.set(id, session);
  watch(owner);

  pty.onData((chunk) => {
    session.pending += chunk;
    session.timer ??= setTimeout(() => flush(id, session), FLUSH_MS);
  });
  pty.onExit(({ exitCode }) => {
    if (session.timer) clearTimeout(session.timer);
    flush(id, session);
    sessions.delete(id);
    if (!owner.isDestroyed()) owner.send("octo:term:exit", id, exitCode);
  });

  return { id, title: shellTitle(file) };
}

/** The session `id`, if `owner` is the page that opened it. */
function owned(owner: WebContents, id: unknown): Session | undefined {
  const session = typeof id === "string" ? sessions.get(id) : undefined;
  return session?.owner === owner ? session : undefined;
}

export function write(owner: WebContents, id: unknown, data: unknown): void {
  if (typeof data === "string") owned(owner, id)?.pty.write(data);
}

export function resize(owner: WebContents, id: unknown, cols: unknown, rows: unknown): void {
  const valid = (n: unknown): n is number => Number.isInteger(n) && (n as number) > 0;
  if (valid(cols) && valid(rows)) owned(owner, id)?.pty.resize(cols, rows);
}

export function close(owner: WebContents, id: unknown): void {
  if (owned(owner, id)) kill(id as string);
}

function kill(id: string): void {
  const session = sessions.get(id);
  if (!session) return;
  try {
    session.pty.kill();
  } catch {
    // Already gone; its exit is on the way or has been reported.
  }
}

/** End every shell: the app is quitting and nothing will be left to show them. */
export function closeAll(): void {
  for (const id of [...sessions.keys()]) kill(id);
}
