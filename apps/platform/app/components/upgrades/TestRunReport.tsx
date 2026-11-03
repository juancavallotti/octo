"use client";

import { CheckCircle2, CircleAlert, Loader2, XCircle } from "lucide-react";
import { SuiteResults, TestTally } from "@octo/editor";
import type { TestReport, TestRun, TestRunStatus } from "@/app/model/upgrades";

const STATUS: Record<TestRunStatus, { label: string; tone: string }> = {
  running: { label: "Running tests", tone: "text-sky-600 dark:text-sky-400" },
  passed: {
    label: "Tests passed",
    tone: "text-emerald-600 dark:text-emerald-400",
  },
  failed: { label: "Tests failed", tone: "text-red-600 dark:text-red-400" },
  errored: {
    label: "Tests could not run",
    tone: "text-amber-600 dark:text-amber-400",
  },
  timed_out: {
    label: "Tests timed out",
    tone: "text-amber-600 dark:text-amber-400",
  },
};

/** The icon for a run's status. */
export function StatusIcon({
  status,
  size = 14,
}: {
  status: TestRunStatus;
  size?: number;
}) {
  if (status === "running")
    return <Loader2 size={size} className="animate-spin" />;
  if (status === "passed") return <CheckCircle2 size={size} />;
  if (status === "failed") return <XCircle size={size} />;
  return <CircleAlert size={size} />;
}

/** A run's status in words, toned. */
export function TestRunStatusLabel({ run }: { run: TestRun }) {
  const s = STATUS[run.status];
  return (
    <span
      className={`inline-flex items-center gap-1.5 text-sm font-medium ${s.tone}`}
    >
      <StatusIcon status={run.status} />
      {s.label}
      {run.runtimeVersion && (
        <span className="font-normal text-zinc-500 dark:text-zinc-400">
          on 🐙 {run.runtimeVersion}
        </span>
      )}
    </span>
  );
}

/**
 * A test run's outcome: its status, what it added up to, every suite and case,
 * and what became of the rollout it asked for.
 */
export default function TestRunReport({ run }: { run: TestRun }) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <TestRunStatusLabel run={run} />
        {run.report && <TestTally totals={run.report.totals} />}
      </div>
      {run.rolloutOnPass && run.rolledOutAt && (
        <p className="text-sm text-emerald-700 dark:text-emerald-400">
          Rolled out onto the tested version.
        </p>
      )}
      {run.error && (
        <p className="whitespace-pre-wrap text-sm text-red-500">{run.error}</p>
      )}
      {run.report && run.report.suites.length > 0 && (
        <div className="overflow-hidden rounded-md border border-black/10 dark:border-white/10">
          {reportSuites(run.report).map((suite) => (
            <SuiteResults key={suite.name} suite={suite} />
          ))}
        </div>
      )}
    </div>
  );
}

/** The report's suites, named by file rather than by the path they ran from. */
export function reportSuites(report: TestReport) {
  return report.suites.map((s) => ({
    name: s.path.slice(s.path.lastIndexOf("/") + 1),
    flow: s.flow,
    cases: s.cases,
  }));
}

/** How many cases in a report did not pass. */
export function failingCases(report?: TestReport): number {
  return report ? report.totals.failed + report.totals.errored : 0;
}
