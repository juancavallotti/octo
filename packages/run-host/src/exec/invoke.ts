import { spawn } from "node:child_process";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { injectDevEnvResource, resolveAndStage, type ResourceProvider } from "../resources";
import { namespaceDir, writeConfig } from "../staging";
import { octoBin, splitLines, terminate } from "../child";

/**
 * One-shot `octo invoke`: run a single named flow and report what it produced, without
 * starting the integration's sources.
 *
 * Session-free, which is why it is here rather than beside the long-running runner. It
 * writes a throwaway config, captures the child's stdout and stderr locally — kept apart,
 * result vs. logs — and never touches a namespace's running process, its log buffer or
 * its port. A concurrent run in the same namespace is undisturbed, and nothing about this
 * depends on which replica served the request.
 */

/** Default wall-clock budget for a one-shot `invoke`, matching the CLI's own default. */
const INVOKE_DEFAULT_TIMEOUT_MS = 30000;

/** Extra head-room the run-host waits beyond the CLI `-timeout` before force-killing. */
const INVOKE_GRACE_MS = 5000;

/** The line `octo invoke` logs to stderr when the flow filters (drops) the message. */
const DROP_MARKER = "flow dropped the message";

/** The log levels the runtime's LOG_LEVEL understands. */
export type LogLevel = "debug" | "info" | "warn" | "error";

/**
 * One case of a mocked block: a CEL condition, and what the block should do when it
 * holds. Mirrors the runtime's `core.MockCase` (runtime/core/mock.go), whose rules this
 * type cannot express and the caller must respect: **exactly one** of `body`, `error`
 * and `drop` is set — a block either produces a message, fails, or filters it out — and
 * `vars` only goes alongside a `body`. The runtime rejects a spec that breaks either.
 */
export interface MockCase {
  /** CEL, evaluated against the message the block *received*: `body.amount > 100`. */
  when?: string;
  /** The body the block returns. A literal value, not an expression. */
  body?: unknown;
  /** Variables set on the message alongside `body` — for blocks reporting through one. */
  vars?: Record<string, unknown>;
  /** Fail the block with this message, so an error path can be tested. */
  error?: string;
  /** Filter the message out, as a filter block would. */
  drop?: boolean;
}

/**
 * What one addressed block is mocked with: cases tried in order, and an optional
 * default. Mirrors the runtime's `core.MockSpec`.
 *
 * With no `default`, a message matching no case **fails the block**. That is not the
 * same as falling through to the real block: the mock *replaced* it, so there is none
 * left to fall through to — keeping one wired would let a "mocked" HTTP or LLM call
 * still reach the network. The escape hatches are a `default`, or a trailing case with
 * `when: "true"`.
 */
export interface MockSpec {
  cases?: MockCase[];
  default?: MockCase;
}

/**
 * One execution of a spied block: the message that went in, and what came out —
 * including the two outcomes that are not a message. Exactly one of `output`, `dropped`
 * and `error` is set. Mirrors the runtime's `core.SpyRecord`.
 */
export interface SpyRecord {
  /** Orders records across *every* spy, not just this one: a total order for the run. */
  seq: number;
  /** RFC3339 timestamp, as the Go side marshals it. */
  at: string;
  /** The message as the block received it, captured before it ran. */
  input: unknown;
  output?: unknown;
  dropped?: boolean;
  error?: string;
}

/** What one spied block saw, in the order it saw it. */
export interface SpyTrace {
  address: string;
  /** Empty is a result, not a gap: the flow may have taken a branch the block isn't on. */
  records: SpyRecord[];
}

/**
 * The envelope `octo invoke` prints on stdout when it was asked to *observe* the run —
 * with `--break-at`, with `--spies`, or both (see the CLI's `debugOutcome` in
 * runtime/octo/debug.go). A `--mocks`-only run observes nothing and prints a plain result
 * message instead, so it produces no envelope at all.
 *
 * `reached` is optional, and that is load-bearing: it is `false` for a breakpoint that
 * never fired (a normal outcome — the message may have taken another branch) and
 * **absent** when there was no breakpoint, so a spies-only envelope is not mistaken for
 * a breakpoint that never fired. {@link parseDebugOutcome} relies on the distinction.
 *
 * `error` carries a *flow* failure, which is a debugging result rather than a bad
 * request: the runner still exits 0, and a spy will have recorded what the message was
 * carrying when it failed.
 */
