import { spawn } from "node:child_process";
import { octoBin, splitLines, terminate } from "../child";

/**
 * One-shot `octo eval`: evaluate a single CEL message expression against an ad-hoc
 * object, without running a flow.
 *
 * The most stateless thing in this package — no namespace, no config, no resource
 * staging, nothing to clean up. It is here beside `invoke` because it is the same kind of
 * operation (spawn, wait, report) and for the same reason: it holds nothing, so no
 * backend has to be selected for it.
 */

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
 * runtime's message-CEL seam and prints a JSON envelope on stdout.
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
  const bin = octoBin();

  const args = ["eval", "-expr", expression];
  if (opts?.data !== undefined) args.push("-data", opts.data);
  if (opts?.vars !== undefined) args.push("-vars", opts.vars);
  // The CEL `env` map is passed as a value, not injected into the child's process env:
  // standalone eval has no config to resolve env from, so the caller supplies it directly.
  //
  // These three (`-expr`, `-data`, `-vars`, `-env`) all ride the argv, which is readable
  // via /proc/<pid>/cmdline while the child runs — so treat them as CEL evaluation
  // *inputs*, not as a secret channel. That is acceptable here because eval is a one-shot
  // the trusted server spawns in its own process (a developer's machine for the standalone
  // app, the server's own pod for the platform), so its cmdline is visible only to that
  // server, which already holds these values — not to any untrusted co-tenant, who reaches
  // this only through the MCP API and never a shell. `octo eval` takes `-env` only as a
  // flag (no file/stdin), and `-data`/`-vars` share the channel regardless, so callers must
  // not pass real credentials as eval inputs; a fake value that exercises the expression is
  // the intended use.
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
  let backstop: NodeJS.Timeout | undefined;
  let cancelKill: (() => void) | undefined;
  return new Promise<EvalResult>((resolve) => {
    const finish = (exitCode: number | null) => {
      if (backstop) clearTimeout(backstop);
      cancelKill?.();
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
        const parsed: unknown = JSON.parse(output);
        // A well-formed envelope is an object with a boolean `ok`. Valid JSON of any
        // other shape — a bare scalar, or an object without `ok` — would otherwise
        // resolve as { ok: undefined }, breaking the boolean this interface promises.
        if (
          typeof parsed !== "object" ||
          parsed === null ||
          typeof (parsed as { ok?: unknown }).ok !== "boolean"
        ) {
          resolve({ ok: false, error: `unexpected eval output: ${output.trim()}`, logs });
          return;
        }
        const env = parsed as { ok: boolean; result?: unknown; error?: string };
        resolve({ ok: env.ok, result: env.result, error: env.error, logs });
      } catch {
        resolve({ ok: false, error: `unexpected eval output: ${output.trim()}`, logs });
      }
    };
    backstop = setTimeout(() => {
      timedOut = true;
      cancelKill = terminate(proc);
    }, timeoutMs);
    proc.on("error", (err) => {
      errText += `✖ failed to start runner: ${err.message}\n`;
      finish(null);
    });
    proc.on("exit", (code) => finish(code));
  });
}
