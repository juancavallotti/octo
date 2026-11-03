/**
 * Browser-side runtime upgrades: the upgrades view and test runs, unwrapped to
 * value-or-throw.
 */

import * as upgradeActions from "@/app/actions/upgrades";
import { unwrap } from "./bff";
import type { NamedUpgradesView, TestRun } from "./upgradeTypes";

export * from "./upgradeTypes";

/** Every deployment as an upgrade sees it, named after its integration. */
export async function listUpgrades(): Promise<NamedUpgradesView> {
  return unwrap(await upgradeActions.listUpgrades());
}

/**
 * Run a deployment's suites against this install's runtime; without a snapshot,
 * the version it runs. With `rolloutOnPass`, a run where every case passes rolls
 * the deployment onto the tested version, keeping its env and tracing.
 */
export async function startTestRun(
  deploymentId: string,
  opts: { snapshotId?: string; rolloutOnPass: boolean },
): Promise<TestRun> {
  return unwrap(await upgradeActions.startTestRun(deploymentId, opts));
}

/** A deployment's most recent test runs, newest first. */
export async function listTestRuns(deploymentId: string): Promise<TestRun[]> {
  return unwrap(await upgradeActions.listTestRuns(deploymentId));
}

/** One test run. */
export async function getTestRun(deploymentId: string, runId: string): Promise<TestRun> {
  return unwrap(await upgradeActions.getTestRun(deploymentId, runId));
}
