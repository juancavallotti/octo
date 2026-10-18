import { execFile, spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";

/**
 * The user's own `docker` CLI: where it is, whether its daemon answers, and one way to
 * run it. Everything container-related goes through the CLI rather than the Engine API
 * so that whatever the user already set up — Docker Desktop, OrbStack, Colima, a
 * remote context, `docker login` credentials — is what we use, with nothing to
 * configure twice.
 */

/** What the container features can rely on right now. */
export type DockerState = "missing" | "stopped" | "running";

export interface DockerStatus {
  state: DockerState;
  /** The CLI in use, when one was found. */
  path?: string;
  client?: string;
  server?: string;
  /** Why the daemon did not answer, in the CLI's own words. */
  error?: string;
}

/**
 * Where docker lives on machines that have it. An app launched from the Finder or the
 * Dock inherits launchd's PATH, which has none of these, so a docker the user's
 * terminal finds would be invisible to us without them.
 */
export function installDirs(home = homedir()): string[] {
  return [
    "/usr/local/bin",
    "/opt/homebrew/bin",
    path.join(home, ".orbstack", "bin"),
    path.join(home, ".docker", "bin"),
    path.join(home, ".rd", "bin"),
    "/Applications/Docker.app/Contents/Resources/bin",
  ];
}

/** The PATH docker runs with: the inherited one, then the install dirs it lacks. */
export function searchPath(inherited = process.env.PATH ?? "", home = homedir()): string {
  const dirs = inherited.split(path.delimiter).filter(Boolean);
  for (const dir of installDirs(home)) if (!dirs.includes(dir)) dirs.push(dir);
  return dirs.join(path.delimiter);
}

/**
 * The docker CLI to run: the override from Settings when it is there, else the first
 * `docker` on the search path. An override pointing at a missing file is not
 * silently replaced — the user chose it, and Settings says it is missing.
 */
export function locate(
  override: string | undefined,
  exists: (file: string) => boolean = existsSync,
  pathVar = searchPath(),
): string | null {
  if (override) return exists(override) ? override : null;
  const name = process.platform === "win32" ? "docker.exe" : "docker";
  for (const dir of pathVar.split(path.delimiter)) {
    const file = path.join(dir, name);
    if (dir && exists(file)) return file;
  }
  return null;
}

/**
 * Read `docker version --format '{{json .}}'`. The CLI prints its own half even when
 * the daemon is down, with `Server` null, and exits non-zero — so the answer comes
 * from the output, not the exit code.
 */
export function parseVersion(stdout: string, stderr = ""): Omit<DockerStatus, "path"> {
  let parsed: { Client?: { Version?: string }; Server?: { Version?: string } | null } | null;
  try {
    parsed = JSON.parse(stdout.trim().split("\n")[0] ?? "") as typeof parsed;
  } catch {
    parsed = null;
  }
  const client = parsed?.Client?.Version;
  const server = parsed?.Server?.Version;
  if (server) return { state: "running", client, server };
  const error = stderr.trim().split("\n").filter(Boolean).pop();
  return { state: "stopped", ...(client ? { client } : {}), ...(error ? { error } : {}) };
}

/** The environment docker runs in: ours, with a PATH its credential helpers resolve on. */
export function dockerEnv(): NodeJS.ProcessEnv {
  return { ...process.env, PATH: searchPath() };
}

/**
 * Ask the CLI found at `file` whether its daemon answers. Bounded, because a context
 * pointing at an unreachable host makes docker wait a long time before saying so,
 * and the menu is waiting on this.
 */
export function probe(file: string | null, timeoutMs = 5000): Promise<DockerStatus> {
  if (!file) return Promise.resolve({ state: "missing" });
  return new Promise((resolve) => {
    execFile(
      file,
      ["version", "--format", "{{json .}}"],
      { timeout: timeoutMs, env: dockerEnv() },
      (err, stdout, stderr) => {
        // Not executable at all is the same answer as not being there.
        if (err && (err as NodeJS.ErrnoException).code === "ENOENT") {
          return resolve({ state: "missing" });
        }
        resolve({ path: file, ...parseVersion(String(stdout), String(stderr || err?.message || "")) });
      },
    );
  });
}

export interface ExecResult {
  code: number;
  stdout: string;
  stderr: string;
}

export interface ExecOptions {
  /** Each output line as it arrives, stdout and stderr alike — docker reports progress on both. */
  onLine?: (line: string) => void;
  signal?: AbortSignal;
}

/**
 * How much of a streamed command's output is kept for its result. A streamed command
 * has already handed every line to its caller, and `docker logs -f` streams for as
 * long as the window follows it; its result only needs the tail, for the error.
 */
const STREAMED_TAIL = 64 * 1024;

/**
 * Run docker with an argument vector. Never a shell string: image references, paths
 * and tags all come from user input, and none of them may be read as syntax.
 */
export function exec(file: string, args: string[], opts: ExecOptions = {}): Promise<ExecResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(file, args, { env: dockerEnv(), signal: opts.signal });
    const kept = { out: "", err: "" };
    const flushes: Array<() => void> = [];
    const lines = (stream: "out" | "err") => {
      let partial = "";
      flushes.push(() => {
        if (partial) opts.onLine?.(partial);
      });
      return (chunk: Buffer) => {
        const text = chunk.toString("utf8");
        kept[stream] += text;
        if (!opts.onLine) return;
        if (kept[stream].length > STREAMED_TAIL) kept[stream] = kept[stream].slice(-STREAMED_TAIL);
        const parts = (partial + text).split(/\r?\n|\r/);
        partial = parts.pop() ?? "";
        for (const line of parts) opts.onLine(line);
      };
    };
    child.stdout.on("data", lines("out"));
    child.stderr.on("data", lines("err"));
    child.once("error", reject);
    child.once("close", (code) => {
      // A last line with no newline after it is still a line.
      for (const flush of flushes) flush();
      resolve({ code: code ?? 1, stdout: kept.out, stderr: kept.err });
    });
  });
}

/** Run docker and fail with its own last words when it does. */
export async function execOk(file: string, args: string[], opts: ExecOptions = {}): Promise<string> {
  const result = await exec(file, args, opts);
  if (result.code !== 0) {
    const reason = result.stderr.trim().split("\n").filter(Boolean).pop();
    throw new Error(reason || `docker ${args[0]} exited with ${result.code}`);
  }
  return result.stdout;
}
