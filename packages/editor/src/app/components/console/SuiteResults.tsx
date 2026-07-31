"use client";

import { totalsOf, type TestSuiteResult } from "../../run/testTransport";
import TestTally from "../TestTally";
import CaseResults from "./CaseResults";

/**
 * One suite's slice of a report: which suite it is, what it added up to, and its cases.
 *
 * The grouping is what makes a run of many suites readable, and it is not only cosmetic —
 * a case name is unique only WITHIN a file, and every scaffolded suite starts with a case
 * called "it runs". Flattened, three suites would show three identical rows with no way to
 * tell which flow each belonged to.
 */
export default function SuiteResults({ suite }: { suite: TestSuiteResult }) {
  return (
    <section>
      <div className="flex items-center gap-2 border-b border-black/5 bg-black/[0.03] px-3 py-1 dark:border-white/5 dark:bg-white/[0.03]">
        <code className="min-w-0 flex-1 truncate text-[11px] font-medium text-zinc-600 dark:text-zinc-300">
          {suite.name}
        </code>
        <TestTally totals={totalsOf(suite.cases)} />
      </div>
      <CaseResults cases={suite.cases} />
    </section>
  );
}
