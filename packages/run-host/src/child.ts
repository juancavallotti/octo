import type { ChildProcess } from "node:child_process";

/**
 * The bits every spawned runner needs, in one place: which binary to run, how to end a
 * child that has outstayed its budget, and how to turn captured output into lines.
 *
 * These were four copies of the same few lines across the long-running runner, `invoke`,
 * `eval` and the test runner. The escalation in particular is worth having once — a
 * SIGTERM that is never followed by a SIGKILL leaves a wedged `octo` holding a port, and
 * a SIGKILL with no grace period truncates the logs the user is reading.
 */

/** Grace period before a stop escalates from SIGTERM to SIGKILL. */
export const STOP_GRACE_MS = 3000;

/**
 * The `octo` binary, or a thrown error naming how to get one. Thrown rather than
 * returned as null because every caller's only sensible response is to stop, and the
 * message is the one thing that tells a developer what to do about it.
 */
export function octoBin(): string {
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }
  return bin;
}

/**
 * The `dolphin` binary, under a variable of its own: either binary can be missing
 * without the other, and a host may run flows while reporting testing as unavailable.
 */
export function dolphinBin(): string {
  const bin = process.env.DOLPHIN_BIN_PATH;
  if (!bin) {
    throw new Error("DOLPHIN_BIN_PATH is not set; launch the editor with `task dev`.");
  }
  return bin;
}

/**
 * Ask a child to exit, and make sure it does: SIGTERM now, SIGKILL after the grace
 * period. Returns a function that cancels the escalation, which the caller invokes once
 * the child is gone so a stopped process leaves no pending timer behind.
 */
export function terminate(proc: ChildProcess): () => void {
  proc.kill("SIGTERM");
  const force = setTimeout(() => {
    try {
      proc.kill("SIGKILL");
    } catch {
      // already gone
    }
  }, STOP_GRACE_MS);
  return () => clearTimeout(force);
}

/**
 * Split captured output into lines, dropping a trailing empty line and CRs.
 *
 * Blank lines in the middle are kept: a runner's stderr is structured log output, and a
 * blank line in it is something it printed. `nonEmptyLines` in exec/test.ts answers the
 * other question and is deliberately a different function.
 */
export function splitLines(text: string): string[] {
  if (text === "") return [];
  return text
    .replace(/\n$/, "")
    .split("\n")
    .map((l) => l.replace(/\r$/, ""));
}
