"use client";

import { AlertCircle } from "lucide-react";
import type { SuiteIssue } from "../../suite/validate";

/**
 * The problems with the open suite, shown above the editor.
 *
 * These are LOAD errors, not test failures: dolphin refuses the whole file, so one
 * unnamed case takes every other case in it down. That is why they are shown as the user
 * types rather than waiting for a run — the alternative is pressing Run and getting a
 * failure that names none of the tests you wrote.
 */
export default function SuiteIssues({ issues }: { issues: SuiteIssue[] }) {
  if (issues.length === 0) return null;
  return (
    <div
      role="alert"
      className="max-h-32 shrink-0 overflow-y-auto border-b border-amber-500/30 bg-amber-500/10 px-3 py-2"
    >
      <p className="mb-1 flex items-center gap-1.5 text-[11px] font-medium text-amber-700 dark:text-amber-300">
        <AlertCircle size={12} className="shrink-0" />
        dolphin will not load this suite
      </p>
      <ul className="space-y-0.5">
        {issues.map((issue, i) => (
          <li
            key={i}
            className="text-[11px] leading-relaxed text-amber-800 dark:text-amber-200/90"
          >
            {issue.caseIndex !== undefined && (
              <span className="text-amber-600 dark:text-amber-400">
                case {issue.caseIndex + 1}:{" "}
              </span>
            )}
            {issue.message}
          </li>
        ))}
      </ul>
    </div>
  );
}
