/**
 * The dolphin test-suite model, mirroring `runtime/dolphin/internal/suite/testfile.go`.
 *
 * A suite is a debug config with assertions bolted on: the input, the mocks and the
 * spied addresses are the very sections `octo invoke --run-debug-config` already reads.
 * That is why the mocks here are the editor's existing {@link MockSpec} — the same type
 * the run transport sends — rather than a second copy of the same shape. It is also what
 * makes the two bridges cheap: a suite case can be handed to the canvas ▶ menu, and a run
 * result can be promoted into a case, without translating between two mock models.
 *
 * These types are the *shape*, not the rules. dolphin rejects several combinations that
 * typecheck fine here (an expectation that wants both a body and an error, a spy naming
 * more records than its count); validate.ts is where those live, because a form has to be
 * able to hold a half-written file long enough for the user to finish it.
 */

import type { MockSpec } from "../run/transport";

export type { MockSpec, MockCaseSpec } from "../run/transport";

/**
 * What a flow is called with — the same shape as a debug config's `input:`, because that
 * is exactly what it becomes. Sources are not started under `invoke`, so nothing
 * populates the message for you: `vars` is how a case seeds what a source normally would
 * (the http source copies request headers into them).
 */
export interface SuiteInput {
  data?: unknown;
  vars?: Record<string, unknown>;
}

/**
 * A case's input: the name of one declared in `inputs:`, or an inline one written where
 * it is used. Both are common — a body reused by six cases wants a name, a body used
 * once does not — so the format takes either.
 */
export type CaseInput = string | SuiteInput;

/** Whether a case input is a reference to a shared input rather than an inline one. */
export function isSharedInput(input: CaseInput | undefined): input is string {
  return typeof input === "string";
}

/**
 * What a message should look like: the body exactly, the variables it must at least
 * carry, and any CEL that must hold over it.
 *
 * `body` is an exact deep-equal and `vars` a subset, and the asymmetry is deliberate.
 * The body is the flow's answer, and a test earns its keep by failing when a new field
 * appears in it. Variables are scratch space the engine and the blocks add to, so
 * pinning the whole map would break on every unrelated change. `that: ['vars.size() ==
 * 2']` is there for when you do want it exact.
 */
export interface MessageExpect {
  body?: unknown;
  vars?: Record<string, unknown>;
  /** CEL over the message; every expression must be true. */
  that?: string[];
}

/**
 * What the flow should have done: produced a message, dropped it, or failed. Exactly as
 * a block does, and for the same reason — those are the three things a flow can do.
 */
export interface Expectation extends MessageExpect {
  /** The flow filtered the message out and returned nothing. */
  dropped?: boolean;
  /** The flow failed, with a message containing this text. */
  error?: string;
}

/** Which of the three outcomes an expectation states. */
export type Outcome = "message" | "dropped" | "error";

/**
 * The outcome an expectation states. Three-way, never three independent fields: dolphin
 * rejects the combinations, so a form that offers them as checkboxes is offering the
 * user a file that will not load.
 */
export function outcomeOf(expect: Expectation | undefined): Outcome {
  if (expect?.error) return "error";
  if (expect?.dropped) return "dropped";
  return "message";
}

/** Whether a message matcher asserts anything at all. */
export function isEmptyExpect(m: MessageExpect | undefined): boolean {
  return !m || (m.body === undefined && !hasKeys(m.vars) && !m.that?.length);
}

/** One crossing of a spied block: what went in, and what came out. */
export interface RecordExpect {
  /** Always available, even for a block that failed: the spy clones before running. */
  input?: MessageExpect;
  /** `output`, `dropped` and `error` are the three ways out; a crossing has exactly one. */
  output?: MessageExpect;
  dropped?: boolean;
  error?: string;
}

/** What one watched block should have seen. */
export interface SpyExpect {
  /**
   * The exact number of crossings. Optional because absent means "not asserted" — but
   * `0` is an assertion ("this block never ran"), so never coerce one to the other.
   */
  count?: number;
  /** Positional: the i-th crossing. Asserting on fewer than happened is fine. */
  records?: RecordExpect[];
}

/** One scenario: what to send, what to stand in for, and what should have happened. */
export interface SuiteCase {
  name: string;
  input?: CaseInput;
  /**
   * Overrides the file's, per address and WHOLE-SPEC — see {@link mocksFor}. Replacing
   * rather than merging is dolphin's rule, and a bridge that merged instead would give
   * the canvas a different run than `dolphin test`.
   */
  mocks?: Record<string, MockSpec>;
  /** Overrides the file's, per VARIABLE — not whole-map, because a variable is a scalar. */
  env?: Record<string, string>;
  /** An absent expectation still asserts something: that the flow completed. */
  expect?: Expectation;
  spies?: Record<string, SpyExpect>;
  /** A Go duration, e.g. `30s`. */
  timeout?: string;
  /** A reason. The case is reported as skipped and never run. */
  skip?: string;
}

/** One `_test.yaml`: the flow under test, and the cases that exercise it. */
export interface Suite {
  /** The flow under test. Every case in the file calls it. */
  flow: string;
  /** Named messages the cases share, so "a vip order" is written once. */
  inputs?: Record<string, SuiteInput>;
  /** Stand-ins for blocks in every case, unless a case overrides the address. */
  mocks?: Record<string, MockSpec>;
  /**
   * Environment every case runs with.
   *
   * Not a nicety. A config's env is resolved when it is LOADED, before any block runs,
   * so a flow whose connector reads `${ANTHROPIC_API_KEY}` cannot even be built without
   * one — and mocking the block that uses it does not help. Without this, the suites you
   * most want are the ones you cannot write.
   */
  env?: Record<string, string>;
  /** A Go duration bounding every case; a case may shorten or lengthen it. */
  timeout?: string;
  cases: SuiteCase[];
}

/** An empty suite for a flow — what "add tests" starts from. */
export function emptySuite(flow: string): Suite {
  return { flow, cases: [] };
}

/**
 * Whether a string is a Go duration (`30s`, `1m30s`, `500ms`). TypeScript has no parser
 * for these, and the value goes to Go verbatim, so the form checks the spelling here
 * rather than letting dolphin refuse the whole file over one field.
 */
export function isValidDuration(value: string): boolean {
  return /^[-+]?(\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)((\d+(\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h))*$/.test(
    value.trim(),
  );
}

function hasKeys(record: Record<string, unknown> | undefined): boolean {
  return !!record && Object.keys(record).length > 0;
}
