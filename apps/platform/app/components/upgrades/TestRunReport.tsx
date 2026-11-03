"use client";

import Link from "next/link";
import {
  CheckCircle2,
  CircleAlert,
  Loader2,
  PencilLine,
  XCircle,
} from "lucide-react";
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

/** Whether a finished run calls for someone to look at the suites or the flows. */
export function needsAttention(run: TestRun): boolean {
  return (
    run.status === "failed" ||
    run.status === "errored" ||
    run.status === "timed_out"
  );
}

/**
 * A test run's outcome: its status, what it added up to, every suite and case,
 * and what became of the rollout it asked for. `editorHref`, when given, is
 * offered on a run that did not pass, since the editor is where suites and
 * flows are fixed.
 */
export default function TestRunReport({
  run,
  editorHref,
}: {
  run: TestRun;
  editorHref?: string;
}) {
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
      {editorHref && needsAttention(run) && (
        <Link
          href={editorHref}
          className="inline-flex w-fit items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-xs font-medium text-zinc-700 hover:bg-black/[0.04] dark:border-white/10 dark:text-zinc-200 dark:hover:bg-white/[0.06]"
        >
          <PencilLine size={13} />
          Open in the editor
        </Link>
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
