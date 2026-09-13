import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Probes and caches the version lines of the two binaries the host spawns: `octo`, the
 * runner, and `dolphin`, the test runner. A fact about the host rather than about any
 * run, which is why it is not on the app-runner port — on a host whose app runs elsewhere
 * the two answers come apart. The caches live on `globalThis`, so they survive dev HMR
 * module reloads.
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
 * What this host can spawn.
 *
 * Not part of what an app runner reports: whether a binary is installed is a fact about
 * the host, and a host whose app runs elsewhere may be unable to start one while running
 * `invoke`, `eval` and a test suite perfectly well.
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
 * `version` rather than `--version` only because that is dolphin's own spelling.
 *
 * Worth asking because the two binaries can be built separately: an older dolphin beside
 * a newer octo shows up as a test failing on a block type one of them does not know. A
 * caller warns on a mismatch and never refuses — rebuilding one binary at a time is
 * normal.
 */
export async function probeTestVersion(): Promise<string | null> {
  if (store.__octoTestVersion !== undefined) return store.__octoTestVersion;
  store.__octoTestVersion = await probe(process.env.DOLPHIN_BIN_PATH, "version");
  return store.__octoTestVersion;
}
