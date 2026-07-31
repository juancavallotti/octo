/**
 * The rules dolphin enforces on a suite, checked here so the Testing tab can say what is
 * wrong before a run instead of surfacing it as an opaque failure.
 *
 * Everything in here mirrors `File.validate` in runtime/dolphin/internal/suite. Two
 * properties matter:
 *
 *   - These are LOAD errors, not test failures. dolphin refuses the whole file, so one
 *     unnamed case takes every other case in it down. That is why they are reported as
 *     you type rather than when you run.
 *   - Unknown keys are among them. dolphin decodes with KnownFields(true), because a
 *     typo'd key that was silently ignored would look exactly like a test that passes: a
 *     misspelled `spys:` watches nothing, asserts nothing, and goes green — worse than
 *     having no test at all. So the editor must be just as strict, or it will happily
 *     author a file dolphin rejects.
 */

import { isSharedInput, outcomeOf, isEmptyExpect, isValidDuration } from "./types";
import type {
  Expectation,
  MockCaseSpec,
  MockSpec,
  RecordExpect,
  Suite,
  SuiteCase,
  SpyExpect,
} from "./types";

/**
 * One problem with a suite. `caseIndex` says which case it belongs to so the rail can
 * mark it; absent means the problem is the file's.
 */
export interface SuiteIssue {
  message: string;
  severity: "error" | "warning";
  caseIndex?: number;
  /**
   * The file says something the model cannot hold, so re-serializing it would DELETE
   * that something. Set by parse.ts on everything it could not read; never by the rules
   * below, which are about content that was read fine and dolphin will nonetheless
   * reject. The form view is disabled while any of these stand — it can only write back
   * what it understood, and quietly dropping a key the user typed is the one failure
   * they would have no way to notice.
   */
  lossy?: boolean;
}

/** The keys each level of the format accepts, mirroring dolphin's KnownFields decoding. */
export const SUITE_KEYS = ["flow", "inputs", "mocks", "env", "timeout", "cases"];
export const CASE_KEYS = [
  "name",
  "input",
  "mocks",
  "env",
  "expect",
  "spies",
  "timeout",
  "skip",
];
export const INPUT_KEYS = ["data", "vars"];
export const EXPECT_KEYS = ["body", "vars", "that", "dropped", "error"];
export const SPY_KEYS = ["count", "records"];
export const RECORD_KEYS = ["input", "output", "dropped", "error"];
export const MOCK_KEYS = ["cases", "default"];
export const MOCK_CASE_KEYS = ["when", "body", "vars", "error", "drop"];

/**
 * Check a suite. Returns every problem found rather than stopping at the first, because
 * a form shows them all at once.
 */
export function validateSuite(suite: Suite): SuiteIssue[] {
  const issues: SuiteIssue[] = [];
  const error = (message: string, caseIndex?: number) =>
    issues.push({ message, severity: "error", caseIndex });

  if (!suite.flow.trim()) {
    error("needs a `flow`: the flow every case in the file calls");
  }
  if (!suite.cases.length) {
    error("has no cases yet");
  }
  if (suite.timeout && !isValidDuration(suite.timeout)) {
    error(`timeout ${JSON.stringify(suite.timeout)} is not a duration (e.g. 30s, 1m30s)`);
  }
  validateMocks(suite.mocks, undefined, error);

  const seen = new Set<string>();
  suite.cases.forEach((c, i) => {
    const name = c.name.trim();
    // A case is named before it is numbered: the name is what a report, a JUnit XML and
    // a person all identify it by, so an unnamed one has nothing to be called.
    if (!name) {
      error(`case ${i} needs a name`, i);
    } else if (seen.has(name)) {
      error(`two cases are named ${JSON.stringify(c.name)}`, i);
    }
    seen.add(name);
    validateCase(suite, c, i, error);
  });
  return issues;
}

type Raise = (message: string, caseIndex?: number) => void;

function validateCase(suite: Suite, c: SuiteCase, i: number, error: Raise): void {
  if (isSharedInput(c.input) && !suite.inputs?.[c.input]) {
    const declared = Object.keys(suite.inputs ?? {}).sort();
    error(
      `input ${JSON.stringify(c.input)} is not declared in \`inputs\` (have: ${
        declared.length ? declared.join(", ") : "none"
      })`,
      i,
    );
  }
  if (c.timeout && !isValidDuration(c.timeout)) {
    error(`timeout ${JSON.stringify(c.timeout)} is not a duration (e.g. 30s, 1m30s)`, i);
  }
  validateMocks(c.mocks, i, error);
  if (c.expect) validateExpectation(c.expect, i, error);
  for (const address of Object.keys(c.spies ?? {}).sort()) {
    if (!address.trim()) {
      error("a spy has an empty block address", i);
      continue;
    }
    validateSpy(c.spies![address], address, i, error);
  }
}

