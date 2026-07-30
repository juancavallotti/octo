/**
 * {@link Suite} → YAML, in dolphin's key order with empties omitted.
 *
 * Keys follow the struct order in runtime/dolphin/internal/suite/testfile.go rather than
 * anything nicer, because a hand-written suite and a form-written one should read the
 * same — a diff between them ought to be about what changed, not about who wrote it.
 *
 * **This does not preserve comments.** It re-emits from the model, so a suite's
 * comments and its original key order are lost. That is why the raw string stays the
 * source of truth in the Testing tab and the YAML view saves byte-for-byte: the form
 * only re-serializes once the user edits through it, after being told. Preserving them
 * would mean editing a `YAML.parseDocument` tree in place, at two to three times the
 * work; worth revisiting if it bites.
 */

import YAML from "yaml";
import type {
  Expectation,
  MessageExpect,
  RecordExpect,
  SpyExpect,
  Suite,
  SuiteCase,
  SuiteInput,
} from "./types";
import { isSharedInput } from "./types";

/** Render a suite as the YAML dolphin reads. */
export function serializeSuite(suite: Suite): string {
  const out: Record<string, unknown> = { flow: suite.flow };
  if (hasKeys(suite.inputs)) out.inputs = mapValues(suite.inputs!, plainInput);
  if (hasKeys(suite.mocks)) out.mocks = suite.mocks;
  if (hasKeys(suite.env)) out.env = suite.env;
  if (suite.timeout) out.timeout = suite.timeout;
  out.cases = suite.cases.map(plainCase);
  return YAML.stringify(out, { lineWidth: 0 });
}

function plainCase(c: SuiteCase): Record<string, unknown> {
  const out: Record<string, unknown> = { name: c.name };
  if (c.input !== undefined) {
    out.input = isSharedInput(c.input) ? c.input : plainInput(c.input);
  }
  if (hasKeys(c.mocks)) out.mocks = c.mocks;
  if (hasKeys(c.env)) out.env = c.env;
  const expect = plainExpectation(c.expect);
  if (expect) out.expect = expect;
  if (hasKeys(c.spies)) out.spies = mapValues(c.spies!, plainSpy);
  if (c.timeout) out.timeout = c.timeout;
  if (c.skip) out.skip = c.skip;
  return out;
}

function plainInput(input: SuiteInput): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (input.data !== undefined) out.data = input.data;
  if (hasKeys(input.vars)) out.vars = input.vars;
  return out;
}

/**
 * An expectation, or undefined when it asserts nothing — an absent `expect:` still
 * asserts that the flow completed and did not fail, so emitting an empty mapping would
 * add noise without adding meaning.
 */
function plainExpectation(e: Expectation | undefined): Record<string, unknown> | undefined {
  if (!e) return undefined;
  const out = plainMessageExpect(e) ?? {};
  if (e.dropped) out.dropped = true;
  if (e.error) out.error = e.error;
  return Object.keys(out).length ? out : undefined;
}

function plainMessageExpect(m: MessageExpect): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {};
  // `body: null` is a real assertion (the flow returned a null body), so only an absent
  // body is omitted.
  if (m.body !== undefined) out.body = m.body;
  if (hasKeys(m.vars)) out.vars = m.vars;
  if (m.that?.length) out.that = m.that;
  return Object.keys(out).length ? out : undefined;
}

function plainSpy(spy: SpyExpect): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  // `count: 0` asserts the block never ran; a falsy check here would drop that.
  if (spy.count !== undefined) out.count = spy.count;
  if (spy.records?.length) out.records = spy.records.map(plainRecord);
  return out;
}

function plainRecord(r: RecordExpect): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const input = r.input && plainMessageExpect(r.input);
  if (input) out.input = input;
  const output = r.output && plainMessageExpect(r.output);
  if (output) out.output = output;
  if (r.dropped) out.dropped = true;
  if (r.error) out.error = r.error;
  return out;
}

function mapValues<T, R>(
  source: Record<string, T>,
  fn: (value: T) => R,
): Record<string, R> {
  return Object.fromEntries(Object.entries(source).map(([k, v]) => [k, fn(v)]));
}

function hasKeys(record: Record<string, unknown> | undefined): boolean {
  return !!record && Object.keys(record).length > 0;
}
