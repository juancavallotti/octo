import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { promisify } from "node:util";
import { bundledBinary, stateDir, type RuntimeBinary } from "./paths";
import { read, write, type DesktopSettings } from "./state";

/**
 * What the user chose, as opposed to what the app shipped with.
 *
 * The only setting with teeth today is which `octo` and `dolphin` run. The app
 * bundles a matched pair, and that is the right default — but a developer working on
 * the runtime wants the binary they just built, and waiting for a release to try it
 * is not a reasonable answer.
 *
 * Cached, because `binary()` is on the path of every server start and every run, and
 * the alternative is reading and parsing a JSON file each time. The cache is dropped
 * on write rather than kept in step, so there is one source of truth and it is the
 * file.
 */

let cached: DesktopSettings | null = null;

export function settings(): DesktopSettings {
  if (!cached) cached = read(stateDir()).settings ?? {};
  return cached;
}

/** Merge `patch` into the stored settings and return the result. */
export function updateSettings(patch: DesktopSettings): DesktopSettings {
  const state = read(stateDir());
  const next: DesktopSettings = {
    ...(state.settings ?? {}),
    ...patch,
    // A patch that mentions `runtime` replaces it wholesale, so clearing one
    // override is expressible; without this, undefined would merge to "unchanged"
    // and "Use bundled" could never be said.
    ...(patch.runtime !== undefined ? { runtime: patch.runtime } : {}),
  };
  write(stateDir(), { ...state, settings: next });
  cached = next;
  return next;
}

/**
 * The binary that should actually run.
 *
 * An override pointing at a file that is not there falls back to the bundled one. A
 * user whose external drive is unplugged, or who moved their checkout, gets a working
 * app and a Settings window that shows the override as missing — rather than an app
 * that cannot start and does not say why.
 */
export function binary(name: RuntimeBinary): string {
  const chosen = settings().runtime?.[name];
  return chosen && existsSync(chosen) ? chosen : bundledBinary(name);
}

const run = promisify(execFile);

/** What a binary says it is, or null when it cannot say. */
export async function probeVersion(file: string, name: RuntimeBinary): Promise<string | null> {
  if (!existsSync(file)) return null;
  try {
    // The two disagree on how to be asked, and have since dolphin was split out.
    const args = name === "octo" ? ["--version"] : ["version"];
    const { stdout } = await run(file, args, { timeout: 5000 });
    return stdout.trim().split("\n")[0] || null;
  } catch {
    // Not a runtime binary, not executable, or refused to run: all the same answer
    // to the only question being asked, which is whether this file can serve.
    return null;
  }
}

/** One binary as the Settings window shows it. */
export interface BinaryStatus {
  name: RuntimeBinary;
  /** The path in use — the override when it is usable, else the bundled one. */
  path: string;
  bundled: string;
  /** What the user chose, whether or not it works. Absent means the bundled one. */
  override?: string;
  /** True when an override is set but cannot be used. */
  missing: boolean;
  version: string | null;
}

export async function binaryStatus(name: RuntimeBinary): Promise<BinaryStatus> {
  const override = settings().runtime?.[name];
  const inUse = binary(name);
  return {
    name,
    path: inUse,
    bundled: bundledBinary(name),
    ...(override ? { override } : {}),
    missing: !!override && override !== inUse,
    version: await probeVersion(inUse, name),
  };
}
