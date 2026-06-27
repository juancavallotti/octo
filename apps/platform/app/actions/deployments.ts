"use server";

/**
 * Server actions for deployments — the BFF replacement for the deployment route
 * handlers (create/scale/undeploy/rollout plus the list and deploy-options reads).
 * Each action authorizes and delegates to the orchestrator client lib; the model
 * unwraps the ActionResult. The live deployment event stream stays a route (SSE).
 */

import type {
  Deployment,
  DeploymentInput,
  DeployOptions,
} from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listDeployments(
  integrationId: string,
): Promise<ActionResult<Deployment[]>> {
  return withRead(() => client.listDeployments(integrationId));
}

export async function getDeployOptions(
  integrationId: string,
  opts: { slug?: string; expose?: "external"; snapshotId?: string } = {},
): Promise<ActionResult<DeployOptions>> {
  return withRead(() => client.getDeployOptions(integrationId, opts));
}

export async function createDeployment(
  integrationId: string,
  input: DeploymentInput,
): Promise<ActionResult<Deployment>> {
  return withWrite(() => client.createDeployment(integrationId, input));
}

export async function rolloutDeployment(
  id: string,
  snapshotId: string,
): Promise<ActionResult<Deployment>> {
  return withWrite(() => client.rolloutDeployment(id, snapshotId));
}

export async function scaleDeployment(
  id: string,
  replicas: number,
): Promise<ActionResult<Deployment>> {
  return withWrite(() => client.scaleDeployment(id, replicas));
}

export async function deleteDeployment(
  id: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteDeployment(id));
}
