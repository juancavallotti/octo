import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Probes and caches the runner's `--version` line. Kept apart from session.ts so
 * the process-owning session stays focused; the cache lives on `globalThis` (like
 * the session) so it survives Next's dev HMR module reloads.
 */

const execFileAsync = promisify(execFile);

const store = globalThis as unknown as {
  /** Cached `--version` line; undefined until probed, null when unavailable. */
  __octoRuntimeVersion?: string | null;
};

/** The cached version line (sync); null until probed or when unavailable. */
export function cachedVersion(): string | null {
  return store.__octoRuntimeVersion ?? null;
}

/**
 * Probe the runner's version once via `octo --version` and cache it. Idempotent:
 * subsequent calls return the cached value. Resolves to null (cached) when no
 * binary is configured or the probe fails. The GET route awaits this to warm the
 * cache so `status()` can read it synchronously.
 */
export async function probeVersion(): Promise<string | null> {
  if (store.__octoRuntimeVersion !== undefined) return store.__octoRuntimeVersion;
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    store.__octoRuntimeVersion = null;
    return null;
  }
  try {
    const { stdout } = await execFileAsync(bin, ["--version"]);
    store.__octoRuntimeVersion = stdout.split("\n")[0].trim() || null;
  } catch {
    store.__octoRuntimeVersion = null;
  }
  return store.__octoRuntimeVersion;
}
