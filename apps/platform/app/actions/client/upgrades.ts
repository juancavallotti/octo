/**
 * Runtime upgrades: the orchestrator's upgrades view, and the test runs nested
 * under a deployment.
 */

import { call, enc } from "./http";
import type { TestRun, UpgradesView } from "@/app/model/upgradeTypes";
import type { ActionResult } from "@octo/http";

/** Every deployment as an upgrade sees it. */
export function listUpgrades(): Promise<ActionResult<UpgradesView>> {
  return call<UpgradesView>("GET", "/upgrades");
}

/** Run a deployment's suites; without a snapshot, the version it runs. */
export function startTestRun(
  deploymentId: string,
  opts: { snapshotId?: string; rolloutOnPass: boolean },
): Promise<ActionResult<TestRun>> {
  return call<TestRun>("POST", `/deployments/${enc(deploymentId)}/testruns`, {
    ...(opts.snapshotId ? { snapshotId: opts.snapshotId } : {}),
    rolloutOnPass: opts.rolloutOnPass,
  });
}

/** A deployment's most recent test runs, newest first. */
export function listTestRuns(deploymentId: string): Promise<ActionResult<TestRun[]>> {
  return call<TestRun[]>("GET", `/deployments/${enc(deploymentId)}/testruns`);
}

/** One test run. */
export function getTestRun(
  deploymentId: string,
  runId: string,
): Promise<ActionResult<TestRun>> {
  return call<TestRun>(
    "GET",
    `/deployments/${enc(deploymentId)}/testruns/${enc(runId)}`,
  );
}
