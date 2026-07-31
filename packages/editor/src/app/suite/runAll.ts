/**
 * Which suites a "run everything" can actually run.
 *
 * The filter is not politeness. dolphin loads every suite it was named BEFORE it runs any
 * case (runtime/dolphin/internal/suite/discover.go, `loadAll`), so one file it refuses
 * aborts the whole run and reports nothing — including for the five suites that were
 * fine. Sending a suite the editor already knows is broken would turn one bad file into
 * zero results, with an error naming none of the good ones.
 *
 * So the rule lives here, beside validate.ts, which already owns what dolphin will refuse.
 * Pure and React-free, like the rest of this directory: it is the same knowledge whether a
 * button, a rail or an agent is the one about to press Run.
 *
 * What is left out is reported rather than dropped. A green tally that quietly covers
 * three suites which never ran is worse than a red one.
 */

import { parseSuite } from "./parse";
import type { TestSuiteInput } from "../run/testTransport";

/** A suite the editor deliberately did not send, and why. */
export interface SkippedSuite {
  /** The flow the suite is stored against — what every surface addresses it by. */
  flow: string;
  reason: string;
}

/** What a run-everything resolves to: what to send, and what was held back. */
export interface RunAllPlan {
  targets: TestSuiteInput[];
  skipped: SkippedSuite[];
}

/** One stored suite, as the suite store keeps them. */
interface StoredSuite {
  flow: string;
  content: string;
}

/**
 * Split the stored suites into the ones that can run and the ones that cannot.
 *
 * `flows` is the document's flow names. A suite whose flow is not among them is a suite
 * left behind by a rename or a deletion: invisible in the Testing tab, which lists flows
 * rather than files, but still on disk — and still picked up by `dolphin test` in a
 * terminal and by CI. Naming it here is usually the first the user hears of it.
 *
 * Order is preserved, so the caller reports suites in the same order the store lists them.
 */
export function planRunAll(
  stored: readonly StoredSuite[],
  flows: readonly string[],
): RunAllPlan {
  const targets: TestSuiteInput[] = [];
  const skipped: SkippedSuite[] = [];

  for (const { flow, content } of stored) {
    if (!flows.includes(flow)) {
      skipped.push({ flow, reason: "there is no flow by that name in this document" });
      continue;
    }
    const { suite, issues } = parseSuite(content);
    // Checked before the issues it is one of, only so it reads as the ordinary starting
    // state it is rather than as a file dolphin would reject.
    if (suite.cases.length === 0) {
      skipped.push({ flow, reason: "has no cases yet" });
      continue;
    }
    if (issues.length > 0) {
      skipped.push({ flow, reason: `dolphin would refuse it: ${issues[0].message}` });
      continue;
    }
    targets.push({ name: flow, content });
  }

  return { targets, skipped };
}
