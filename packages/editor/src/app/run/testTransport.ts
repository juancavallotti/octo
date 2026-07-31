/**
 * The Testing tab's half of the RUN transport contract: what it takes to run a flow's
 * dolphin suites, and what comes back.
 *
 * Declared here rather than imported from `@octo/run-host`, for the same reason
 * {@link FlowRunOutcome} is: that package spawns processes and reads the filesystem, and
 * the editor is a browser bundle. The two shapes are kept aligned by the host action in
 * the middle, which maps them field by field.
 *
 * A separate module from transport.ts because these types are as long again as the rest
 * of the contract, and nothing outside the Testing tab reads them.
 */

/** What became of one case. Mirrors dolphin's report; see runtime/dolphin/internal/report. */
export type TestCaseStatus = "passed" | "failed" | "errored" | "skipped" | "not-run";

/** One expectation that did not hold. */
export interface TestFailure {
  /** One line, for a list row. */
  summary: string;
  /** The full text, which for a body diff is several lines — render it preformatted. */
  detail: string;
}

/**
 * What the flow actually did, as distinct from what the case wanted. Present for every
 * case that ran, not only the failures: a passing case's output is what "Save as test
 * case" reads, and showing want-beside-got is most of what makes a failure legible.
 */
export interface TestCaseOutcome {
  /** The flow's result message: `{event_id, variables, body}`. */
  result?: unknown;
  dropped?: boolean;
  /** The flow's own failure. */
  error?: string;
  /** What each watched block saw, keyed by address. */
  spies?: Record<string, unknown[]>;
}

/** One case, run or not. */
export interface TestCaseResult {
  name: string;
  status: TestCaseStatus;
  elapsedMs: number;
  summary?: string;
  failures?: TestFailure[];
  /** Why an ERRORED case could not be run — the suite is wrong, not the flow. */
  error?: string;
  /** octo's stderr, carried only for an errored case. */
  stderr?: string;
  /** The reason a skipped case gave. */
  skip?: string;
  outcome?: TestCaseOutcome;
}

/** One suite, run. */
export interface TestSuiteResult {
  /** The name the caller asked for the suite under — a flow name, never a path. */
  name: string;
  /** The flow the suite declares. */
  flow: string;
  cases: TestCaseResult[];
}

/** What the run added up to. */
export interface TestTotals {
  cases: number;
  passed: number;
  failed: number;
  errored: number;
  skipped: number;
  notRun: number;
  elapsedMs: number;
}

/** One suite to run: the flow it tests, and its YAML. */
export interface TestSuiteInput {
  name: string;
  content: string;
}

/**
 * A request to run one or more suites against a config.
 *
 * The suites travel in the request rather than being read from the store, exactly as
 * `yaml` does in a {@link FlowRunRequest}: the tab must be able to run the edit in front
 * of the user, not the last one that happened to be saved.
 */
export interface TestRunRequest {
  /** The config under test, as the editor renders it. */
  yaml: string;
  /** The open integration, so the host can resolve its resources; absent for a draft. */
  integrationId?: string;
  suites: TestSuiteInput[];
  /** Extra environment for every case, over what the suites declare. */
  env?: Record<string, string>;
}

/** The outcome of running one or more suites. */
export interface TestRunOutcome {
  /**
   * Whether a report came back that could be read — **not** whether the tests passed.
   *
   * dolphin exits non-zero when a case fails, and a failing case is the normal thing the
   * user is here to look at. Folding that into `ok` would report "the run failed" and
   * hide it. The verdict is in {@link totals}.
   */
  ok: boolean;
  /** True when the wall-clock backstop had to kill the run. */
  timedOut: boolean;
  totals: TestTotals;
  suites: TestSuiteResult[];
  /** The runner's stderr lines. */
  logs: string[];
  /** Why the run could not be made at all, or its report not read. */
  error?: string;
}

/** An empty tally — what a run that produced no report reports. */
export function emptyTotals(): TestTotals {
  return {
    cases: 0,
    passed: 0,
    failed: 0,
    errored: 0,
    skipped: 0,
    notRun: 0,
    elapsedMs: 0,
  };
}

/** The status each case counts towards, keyed as {@link TestTotals} names them. */
const TALLY: Record<TestCaseStatus, keyof TestTotals> = {
  passed: "passed",
  failed: "failed",
  errored: "errored",
  skipped: "skipped",
  "not-run": "notRun",
};

/**
 * Tally a subset of a report.
 *
 * A run may carry several suites while the Testing tab's toolbar speaks for exactly one,
 * so the whole-run {@link TestRunOutcome.totals} is the wrong scope there. `elapsedMs`
 * sums the cases rather than measuring wall clock — exact while dolphin runs one case at
 * a time, and the run's own figure stays authoritative in the console.
 */
export function totalsOf(cases: readonly TestCaseResult[]): TestTotals {
  const totals = emptyTotals();
  for (const c of cases) {
    totals.cases++;
    totals[TALLY[c.status]]++;
    totals.elapsedMs += c.elapsedMs;
  }
  return totals;
}

/**
 * Why a test run cannot be started at all, shared by every control that offers one, so the
 * open suite's Run button and the header's cannot drift on how they explain it.
 */
export const NO_TEST_RUNNER =
  "No test runner: DOLPHIN_BIN_PATH is not set on the server.";