export interface DebugOutcome {
  reached?: boolean;
  /** The address the breakpoint was set on, echoed back. */
  block?: string;
  /** The message as it looked after the addressed block ran; absent when unreached. */
  message?: unknown;
  /** The flow's own result, for a run that was not stopped at a breakpoint. */
  result?: unknown;
  dropped?: boolean;
  spies?: SpyTrace[];
  /** The flow's own failure, when it failed. */
  error?: string;
}

/**
 * The breakpoint-shaped view of a {@link DebugOutcome}, for callers that only ever asked
 * for one. `reached` is a plain boolean here because a breakpoint run always reports it.
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
   * result at all — distinct from a flow that legitimately produced an empty `output`.
   */
  dropped: boolean;
  /**
   * The flow's result *message* as JSON — `{event_id, variables, body}` — not the body
   * alone. The variables a flow built up are as much its result as its body, so the
   * message is what it reports, and a caller after the body reads `.body` of the parsed
   * envelope.
   *
   * This means the same thing whatever debug features the run used. Under `--spies` the
   * runner prints a {@link DebugOutcome} on stdout *instead of* the result message, so
   * this is re-derived from the envelope's `result` rather than left as the raw envelope
   * text — otherwise turning on a spy, which is meant to be read-only, would change what
   * every existing reader of `output` sees. It is empty for a `breakAt` run, which
   * reports its snapshot in `breakpoint` and never produces a result.
   */
  output: string;
  /** The runner's stderr, split into lines (its slog output). */
  logs: string[];
  /**
   * The decoded debug envelope — present only for a run that was asked to *observe*
   * itself (`breakAt`, `spies`, or both) and got far enough to print one. A `mocks`-only
   * run has no envelope: mocking changes what the flow does but observes nothing.
   */
  debug?: DebugOutcome;
  /**
   * The breakpoint view of {@link debug}, present only for a `breakAt` invoke. Kept
   * distinct from `debug` because a caller that only set a breakpoint should not have to
   * narrow an optional `reached`.
   */
  breakpoint?: BreakOutcome;
  /** What each spied block saw, present only for a `spies` invoke. */
  spies?: SpyTrace[];
}

