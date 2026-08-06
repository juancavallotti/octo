import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Probes and caches the version lines of the two binaries the host spawns: `octo`, the
 * runner, and `dolphin`, the test runner behind the editor's Testing tab. Kept apart
 * from session.ts so the process-owning session stays focused; the caches live on
 * `globalThis` (like the session) so they survive Next's dev HMR module reloads.
 *
 * One module rather than two near-identical ones, because the two probes differ only in
 * which variable names the binary and which word asks it for its version — and a reader
 * comparing them for a version-skew warning wants them side by side.
 */

const execFileAsync = promisify(execFile);

const store = globalThis as unknown as {
  /** Cached `--version` line; undefined until probed, null when unavailable. */
  __octoRuntimeVersion?: string | null;
  /** Cached `dolphin version` line; undefined until probed, null when unavailable. */
  __octoTestVersion?: string | null;
};

/**
 * Run `<bin> <arg>` and return its first line, or null when there is no binary
 * configured or it would not answer. A probe that fails is a null, not a throw: an
 * absent runner is a normal state the UI renders, not an error the caller handles.
 */
async function probe(bin: string | undefined, arg: string): Promise<string | null> {
  if (!bin) return null;
  try {
    const { stdout } = await execFileAsync(bin, [arg]);
    return stdout.split("\n")[0].trim() || null;
  } catch {
    return null;
  }
}

/** Which binaries the host has for its one-shot runs, and what they are. */
export interface Binaries {
  /** Whether `octo` is configured — what gates every one-shot run and the CEL tester. */
  available: boolean;
  version: string | null;
  /** Whether `dolphin` is configured. Separate because either can be absent alone. */
  testAvailable: boolean;
  testVersion: string | null;
}

/**
 * What this host can spawn, for the editor's status.
 *
 * Deliberately not part of what an app runner reports. Whether a binary is installed is
 * a fact about the HOST, not about the backend running the app — and on a host whose app
 * runs elsewhere the two answers come apart: it may be unable to start an app while
 * still running `invoke`, `eval` and a test suite perfectly well, all of which are these
 * binaries. Conflating them would hide working features whenever the app runner was
 * unavailable.
 *
 * Probe first ({@link probeVersion}, {@link probeTestVersion}) so the versions are warm;
 * this reads the caches synchronously.
 */
export function binaries(): Binaries {
  return {
    available: !!process.env.OCTO_BIN_PATH,
    version: cachedVersion(),
    testAvailable: !!process.env.DOLPHIN_BIN_PATH,
    testVersion: cachedTestVersion(),
  };
}

/** The cached octo version line (sync); null until probed or when unavailable. */
export function cachedVersion(): string | null {
  return store.__octoRuntimeVersion ?? null;
}

/** The cached dolphin version line (sync); null until probed or when unavailable. */
export function cachedTestVersion(): string | null {
  return store.__octoTestVersion ?? null;
}

/**
 * Probe the runner's version once via `octo --version` and cache it. Idempotent:
 * subsequent calls return the cached value. Resolves to null (cached) when no
 * binary is configured or the probe fails. The GET route awaits this to warm the
 * cache so `status()` can read it synchronously.
 */
export async function probeVersion(): Promise<string | null> {
  if (store.__octoRuntimeVersion !== undefined) return store.__octoRuntimeVersion;
  store.__octoRuntimeVersion = await probe(process.env.OCTO_BIN_PATH, "--version");
  return store.__octoRuntimeVersion;
}

/**
 * Probe the test runner's version once via `dolphin version` and cache it, on the same
 * terms as {@link probeVersion}.
 *
 * `version` rather than `--version` only because that is dolphin's own spelling; it
 * accepts both (see runtime/dolphin/main.go).
 *
 * Why a caller wants this at all: inside an image the two binaries are built from one
 * module in one stage and cannot disagree, but a developer's `bin/dolphin` can be an
 * older build than the `bin/octo` beside it. That mismatch shows up as a test failing
 * on a block type the octo under test supports and the dolphin's octo does not — a
 * confusing failure that is cheap to warn about and expensive to debug. It is a
 * warning, never a refusal: rebuilding one binary at a time is a normal thing to do.
 */
export async function probeTestVersion(): Promise<string | null> {
  if (store.__octoTestVersion !== undefined) return store.__octoTestVersion;
  store.__octoTestVersion = await probe(process.env.DOLPHIN_BIN_PATH, "version");
  return store.__octoTestVersion;
}
