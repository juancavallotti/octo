/**
 * The platform agent's install lifecycle: what is installed, what is running, and
 * the handful of things an operator can do about it.
 *
 * The agent is an ordinary integration — the orchestrator installs it from a bundle
 * it ships, but everything after that is the same integration, snapshot and
 * deployment machinery as anything a user builds. That is why there is no "agent
 * logs" or "agent scale" operation here: those already exist on deployments, and
 * `deploymentId` is what connects the two.
 */

import type { ActionResult } from "@octo/http";
import { call } from "./http";

/**
 * What the orchestrator reports about the agent.
 *
 * `blocked` is the field worth understanding: the status route deliberately does not
 * fail when the agent *cannot* be installed, because a page that 500s cannot tell
 * anyone what to configure. So a missing cluster, a missing encryption key and a
 * missing LLM key all arrive here as a reason rather than an error.
 */
export interface AgentStatus {
  /** not_installed | installed | deployed | update_available | failed */
  state: string;
  integrationId?: string;
  deploymentId?: string;
  /** The in-cluster address the chat panel proxies to. Empty until deployed. */
  internalUrl?: string;
  /** The version tag the running deployment was published under. */
  installedTag?: string;
  /** The binary ships a newer bundle than was last rolled out. */
  updateAvailable: boolean;
  /**
   * The installed definition or its skills no longer match the bundle they came
   * from — someone edited the agent, which is a supported thing to do. It matters
   * because rolling out an update replaces those edits.
   */
  edited: boolean;
  tracing: boolean;
  /**
   * How many tool-calling turns one run may take, when an operator has set a limit.
   * Absent means the agent's own definition decides, which is the default state and
   * is why this is optional rather than a number that is sometimes meaningless.
   */
  maxIterations?: number;
  /** "" when nothing stands in the way; otherwise kubernetes | encryption | llm_key. */
  blocked?: string;
  /** The deployment's own phase, and its failure message when it has one. */
  deploymentStatus?: string;
  reason?: string;
}

export function getAgentStatus(): Promise<ActionResult<AgentStatus>> {
  return call<AgentStatus>("GET", "/settings/agent");
}

export function installAgent(actorId: string): Promise<ActionResult<AgentStatus>> {
  return call<AgentStatus>("POST", "/settings/agent/install", { actorId });
}

export function rolloutAgent(actorId: string): Promise<ActionResult<AgentStatus>> {
  return call<AgentStatus>("POST", "/settings/agent/rollout", { actorId });
}

export function setAgentTracing(
  actorId: string,
  tracing: boolean,
): Promise<ActionResult<AgentStatus>> {
  return call<AgentStatus>("POST", "/settings/agent/tracing", { actorId, tracing });
}

/**
 * Set how many turns one run may take. Zero clears the override and puts the
 * definition's own default back in force — the only way back to the shipped value.
 */
export function setAgentMaxIterations(
  actorId: string,
  maxIterations: number,
): Promise<ActionResult<AgentStatus>> {
  return call<AgentStatus>("POST", "/settings/agent/max-iterations", { actorId, maxIterations });
}

/**
 * Remove the agent. Without `purge` the integration is kept, because it holds the
 * definition and that may have been edited; purge deletes it and its skills too.
 */
export function uninstallAgent(purge: boolean): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/settings/agent${purge ? "?purge=1" : ""}`);
}
