import { app } from "electron";
import { createWriteStream, mkdirSync, type WriteStream } from "node:fs";
import path from "node:path";

/**
 * The editor server's output, kept in two places: a bounded in-memory tail and a
 * file on disk.
 *
 * The tail exists for exactly one moment — the server failed to become ready and
 * the user is looking at a splash screen that will never finish. Without it the
 * only honest thing the app could say is "it didn't start", which is useless. With
 * it, the dialog can show the last thing the server actually said, which is
 * usually the whole answer (a port conflict, a missing file, a bad vault).
 *
 * The file exists for the other moment: something misbehaved an hour ago and the
 * user wants to send it to someone.
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
  // Truncating rather than appending: this file is for the session you are in.
  // An append-forever log in userData is a disk leak nobody ever notices.
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
