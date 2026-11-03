/**
 * Runtime upgrades as the orchestrator reports them: which deployments run an
 * older runtime than this install deploys, and the test runs that prove a move.
 */

import type { TestCaseResult, TestTotals } from "@octo/editor";

/** Where a test run is. */
export type TestRunStatus = "running" | "passed" | "failed" | "errored" | "timed_out";

/** One suite in dolphin's report, by the path it ran from. */
export interface TestReportSuite {
  path: string;
  flow: string;
  cases: TestCaseResult[];
}

/** Dolphin's JSON report, as the run recorded it. */
export interface TestReport {
  dolphin?: string;
  wallMs?: number;
  totals: TestTotals;
  suites: TestReportSuite[];
}

/** One run of a deployment's test suites against this install's runtime. */
export interface TestRun {
  id: string;
  deploymentId: string;
  snapshotId: string;
  /** The runtime release the suites ran against. */
  runtimeVersion?: string;
  status: TestRunStatus;
  rolloutOnPass: boolean;
  rolledOutAt?: string;
  report?: TestReport;
  /** Why the run errored, or why the rollout it asked for did not happen. */
  error?: string;
  createdBy?: string;
  createdAt: string;
  finishedAt?: string;
}

/** One deployment as an upgrade sees it. */
export interface DeploymentUpgrade {
  deploymentId: string;
  integrationId: string;
  name: string;
  status: string;
  tag?: string;
  snapshotId?: string;
  runtimeVersion?: string;
  /** The newer runtime a rollout would move it onto; absent when it is current. */
  upgradeTo?: string;
  /** Whether the version it runs has test suites. */
  hasSuites: boolean;
  latestRun?: TestRun;
}

/** Every deployment as an upgrade sees it, and the runtime this install deploys. */
export interface UpgradesView {
  runtimeVersion?: string;
  deployments: DeploymentUpgrade[];
}

/** A deployment upgrade named after its integration. */
export type NamedDeploymentUpgrade = DeploymentUpgrade & { integrationName: string };

/** The upgrades view with every deployment named. */
export interface NamedUpgradesView {
  runtimeVersion?: string;
  deployments: NamedDeploymentUpgrade[];
}

/** Whether a run has stopped changing. */
export function isFinished(run: TestRun): boolean {
  return run.status !== "running";
}
