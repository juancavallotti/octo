import type { NamedDeploymentUpgrade, TestRun } from "@/app/model/upgrades";
import { failingCases } from "./TestRunReport";

/**
 * The run that speaks for this deployment on this runtime: the latest, when it
 * ran on it and the deployment has not been rolled out since. A run still going
 * always speaks, so its row stays busy.
 */
export function currentRun(
  d: NamedDeploymentUpgrade,
  runtime?: string,
): TestRun | undefined {
  const run = d.latestRun;
  if (!run || !runtime || run.runtimeVersion !== runtime) return undefined;
  if (d.latestRunStale && run.status !== "running") return undefined;
  return run;
}

/** One sentence on how a run went. */
export function runSummary(run: TestRun): string {
  switch (run.status) {
    case "running":
      return run.rolloutOnPass ? "Testing, then upgrading" : "Testing";
    case "passed":
      if (run.rolledOutAt) return "Passed and upgraded";
      // A rollout the run asked for and could not make leaves its reason in
      // error; one still on its way has neither.
      return run.rolloutOnPass && run.error ? "Passed, not upgraded" : "Passed";
    case "failed": {
      const n = failingCases(run.report);
      return `${n} failing case${n === 1 ? "" : "s"}`;
    }
    case "errored":
      return "Could not run";
    case "timed_out":
      return "Timed out";
  }
}

export const RUN_TONE: Record<TestRun["status"], string> = {
  running: "text-sky-600 dark:text-sky-400",
  passed: "text-emerald-600 dark:text-emerald-400",
  failed: "text-red-600 dark:text-red-400",
  errored: "text-amber-600 dark:text-amber-400",
  timed_out: "text-amber-600 dark:text-amber-400",
};
