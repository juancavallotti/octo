import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Probes and caches the runtime's generated capability schema (`octo schema`).
 * Modelled on version.ts: the runner owns the schema (it's generated from the Go
 * block/connector metadata), so we ask the bundled `octo` binary for it once and
 * cache the parsed JSON. The cache lives on `globalThis` (like the version probe
 * and the session) so it survives Next's dev HMR module reloads.
 *
 * Returns null when no binary is configured or the probe fails — the caller then
 * falls back to the editor's bundled `capabilities.json`.
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
 * A *failed* probe is deliberately not cached. Configuring a binary is a statement
 * of intent, and the usual reason the exec fails is that the binary is not there
 * *yet* — a dev server started while `task build` is still linking it. Caching that
 * null would leave the process schema-blind for its whole life: an empty palette, and
 * a validator that calls every block type unknown, until someone thinks to restart it.
 * Retrying costs one exec on a path that is already broken.
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
