"use server";

/**
 * Server actions for runtime upgrades: the upgrades view and a deployment's test
 * runs. Each action authorizes and delegates to the orchestrator client lib.
 */

import type { NamedUpgradesView, TestRun } from "@/app/model/upgradeTypes";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

/**
 * Every deployment as an upgrade sees it, named after its integration. A
 * deployment whose integration was deleted between the two reads is left out.
 */
export async function listUpgrades(): Promise<ActionResult<NamedUpgradesView>> {
  return withRead(async () => {
    const [view, integrations] = await Promise.all([
      client.listUpgrades(),
      client.listIntegrations(),
    ]);
    if (!integrations.ok) return integrations;
    // An install with no cluster serves no upgrades route: nothing is deployed.
    if (!view.ok && view.status !== 404) return view;
    const data = view.ok ? view.data : { deployments: [] };
    const names = new Map(integrations.data.map((i) => [i.id, i.name]));
    return {
      ok: true,
      data: {
        runtimeVersion: data.runtimeVersion,
        deployments: data.deployments.flatMap((d) => {
          const integrationName = names.get(d.integrationId);
          return integrationName === undefined ? [] : [{ ...d, integrationName }];
        }),
      },
    };
  });
}

export async function startTestRun(
  deploymentId: string,
  opts: { snapshotId?: string; rolloutOnPass: boolean },
): Promise<ActionResult<TestRun>> {
  return withWrite(() => client.startTestRun(deploymentId, opts));
}

export async function listTestRuns(
  deploymentId: string,
): Promise<ActionResult<TestRun[]>> {
  return withRead(() => client.listTestRuns(deploymentId));
}

export async function getTestRun(
  deploymentId: string,
  runId: string,
): Promise<ActionResult<TestRun>> {
  return withRead(() => client.getTestRun(deploymentId, runId));
}