/**
 * The one-outcome rule: a flow either produced a message, dropped it, or failed. A case
 * expecting a body AND a failure is not a stricter test, it is an impossible one — under
 * `error` the flow returned no message for a body to be compared against.
 */
function validateExpectation(e: Expectation, i: number, error: Raise): void {
  if (e.dropped && e.error) {
    error(
      "expects both `dropped` and `error`: a flow that failed did not filter the message out",
      i,
    );
    return;
  }
  const outcome = outcomeOf(e);
  if (outcome === "message") return;
  if (!isEmptyExpect(e)) {
    const past = outcome === "error" ? "failed" : "dropped the message";
    error(
      `expects \`${outcome}\` as well as the message the flow returned, but a flow that ${past} returned none`,
      i,
    );
  }
}

function validateSpy(spy: SpyExpect, address: string, i: number, error: Raise): void {
  const at = `spy ${JSON.stringify(address)}`;
  if (spy.count !== undefined && spy.count < 0) {
    error(`${at}: count is negative`, i);
  }
  // Naming more records than the count asserts is a contradiction the suite can never
  // satisfy, and it is nearly always a miscount rather than an intent.
  const records = spy.records ?? [];
  if (spy.count !== undefined && records.length > spy.count) {
    error(
      `${at}: asserts on ${records.length} records but expects a count of ${spy.count}`,
      i,
    );
  }
  records.forEach((record, r) => {
    const outcomes = recordOutcomes(record);
    if (outcomes.length > 1) {
      error(
        `${at}: record ${r} expects more than one outcome (${outcomes.join(
          ", ",
        )}): a block either returns a message, fails, or drops it`,
        i,
      );
    }
  });
}

/**
 * A crossing's stated outcomes. `input` is deliberately not one of them: the spy clones
 * the message before running the block, so even a block that failed shows what it was
 * carrying when it did.
 */
function recordOutcomes(r: RecordExpect): string[] {
  const out: string[] = [];
  if (r.output) out.push("output");
  if (r.dropped) out.push("dropped");
  if (r.error) out.push("error");
  return out;
}

/**
 * Mocks, against the engine's own rules (`core.validateSpec`). dolphin runs these too,
 * via `core.NewMocks`, before it spawns anything — so a bad spec is a load error that
 * takes the whole file down, and it belongs here with the rest of them.
 */
function validateMocks(
  mocks: Record<string, MockSpec> | undefined,
  i: number | undefined,
  error: Raise,
): void {
  for (const address of Object.keys(mocks ?? {}).sort()) {
    if (!address.trim()) {
      error("a mock has an empty block address", i);
      continue;
    }
    const at = `mock ${JSON.stringify(address)}`;
    const spec = mocks![address];
    if (!spec.cases?.length && !spec.default) {
      error(`${at}: needs at least one case, or a default`, i);
    }
    spec.cases?.forEach((c, n) => {
      if (!c.when) {
        error(
          `${at}: case ${n} needs a \`when\` expression (a case with no condition is the \`default\`)`,
          i,
        );
      }
      mockOutcome(c, `${at}: case ${n}`, i, error);
    });
    if (spec.default) {
      // Without a default, a message that matches no case fails the block — which is
      // not the same as falling through to the real one, because the mock replaced it.
      if (spec.default.when) {
        error(
          `${at}: the default must not have a \`when\` expression: it is what runs when no case matched`,
          i,
        );
      }
      mockOutcome(spec.default, `${at}: default`, i, error);
    }
  }
}

/** The one-outcome rule on a mock case: a block returns a message, fails, or drops it. */
function mockOutcome(c: MockCaseSpec, at: string, i: number | undefined, error: Raise): void {
  const set: string[] = [];
  if (c.body !== undefined && c.body !== null) set.push("body");
  if (c.error) set.push("error");
  if (c.drop) set.push("drop");

  if (set.length === 0) {
    error(`${at}: needs an outcome: one of \`body\`, \`error\` or \`drop\``, i);
    return;
  }
  if (set.length > 1) {
    error(
      `${at}: has more than one outcome (${set.join(
        ", ",
      )}): a block either returns a message, fails, or drops it`,
      i,
    );
    return;
  }
  if (c.vars && Object.keys(c.vars).length && c.body === undefined) {
    error(
      `${at}: \`vars\` only goes with a \`body\`: a block that failed or dropped returned no message`,
      i,
    );
  }
}
