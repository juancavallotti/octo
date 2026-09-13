/**
 * Where the agent is, for everything that has to reach him.
 *
 * The address is not configuration: it is the in-cluster Service of whatever
 * deployment the install produced, which the orchestrator reports as part of the
 * agent's status. So it is looked up, and because it changes only when someone
 * installs, removes or redeploys the agent, it is cached briefly rather than
 * fetched on every message.
 */

import { baseUrl, callRaw } from "./http";

/** How long a resolved address is trusted. Short enough that an uninstall is noticed. */
const TTL_MS = 30_000;

/**
 * How long the status lookup waits before giving up.
 *
 * fetch has no timeout of its own, so an orchestrator that accepts a connection
 * and then says nothing would hold every caller open indefinitely. Five seconds is
 * far longer than a request to a service in the same cluster takes and short
 * enough to be a delay rather than a hang.
 */
const STATUS_TIMEOUT_MS = 5_000;

interface Resolved {
  url: string;
  at: number;
}

let cached: Resolved | null = null;

/** Whether an orchestrator base URL is configured at all. */
export function orchestratorConfigured(): boolean {
  return baseUrl() !== "";
}

/** The agent's status, as much of it as reaching him needs. */
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

/** Read the agent's status from the orchestrator — the state, not just the address. */
export async function fetchAgentStatus(): Promise<AgentReachability | null> {
  try {
    const res = await callRaw("/settings/agent", {
      cache: "no-store",
      signal: AbortSignal.timeout(STATUS_TIMEOUT_MS),
    });
    if (!res || !res.ok) return null;
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
