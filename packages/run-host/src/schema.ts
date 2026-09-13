import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Probes and caches the runtime's generated capability schema (`octo schema`). The
 * runner owns the schema, so the binary is asked once and the parsed JSON cached on
 * `globalThis`, where it survives dev HMR module reloads.
 *
 * Returns null when no binary is configured or the probe fails, leaving the caller to
 * fall back to its bundled schema.
 */

const execFileAsync = promisify(execFile);

const store = globalThis as unknown as {
  /** Cached schema; undefined until probed, null when unavailable. */
  __octoRuntimeSchema?: unknown | null;
};

/** The cached schema (sync); null until probed or when unavailable. */
export function cachedSchema(): unknown | null {
  return store.__octoRuntimeSchema ?? null;
}

/**
 * Probe the runner's capability schema once via `octo schema` and cache it.
 * Idempotent: subsequent calls return the cached value. Resolves to null when no
 * binary is configured or the probe fails.
 *
 * A *failed* probe is not cached: the usual reason the exec fails is that the binary is
 * not there yet, and caching that null would leave the process schema-blind — an empty
 * palette, every block type unknown — for the rest of its life.
 */
export async function probeSchema(): Promise<unknown | null> {
  if (store.__octoRuntimeSchema !== undefined) return store.__octoRuntimeSchema;
  const bin = process.env.OCTO_BIN_PATH;
  if (!bin) {
    store.__octoRuntimeSchema = null;
    return null;
  }
  try {
    const { stdout } = await execFileAsync(bin, ["schema"], {
      maxBuffer: 32 * 1024 * 1024,
    });
    store.__octoRuntimeSchema = JSON.parse(stdout);
  } catch {
    return null; // not cached: the binary may yet appear
  }
  return store.__octoRuntimeSchema;
}
