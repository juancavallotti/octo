"use server";

/**
 * Server actions for the platform agent's install lifecycle.
 *
 * Administrators only, reads included: the agent's status describes the
 * installation, and every mutation spends cluster resources — an install creates
 * a deployment, tracing replaces its pods.
 *
 * The mutations go through `withAdminUser` rather than `withAdmin` because the
 * orchestrator records who acted: it has no session of its own and trusts this
 * boundary for the actor's identity.
 */

import { withAdmin, withAdminUser } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";
import type { AgentStatus } from "./client/agent";

export async function getAgentStatus(): Promise<ActionResult<AgentStatus>> {
  return withAdmin(() => client.getAgentStatus());
}

export async function installAgent(): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) => client.installAgent(userId));
}

export async function rolloutAgent(): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) => client.rolloutAgent(userId));
}

export async function setAgentTracing(
  tracing: boolean,
): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) => client.setAgentTracing(userId, tracing));
}

export async function setAgentAutoFix(
  autoFix: boolean,
): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) => client.setAgentAutoFix(userId, autoFix));
}

export async function setAgentDeploymentSettings(settings: {
  maxIterations?: number;
  autoFix?: boolean;
}): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) =>
    client.setAgentDeploymentSettings(userId, settings),
  );
}

export async function setAgentMaxIterations(
  maxIterations: number,
): Promise<ActionResult<AgentStatus>> {
  return withAdminUser((userId) =>
    client.setAgentMaxIterations(userId, maxIterations),
  );
}

/**
 * The actor is resolved and then not sent, because the route has nowhere to put it:
 * uninstall undeploys and optionally deletes the integration, and neither of those
 * orchestrator operations takes an actor — no removal anywhere in the platform is
 * attributed today. Sending one would describe an attribution that is not recorded.
 * It still goes through the user gate so an unprovisioned caller fails closed.
 */
export async function uninstallAgent(
  purge: boolean,
): Promise<ActionResult<void>> {
  return withAdminUser(() => client.uninstallAgent(purge));
}
