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
 *
 * The one thing sessionStorage does not give us for free: a tab created *from*
 * another one — "Duplicate tab", window.open, ctrl-clicking a target=_blank link —
 * inherits a copy of its opener's storage, and so its id. That is a likely way to
 * reach for a second integration, and it would land both tabs back on one runner,
 * so an inherited id is checked against the tabs already holding it.
 */

/** sessionStorage key holding this tab's id. */
const STORAGE_KEY = "octo_run_tab";

/** Channel the tabs of one browser use to sort out who holds which id. */
const CHANNEL = "octo_run_tab";

/** How long to let live tabs object to an inherited id. Only ever paid once, on a
 * load that inherited one, and RunProvider asks for status on mount — so it lands
 * well before anything a user could click. */
const CLAIM_TIMEOUT_MS = 75;

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

/** mintTabId returns a fresh id in the shape the host validates: url-safe, bounded.
 *
 * randomUUID is restricted to secure contexts, so a host served over plain HTTP —
 * an internal deployment on a LAN, say — does not have it. getRandomValues carries
 * no such restriction, and 16 random bytes is the same 32 hex characters. */
function mintTabId(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID().replace(/-/g, "");
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
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

/** Opened once this tab has an id to defend, and left open for the tab's life. */
let channel: BroadcastChannel | null = null;

/** Answers other tabs asking whether `id` is already taken, and keeps answering as
 * long as this tab holds it. Absent BroadcastChannel (older Safari, some sandboxed
 * frames) simply means no one can be asked — see {@link claim}. */
function defend(id: string): void {
  if (typeof BroadcastChannel === "undefined") return;
  channel?.close();
  channel = new BroadcastChannel(CHANNEL);
  channel.onmessage = (ev: MessageEvent) => {
    if (ev.data?.type === "claim" && ev.data.id === id) {
      channel?.postMessage({ type: "taken", id });
    }
  };
}

/**
 * Resolves true when a live tab already holds `id` — i.e. this tab is a duplicate
 * and needs its own.
 *
 * Silence means free, and that is forced rather than chosen: the ordinary case is a
 * reload with no sibling to answer, and treating silence as taken would hand it a
 * new id every time — losing exactly the runner-survives-a-reload property this
 * whole mechanism exists to provide.
 *
 * So the errors are not symmetric. A wrong `true` costs one unnecessary fresh
 * runner. A wrong `false` — a sibling that misses the window — leaves two tabs on
 * one runner, which is the old behaviour, for that tab only, until it reloads.
 * Hence a window generous enough that a live tab answering a same-origin broadcast
 * will not realistically miss it.
 */
function claim(id: string): Promise<boolean> {
  if (typeof BroadcastChannel === "undefined") return Promise.resolve(false);
  return new Promise((resolve) => {
    const asking = new BroadcastChannel(CHANNEL);
    const done = (taken: boolean) => {
      clearTimeout(timer);
      asking.close();
      resolve(taken);
    };
    const timer = setTimeout(() => done(false), CLAIM_TIMEOUT_MS);
    asking.onmessage = (ev: MessageEvent) => {
      if (ev.data?.type === "taken" && ev.data.id === id) done(true);
    };
    asking.postMessage({ type: "claim", id });
  });
}

/** Establishes this tab's id: keep what was stored unless another tab still holds
 * it, in which case this load is a duplicate and starts fresh. */
async function establish(): Promise<string> {
  const stored = readStored();
  // Nothing stored means nothing to conflict with — this tab is the first to use it.
  if (!stored) {
    const id = mintTabId();
    writeStored(id);
    defend(id);
    return id;
  }
  const id = (await claim(stored)) ? mintTabId() : stored;
  if (id !== stored) writeStored(id);
  defend(id);
  return id;
}

/**
 * runTabId returns this tab's id, establishing one on first use.
 *
 * Returns `""` when there is no browser — the editor is server-rendered, and a host
 * that sends no tab id gets its plain cookie namespace, which is how RUN behaved
 * before tabs were separated. Callers must therefore invoke this lazily (inside a
 * transport method), never at module scope.
 */
export function runTabId(): Promise<string> {
  if (typeof window === "undefined") return Promise.resolve("");
  // Never rejects. A browser that cannot produce an id at all — no usable crypto, a
  // sandbox that blocks BroadcastChannel outright — sends none and gets the plain
  // cookie namespace, which is how RUN behaved before tabs were separated: one
  // shared runner. Since the answer is memoized, a rejection would instead be
  // memoized too, and break every RUN call for the life of the page.
  if (!resolved) resolved = establish().catch(() => "");
  return resolved;
}

/** Drops the memoized id. Exported for tests, which need a fresh tab per case. */
export function resetTabIdForTest(): void {
  resolved = null;
  inMemory = null;
  channel?.close();
  channel = null;
}