/**
 * Run a single named flow once and return its result plus logs, without starting the
 * integration's sources. This shells out to `octo invoke`, which builds an invoke-mode
 * service, calls the flow, prints its result message to stdout, streams slog to stderr,
 * and tears down.
 *
 * The child's own `-timeout` bounds only the flow call; a wall-clock backstop here
 * force-kills a runner that hangs in startup or teardown so this never blocks forever.
 *
 * The three debug features are the runtime's, passed straight through (see
 * docs/debug-seam.md). With `breakAt` the runner halts at the addressed block; with
 * `spies` it records every message crossing the addressed blocks; with `mocks` it stands
 * in for them. The first two make it print a {@link DebugOutcome} envelope on stdout,
 * decoded here into `debug`/`breakpoint`/`spies`. All three are invoke-mode only, which
 * is exactly what this is.
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
    /** Block addresses to record every message that crosses them (the CLI's `--spies`). */
    spies?: string[];
    /** Block addresses to stand in for, by address (the CLI's `--mocks`). */
    mocks?: Record<string, MockSpec>;
    /** Runner log level. Applied as LOG_LEVEL, which `env` can still override. */
    logLevel?: LogLevel;
  },
): Promise<InvokeResult> {
  const bin = octoBin();

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

  // An address cannot contain a comma under the address grammar, so joining on one is
  // unambiguous — the CLI splits it straight back apart.
  const spies = opts?.spies ?? [];
  if (spies.length > 0) args.push("--spies", spies.join(","));

  // Mocks go as one JSON blob rather than a repeated flag: a spec is a nested thing (a
  // list of cases, each with an expression and an outcome) and there is no honest way to
  // flatten that onto argv. An empty map is not passed at all — the CLI would take it as
  // "mock nothing", which is what omitting the flag already means.
  const mocks = opts?.mocks ?? {};
  if (Object.keys(mocks).length > 0) args.push("--mocks", JSON.stringify(mocks));

  // Only a run that was asked to *observe* itself prints an envelope. A mocks-only run
  // prints its result message like any other, so we must not go looking for one.
  const observing = opts?.breakAt !== undefined || spies.length > 0;

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
  let backstop: NodeJS.Timeout | undefined;
  let cancelKill: (() => void) | undefined;
  const result = await new Promise<InvokeResult>((resolve) => {
    const finish = (exitCode: number | null) => {
      if (backstop) clearTimeout(backstop);
      cancelKill?.();
      const logs = splitLines(errText);
      let ok = exitCode === 0 && !timedOut;

      // An observing run prints an envelope instead of a result message. It only does so
      // when it got far enough to run the flow: a bad address, a bad mock spec or an
      // unloadable config exits non-zero with nothing on stdout, which stays a failure
      // here rather than becoming a silent "not reached".
      let debug: DebugOutcome | undefined;
      let result = output;
      let dropped = ok && logs.some((l) => l.includes(DROP_MARKER));

      if (ok && observing) {
        debug = parseDebugOutcome(output);
        if (!debug) {
          ok = false;
          logs.push(`✖ could not parse the debug envelope: ${output.trim()}`);
        } else {
          // Keep `output` meaning "the flow's result message" whatever the run observed.
          // A spies-only run *does* carry a result — it just carries it inside the
          // envelope — and a spy is supposed to be read-only: turning one on must not
          // change what a caller reading `output` sees. A breakpoint run has no result
          // (it stopped early), so `output` is empty there and `breakpoint.message` is
          // the thing to read.
          result = debug.result !== undefined ? JSON.stringify(debug.result) : "";
          dropped = debug.dropped === true;
        }
      }

      resolve({
        ok,
        exitCode,
        timedOut,
        dropped,
        output: result,
        logs,
        debug,
        breakpoint: toBreakOutcome(debug),
        spies: debug?.spies,
      });
    };
    // Wall-clock backstop: give the child its `-timeout` plus head-room, then escalate
    // SIGTERM → SIGKILL exactly like a stop does.
    backstop = setTimeout(() => {
      timedOut = true;
      cancelKill = terminate(proc);
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
 * Decode the envelope an observing run prints, or undefined when stdout does not hold
 * one. The runner prints it as a single JSON line, but slog output can only reach
 * stderr, so the last non-blank stdout line is the envelope.
 *
 * Identifying it takes both keys, not just one. A breakpoint envelope always carries a
 * boolean `reached`; a **spies-only** envelope deliberately omits it (so it cannot be
 * read as a breakpoint that never fired) and carries a `spies` array instead. A plain
 * result message — `{event_id, variables, body}` — carries neither, which is what keeps
 * a mocks-only run from being mistaken for an envelope.
 */
function parseDebugOutcome(stdout: string): DebugOutcome | undefined {
  const line = splitLines(stdout)
    .filter((l) => l.trim() !== "")
    .pop();
  if (!line) return undefined;
  try {
    const parsed: unknown = JSON.parse(line);
    if (typeof parsed !== "object" || parsed === null) return undefined;
    const outcome = parsed as DebugOutcome;
    if (typeof outcome.reached !== "boolean" && !Array.isArray(outcome.spies)) {
      return undefined;
    }
    return outcome;
  } catch {
    return undefined;
  }
}

/**
 * The breakpoint view of an envelope, or undefined when the run set no breakpoint. A
 * boolean `reached` is what says one was set: the CLI omits the key entirely otherwise.
 */
function toBreakOutcome(debug: DebugOutcome | undefined): BreakOutcome | undefined {
  if (!debug || typeof debug.reached !== "boolean") return undefined;
  return {
    reached: debug.reached,
    block: debug.block ?? "",
    message: debug.message,
    error: debug.error,
  };
}
