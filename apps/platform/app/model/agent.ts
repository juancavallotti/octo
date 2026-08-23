/**
 * Browser-side client for the platform agent's install lifecycle. Backed by the
 * server actions in `app/actions/agent.ts`; these wrappers unwrap the ActionResult
 * so callers keep a value-or-throw contract.
 */

import * as actions from "@/app/actions/agent";
import { unwrap } from "./bff";

export type { AgentStatus } from "@/app/actions/client/agent";

import type { AgentStatus } from "@/app/actions/client/agent";

/** Read the agent's status. Never fails for a reason `blocked` could name instead. */
export async function getAgentStatus(): Promise<AgentStatus> {
  return unwrap(await actions.getAgentStatus());
}

/** Install the agent and deploy it internal-only. Idempotent. */
export async function installAgent(): Promise<AgentStatus> {
  return unwrap(await actions.installAgent());
}

/**
 * Publish the bundle this orchestrator ships as a new version and roll onto it.
 * Replaces local edits to the agent's definition — see `AgentStatus.edited`.
 */
export async function rolloutAgent(): Promise<AgentStatus> {
  return unwrap(await actions.rolloutAgent());
}

/** Turn tracing on or off. A rolling update: the runtime reads it at startup. */
export async function setAgentTracing(tracing: boolean): Promise<AgentStatus> {
  return unwrap(await actions.setAgentTracing(tracing));
}

/**
 * Set the turn limit for one run, or 0 to go back to the definition's default.
 * A rolling update, for the same reason tracing is: the runtime reads it at startup.
 */
export async function setAgentMaxIterations(maxIterations: number): Promise<AgentStatus> {
  return unwrap(await actions.setAgentMaxIterations(maxIterations));
}

/** Undeploy the agent, and with `purge` delete its integration too. */
export async function uninstallAgent(purge: boolean): Promise<void> {
  return unwrap(await actions.uninstallAgent(purge));
}
