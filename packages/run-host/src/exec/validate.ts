import { spawn } from "node:child_process";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { namespaceDir, stageProject, type ProjectFile } from "../staging";
import type { ResourceProvider } from "../resources";
import { octoBin, splitLines, terminate } from "../child";

/**
 * One-shot `octo validate`: check a project's references the way the runtime would
 * resolve them, without starting anything.
 *
 * The project is staged as a folder, because that is the unit the runtime merges: a
 * reference from one file to a connector or flow declared in another only resolves
 * when both are loaded together.
 */

/** Default wall-clock budget. Validation starts nothing, so this only guards a hang. */
const VALIDATE_DEFAULT_TIMEOUT_MS = 15000;

/** One problem `octo validate` found. Mirrors the runtime's `runtime.Issue`. */
export interface ValidationIssue {
  /** The block address, flow, or connector it sits on; absent for the whole config. */
  where?: string;
  message: string;
}

/** The outcome of an `octo validate`. */
export interface ValidateResult {
  /**
   * True when the runner answered with a well-formed envelope. A project with issues is
   * still a run that answered: read `valid` for the verdict.
   */
  ok: boolean;
  /** True exactly when `issues` is empty. */
  valid: boolean;
  issues: ValidationIssue[];
  /** Required environment variables nothing supplied. Notes, not issues. */
  unsetEnv: string[];
  /** Why the runner did not answer, when `ok` is false. */
  error?: string;
  /** The runner's stderr, split into lines. */
  logs: string[];
}

/** Validate a project: stage its files in a throwaway folder and run `octo validate`. */
export async function validate(
  ns: string,
  files: ProjectFile[],
  opts?: {
    env?: Record<string, string>;
    resources?: ResourceProvider;
    timeoutMs?: number;
  },
): Promise<ValidateResult> {
  const bin = octoBin();
  const dir = join(namespaceDir(ns), `validate-${randomUUID()}`);
  await mkdir(dir, { recursive: true });
  try {
    await stageProject(dir, files, opts?.resources);
    return await runValidate(bin, dir, opts?.env ?? {}, opts?.timeoutMs ?? VALIDATE_DEFAULT_TIMEOUT_MS);
  } finally {
    await rm(dir, { recursive: true, force: true }).catch(() => {});
  }
}

function runValidate(
  bin: string,
  dir: string,
  env: Record<string, string>,
  timeoutMs: number,
): Promise<ValidateResult> {
  const proc = spawn(bin, ["validate", "-config", dir, "-format", "json"], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, ...env },
  });

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
  return new Promise<ValidateResult>((resolve) => {
    const finish = (exitCode: number | null) => {
      if (backstop) clearTimeout(backstop);
      cancelKill?.();
      const logs = splitLines(errText);
      const failed = (error: string): ValidateResult => ({
        ok: false,
        valid: false,
        issues: [],
        unsetEnv: [],
        error,
        logs,
      });
      if (timedOut) {
        resolve(failed(`validate timed out after ${timeoutMs}ms`));
        return;
      }
      // A project with issues exits non-zero *and* prints its envelope, so the envelope
      // decides — the exit code only says something when there is none.
      const envelope = parseEnvelope(output);
      if (!envelope) {
        const reason = logs.length > 0 ? logs[logs.length - 1] : `runner exited ${exitCode}`;
        resolve(failed(reason));
        return;
      }
      resolve({ ok: true, valid: envelope.ok, issues: envelope.issues, unsetEnv: envelope.unsetEnv, logs });
    };
    backstop = setTimeout(() => {
      timedOut = true;
      cancelKill = terminate(proc);
    }, timeoutMs);
    proc.on("error", (err) => {
      errText += `✖ failed to start runner: ${err.message}\n`;
      finish(null);
    });
    // `close`, not `exit`: the process can exit before its stdout is drained, and the
    // envelope must be read whole.
    proc.on("close", (code) => finish(code));
  });
}

interface envelope {
  ok: boolean;
  issues: ValidationIssue[];
  unsetEnv: string[];
}

/** Decode the `octo validate --format json` envelope, or undefined when stdout holds none. */
function parseEnvelope(stdout: string): envelope | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    return undefined;
  }
  if (typeof parsed !== "object" || parsed === null) return undefined;
  const { ok, issues, unsetEnv } = parsed as { ok?: unknown; issues?: unknown; unsetEnv?: unknown };
  if (typeof ok !== "boolean" || !Array.isArray(issues)) return undefined;
  return {
    ok,
    issues: issues.filter(isIssue),
    unsetEnv: Array.isArray(unsetEnv) ? unsetEnv.filter((n): n is string => typeof n === "string") : [],
  };
}

function isIssue(value: unknown): value is ValidationIssue {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { message?: unknown }).message === "string"
  );
}
