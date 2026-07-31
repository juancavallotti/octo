/**
 * The browser half of a run namespace. The runner a tab drives is keyed by the
 * cookie the host sets *plus* this id, so two tabs of one browser get two runners
 * instead of fighting over one — see @octo/run-host's `deriveNamespace`.
 *
 * sessionStorage is the right home for it: scoped to one tab by definition, and
 * stable across reloads, which is exactly the lifetime a runner should have.
 *
 * A host's RunTransport calls this and passes the result up with each request. The
 * id is opaque and carries no authority on its own — the server mixes it with a
 * secret the browser can't read, so a forged one only ever reaches another
 * namespace of the same browser's.
 */

/** sessionStorage key holding this tab's id. */
const STORAGE_KEY = "octo_run_tab";

/**
 * Resolved once per tab. Deliberately a promise even though nothing awaits yet:
 * establishing an id can require asking other tabs whether they already hold it
 * (a duplicated tab inherits its opener's sessionStorage), and callers should not
 * have to change shape when it does.
 */
let resolved: Promise<string> | null = null;

/** Set when sessionStorage is unreachable (Safari private mode, sandboxed frames),
 * so the id at least stays stable for the life of the page. */
let inMemory: string | null = null;

/** mintTabId returns a fresh id in the shape the host validates: url-safe, bounded. */
function mintTabId(): string {
  return crypto.randomUUID().replace(/-/g, "");
}

/** Reads the stored id, treating an unusable sessionStorage as "nothing stored". */
function readStored(): string | null {
  try {
    return window.sessionStorage.getItem(STORAGE_KEY);
  } catch {
    return inMemory;
  }
}

/** Persists the id, falling back to module state when sessionStorage refuses. */
function writeStored(id: string): void {
  inMemory = id;
  try {
    window.sessionStorage.setItem(STORAGE_KEY, id);
  } catch {
    // Unreachable storage costs stability across reloads, not correctness: the id
    // held above still keeps this page distinct from every other tab.
  }
}

/**
 * runTabId returns this tab's id, minting and persisting one on first use.
 *
 * Returns `""` when there is no browser — the editor is server-rendered, and a host
 * that sends no tab id gets its plain cookie namespace, which is how RUN behaved
 * before tabs were separated. Callers must therefore invoke this lazily (inside a
 * transport method), never at module scope.
 */
export function runTabId(): Promise<string> {
  if (typeof window === "undefined") return Promise.resolve("");
  if (!resolved) {
    const existing = readStored();
    const id = existing ?? mintTabId();
    if (!existing) writeStored(id);
    resolved = Promise.resolve(id);
  }
  return resolved;
}

/** Drops the memoized id. Exported for tests, which need a fresh tab per case. */
export function resetTabIdForTest(): void {
  resolved = null;
  inMemory = null;
}
