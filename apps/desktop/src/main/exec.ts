import { spawn } from "node:child_process";

/**
 * Running a CLI the shell depends on — docker, octopaas — with an argument vector,
 * its output handed over a line at a time as it arrives and kept for the result.
 */

export interface ExecResult {
  code: number;
  stdout: string;
  stderr: string;
}

export interface ExecOptions {
  /**
   * Each output line as it arrives, with the stream it came on. CLIs report progress
   * on stderr and data on stdout, and a caller showing both needs them apart.
   */
  onLine?: (line: string, stream: "stdout" | "stderr") => void;
  signal?: AbortSignal;
  /** The environment to run in. Absent means ours. */
  env?: NodeJS.ProcessEnv;
  /** Written to the process's stdin, which is then closed. */
  input?: string;
}

/**
 * How much of a streamed command's output is kept for its result. A streamed command
 * has already handed every line to its caller, and a follow streams for as long as
 * the window follows it; its result only needs the tail, for the error.
 */
const STREAMED_TAIL = 64 * 1024;

/** Run `file` with `args`. Never a shell string, so no argument is read as syntax. */
export function exec(file: string, args: string[], opts: ExecOptions = {}): Promise<ExecResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(file, args, { env: opts.env ?? process.env, signal: opts.signal });
    const kept = { stdout: "", stderr: "" };
    const flushes: Array<() => void> = [];
    const lines = (stream: "stdout" | "stderr") => {
      let partial = "";
      flushes.push(() => {
        if (partial) opts.onLine?.(partial, stream);
      });
      return (chunk: Buffer) => {
        const text = chunk.toString("utf8");
        kept[stream] += text;
        if (!opts.onLine) return;
        if (kept[stream].length > STREAMED_TAIL) kept[stream] = kept[stream].slice(-STREAMED_TAIL);
        const parts = (partial + text).split(/\r?\n|\r/);
        partial = parts.pop() ?? "";
        for (const line of parts) opts.onLine(line, stream);
      };
    };
    child.stdout.on("data", lines("stdout"));
    child.stderr.on("data", lines("stderr"));
    child.once("error", reject);
    child.once("close", (code) => {
      // A last line with no newline after it is still a line.
      for (const flush of flushes) flush();
      resolve({ code: code ?? 1, stdout: kept.stdout, stderr: kept.stderr });
    });
    if (opts.input !== undefined) child.stdin.end(opts.input);
    else child.stdin.end();
  });
}
