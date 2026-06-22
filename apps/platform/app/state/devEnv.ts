/**
 * Client-only store for the *values* of a run's declared environment variables —
 * the actual secrets that satisfy the document's `${NAME}` variables when the
 * editor spawns a local runner. The Dev .env console tab mirrors the document's
 * declared `env:` list and lets the user fill in a value for each.
 *
 * These never enter the document model or the rendered YAML (which would leave
 * secrets on disk under OCTO_RUN_DIR). They live in the browser's localStorage and
 * are sent to the BFF only at run time, where they are injected into the spawned
 * process's environment and discarded (see api/run/session.ts).
 *
 * Values are scoped by the open integration's id so secrets never bleed between
 * integrations. Unsaved drafts (id === null) share a single "__draft__" bucket.
 * This is a stopgap until per-user profiles exist.
 */

/** Map of declared env var name → its dev value. */
export type DevEnvMap = Record<string, string>;

const PREFIX = "octo.devEnv:";
const DRAFT = "__draft__";

/** localStorage key holding the dev env values for the given integration id. */
export function devEnvKey(id: string | null): string {
  return `${PREFIX}${id ?? DRAFT}`;
}

/** Read the stored dev env value map for an integration (empty when none/unavailable). */
export function loadDevEnv(id: string | null): DevEnvMap {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(devEnvKey(id));
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return {};
    }
    const out: DevEnvMap = {};
    for (const [name, val] of Object.entries(parsed)) {
      if (typeof val === "string") out[name] = val;
    }
    return out;
  } catch {
    return {};
  }
}

/** Persist the dev env value map for an integration. */
export function saveDevEnv(id: string | null, map: DevEnvMap): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(devEnvKey(id), JSON.stringify(map));
  } catch {
    // Best-effort: ignore quota/serialization failures.
  }
}
