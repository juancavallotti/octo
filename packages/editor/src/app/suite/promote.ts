/**
 * A run that just happened → a test case.
 *
 * A debugging session already contains everything a case needs: the input, the mocks
 * that stood in for the awkward blocks, and the message that came back. Pure, and knows
 * nothing about React or the run provider; the caller passes a {@link PromotableRun} and
 * decides which runs qualify.
 *
 * Two rules, either of which makes a promoted test fail on its first honest run if got
 * wrong. **The body is the message's `body`, not the message** — a run's result is the
 * whole `{event_id, variables, body}` envelope, and no flow ever produces one of those.
 * And **variables are opt-in**, because `expect.vars` is a subset check and seeding it
 * with everything the run built up asserts far more than the user meant.
 */

import type { Expectation, MockSpec, Outcome, SuiteCase, SuiteInput } from "./types";

/**
 * What a run was made with, kept on the result so it can be promoted afterwards.
 *
 * Values, not the JSON text the transport moves: this is the shape a suite case holds.
 */
export interface RunSetup {
  input?: SuiteInput;
  /** The blocks that were stood in for, already as values. */
  mocks?: Record<string, MockSpec>;
  /**
   * Address → how many messages crossed it ON THIS RUN. Zero is a fact worth keeping —
   * `count: 0` asserts a block never ran — so it is recorded, not dropped.
   */
  spies?: Record<string, number>;
}

/** One finished run, as much of it as a case can be made from. */
export interface PromotableRun {
  outcome: Outcome;
  /** The whole result message — `{event_id, variables, body}` — not the body alone. */
  message?: unknown;
  setup?: RunSetup;
}

export interface PromoteOptions {
  name: string;
  /** Assert the variables the run produced, as a subset. Off unless asked for. */
  vars?: boolean;
  /** Assert how many messages crossed each spied block. */
  spies?: boolean;
  /**
   * What the failure message must contain, for a run that failed.
   *
   * Typed by the user rather than lifted from the run: a failed flow's stderr is a
   * timestamped log line, and a case seeded with that could never pass.
   *
   * Without one there is nothing to assert — dolphin has no "expect any failure", so the
   * case comes out asserting that the flow COMPLETED.
   */
  errorContains?: string;
}

/** Build the case a run would become. */
export function promoteRun(run: PromotableRun, opts: PromoteOptions): SuiteCase {
  const c: SuiteCase = { name: opts.name.trim() };

  const input = run.setup?.input;
  if (input && (input.data !== undefined || hasKeys(input.vars))) c.input = input;
  if (hasKeys(run.setup?.mocks)) c.mocks = run.setup?.mocks;
  if (opts.spies && run.setup?.spies && hasKeys(run.setup.spies)) {
    c.spies = Object.fromEntries(
      Object.entries(run.setup.spies).map(([address, count]) => [address, { count }]),
    );
  }

  const expect = expectationOf(run, opts);
  if (expect) c.expect = expect;
  return c;
}

/**
 * What the run says should have happened. Absent is not a gap: a case with no `expect`
 * asserts that the flow completed without failing, which is exactly what a run that
 * produced an empty message tells us.
 */
function expectationOf(run: PromotableRun, opts: PromoteOptions): Expectation | undefined {
  if (run.outcome === "dropped") return { dropped: true };
  if (run.outcome === "error") {
    const text = opts.errorContains?.trim();
    return text ? { error: text } : undefined;
  }

  const message = asMessage(run.message);
  const expect: Expectation = {};
  if (message.body !== undefined) expect.body = message.body;
  if (opts.vars && hasKeys(message.variables)) expect.vars = message.variables;
  return Object.keys(expect).length ? expect : undefined;
}

/**
 * Read a result message's parts. A run whose output was not a message — a runner that
 * printed something else — yields neither, and the case simply asserts less rather than
 * asserting something invented.
 */
function asMessage(message: unknown): {
  body?: unknown;
  variables?: Record<string, unknown>;
} {
  if (typeof message !== "object" || message === null) return {};
  const { body, variables } = message as {
    body?: unknown;
    variables?: Record<string, unknown>;
  };
  return { body, variables };
}

function hasKeys(record: Record<string, unknown> | undefined): boolean {
  return !!record && Object.keys(record).length > 0;
}
