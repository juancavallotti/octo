import { app } from "electron";
import { createWriteStream, mkdirSync, type WriteStream } from "node:fs";
import path from "node:path";

/**
 * The editor server's output, kept in two places. The bounded in-memory tail is what
 * a failure dialog quotes, so a server that never came up can say why; the file on
 * disk is what a user sends on after the fact.
 */

const TAIL_LINES = 200;

let tail: string[] = [];
let stream: WriteStream | null = null;

function logFile(): string {
  const dir = app.getPath("logs");
  mkdirSync(dir, { recursive: true });
  return path.join(dir, "server.log");
}

/** Start a fresh log for a newly-spawned server. */
export function openLog(): void {
  tail = [];
  stream?.end();
  // Truncating rather than appending: the file covers one session, and an
  // append-forever log in userData grows unwatched.
  stream = createWriteStream(logFile(), { flags: "w" });
}

/** Record a chunk of server output. */
export function append(chunk: string): void {
  stream?.write(chunk);
  for (const line of chunk.split("\n")) {
    if (line.trim() === "") continue;
    tail.push(line);
  }
  if (tail.length > TAIL_LINES) tail = tail.slice(-TAIL_LINES);
}

/** The last `n` lines the server printed, oldest first. */
export function recent(n = 20): string[] {
  return tail.slice(-n);
}

/** Where the log is, for the "Show Logs" affordance. */
export function logPath(): string {
  return logFile();
}

export function closeLog(): void {
  stream?.end();
  stream = null;
}
