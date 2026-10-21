import { exec, type ExecOptions } from "../exec";
import { binary } from "../settings";
import { common, failure } from "./args";

/**
 * octopaas as the shell runs it: the bundled binary (or the one Settings chose), told
 * which installation every time, printing JSON on stdout and plain text on stderr.
 * Profiles and sign-ins are octopaas's own, in ~/.octopaas, shared with the CLI in
 * the user's terminal.
 */

/** A command octopaas refused or failed, in its own words. */
export class CliError extends Error {
  constructor(
    message: string,
    readonly code: number,
  ) {
    super(message);
  }
}

/** The app keeps octopaas current itself, so the CLI's own update notice is noise. */
function env(): NodeJS.ProcessEnv {
  return { ...process.env, NO_COLOR: "1", OCTOPAAS_NO_UPDATE_NOTIFIER: "1" };
}

export interface RunOptions extends Pick<ExecOptions, "onLine" | "signal" | "input"> {
  /** The installation to act on; null for the commands that are about profiles. */
  profile: string | null;
}

/**
 * Run octopaas and read what it printed. Commands that print nothing on success
 * (pull, rm, logout) resolve to null.
 */
export async function run<T>(args: string[], opts: RunOptions): Promise<T | null> {
  const { profile, ...rest } = opts;
  const result = await exec(binary("octopaas"), [...common(profile), ...args], { ...rest, env: env() });
  if (result.code !== 0) throw new CliError(failure(result.stderr, result.code), result.code);
  const out = result.stdout.trim();
  if (!out) return null;
  try {
    return JSON.parse(out) as T;
  } catch {
    throw new CliError(`octopaas printed something that is not JSON: ${out.slice(0, 200)}`, 1);
  }
}

/** A list, where octopaas may print null for an empty one. */
export async function list<T>(args: string[], opts: RunOptions): Promise<T[]> {
  return (await run<T[]>(args, opts)) ?? [];
}

/**
 * Run octopaas for what it says along the way rather than what it prints at the end:
 * a deploy's progress, a followed log. Every line goes to `onLine`; a failure still
 * throws, a cancel included.
 */
export async function stream(args: string[], opts: RunOptions & Required<Pick<ExecOptions, "onLine">>): Promise<void> {
  const { profile, ...rest } = opts;
  const result = await exec(binary("octopaas"), [...common(profile), ...args], { ...rest, env: env() });
  if (result.code !== 0) throw new CliError(failure(result.stderr, result.code), result.code);
}
