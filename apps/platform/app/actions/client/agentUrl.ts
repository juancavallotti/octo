/**
 * Where the agent is, for everything that has to reach him.
 *
 * The address is not configuration: it is the in-cluster Service of whatever
 * deployment the install produced, which the orchestrator reports as part of the
 * agent's status. So it is looked up, and because it changes only when someone
 * installs, removes or redeploys the agent, it is cached briefly rather than
 * fetched on every message.
 *
 * It lives in the client layer rather than beside the routes because it is a
 * client-layer concern — a base URL, resolved — and because it now has three
 * callers rather than two: the chat proxy, the status probe, and the server
 * actions that read a person's past conversations — which use the status for the
 * integration id rather than the address, since conversations live in the
 * orchestrator now.
 */

/** How long a resolved address is trusted. Short enough that an uninstall is noticed. */
const TTL_MS = 30_000;

/**
 * How long the status lookup waits before giving up.
 *
 * fetch has no timeout of its own, and this call sits in front of everything that
 * reaches the agent — the chat proxy, the status probe, the conversation list — so
 * an orchestrator that accepts a connection and then says nothing would hold each
 * of them open indefinitely. Five seconds is far longer than a request to a
 * service in the same cluster takes and short enough to be a delay rather than a
 * hang.
 */
const STATUS_TIMEOUT_MS = 5_000;

interface Resolved {
  url: string;
  at: number;
}

let cached: Resolved | null = null;

/** The orchestrator base URL with any trailing slash trimmed, or "" when unset. */
export function orchestratorUrl(): string {
  return (process.env.ORCHESTRATOR_URL ?? "").replace(/\/+$/, "");
}

/** The agent's status, as much of it as these routes care about. */
export interface AgentReachability {
  state: string;
  internalUrl?: string;
  /**
   * The integration the agent is installed as. Conversations are keyed on it —
   * they belong to the integration and survive a redeploy — so reading somebody's
   * history needs this rather than the address.
   */
  integrationId?: string;
}

export type ResolveResult =
  | { ok: true; url: string }
  | { ok: false; error: string; status: number };

/**
 * Read the agent's status from the orchestrator. Used directly by the status probe,
 * which wants the state and not just the address.
 */
export async function fetchAgentStatus(): Promise<AgentReachability | null> {
  const base = orchestratorUrl();
  if (!base) return null;
  try {
    const res = await fetch(`${base}/settings/agent`, {
      cache: "no-store",
      signal: AbortSignal.timeout(STATUS_TIMEOUT_MS),
    });
    if (!res.ok) return null;
    return (await res.json()) as AgentReachability;
  } catch {
    return null;
  }
}

/** Resolve the agent's address, from the cache when it is fresh. */
export async function resolveAgentUrl(): Promise<ResolveResult> {
  if (cached && Date.now() - cached.at < TTL_MS) {
    return { ok: true, url: cached.url };
  }

  const status = await fetchAgentStatus();
  if (!status) {
    return { ok: false, error: "could not read the agent's status", status: 502 };
  }
  const url = (status.internalUrl ?? "").replace(/\/+$/, "");
  if (!url) {
    // Not an error worth a stack trace: the agent simply is not installed, and the
    // panel should say so rather than look broken.
    return {
      ok: false,
      error: "the platform agent is not deployed — install it from Admin → Platform agent",
      status: 503,
    };
  }

  cached = { url, at: Date.now() };
  return { ok: true, url };
}

/**
 * Drop the cached address after a failure to connect, so the next attempt looks it
 * up again instead of retrying a pod that has been replaced for the whole TTL.
 */
export function forgetAgentUrl(): void {
  cached = null;
}
