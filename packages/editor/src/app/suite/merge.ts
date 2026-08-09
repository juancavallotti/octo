/**
 * How a case resolves against the file it lives in: `File.InputFor`, `File.MocksFor`,
 * `File.EnvFor` and `File.TimeoutFor` from runtime/dolphin/internal/suite/testfile.go.
 *
 * This is not a convenience layer — it is the bridge's correctness. The canvas ▶ menu
 * runs a suite case by resolving it here and handing the result to the run transport, so
 * if these rules drift from dolphin's, a scenario passes on the canvas and fails in CI
 * (or worse, the other way round) with nothing to point at.
 */

import type { MockSpec, Suite, SuiteCase, SuiteInput } from "./types";
import { isSharedInput } from "./types";

/** The default case timeout, matching `octo invoke`'s own (suite.DefaultCaseTimeout). */
export const DEFAULT_CASE_TIMEOUT = "30s";

/**
 * A case's input, resolved against the file's shared ones. A case with no input at all
 * calls the flow with an empty message, which is a legitimate thing to test.
 *
 * An input name the file does not declare resolves to empty here; validate.ts reports it
 * as the load error dolphin would raise, so the form can say so before a run.
 */
export function inputFor(suite: Suite, c: SuiteCase): SuiteInput {
  if (c.input === undefined) return {};
  if (isSharedInput(c.input)) return suite.inputs?.[c.input] ?? {};
  return c.input;
}

/**
 * The mocks one case runs under: the file's, with the case's REPLACING them address by
 * address — whole-spec, not deep-merged.
 *
 * That is dolphin's rule, and it is the one thing in this file most likely to be
 * "improved" into a deep merge. It must not be: a mock a case declares says exactly what
 * that block will do, without the reader having to hold the file's version in their head
 * too. A merged run would also diverge from `dolphin test` silently — the same suite,
 * two verdicts.
 *
 * A `null` on a case REMOVES the file's mock for that address, so the real block runs.
 */
export function mocksFor(suite: Suite, c: SuiteCase): Record<string, MockSpec> {
  const merged: Record<string, MockSpec> = {};
  for (const [address, spec] of Object.entries(suite.mocks ?? {})) {
    // A null at the FILE level is not an un-mock — there is nothing to lift. validate.ts
    // reports it; here it is simply not a mock.
    if (spec) merged[address] = spec;
  }
  for (const [address, spec] of Object.entries(c.mocks ?? {})) {
    if (spec === null) delete merged[address];
    else merged[address] = spec;
  }
  return merged;
}

/**
 * The environment a case runs with: the file's, with the case's laid over it VARIABLE by
 * variable — unlike mocks, because a variable is a scalar and a case that needs one
 * value different should not have to restate the other five to get it.
 */
export function envFor(suite: Suite, c: SuiteCase): Record<string, string> {
  return { ...suite.env, ...c.env };
}

/** How long a case may take: its own, else the file's, else the default. */
export function timeoutFor(suite: Suite, c: SuiteCase): string {
  return c.timeout || suite.timeout || DEFAULT_CASE_TIMEOUT;
}

/** The addresses a case watches, sorted so a run is reproducible (suite.SpiedBy). */
export function spiedBy(c: SuiteCase): string[] {
  return Object.keys(c.spies ?? {}).sort();
}
