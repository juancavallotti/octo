/**
 * YAML → {@link Suite}, tolerantly.
 *
 * parseSuite never throws. The Testing tab renders a file the user is halfway through
 * writing, and a parser that refuses one would leave them staring at an error with no
 * way back to the form. So it takes what it can read and reports the rest as issues.
 *
 * Two kinds of issue come out of here: what YAML itself could not read, and the keys
 * dolphin's KnownFields(true) decoding would reject. The second is the one worth the
 * code — a typo'd key that is silently ignored looks exactly like a passing test, so the
 * editor has to be as strict as dolphin or it will cheerfully author a file dolphin will
 * not load. The semantic rules live next door in validate.ts.
 */

import YAML from "yaml";
import type {
  CaseInput,
  Expectation,
  MessageExpect,
  MockSpec,
  RecordExpect,
  SpyExpect,
  Suite,
  SuiteCase,
  SuiteInput,
} from "./types";
import {
  CASE_KEYS,
  EXPECT_KEYS,
  INPUT_KEYS,
  RECORD_KEYS,
  MOCK_CASE_KEYS,
  MOCK_KEYS,
  SPY_KEYS,
  SUITE_KEYS,
  validateSuite,
  type SuiteIssue,
} from "./validate";

export interface ParsedSuite {
  suite: Suite;
  issues: SuiteIssue[];
}

/** Read a suite, reporting rather than throwing. */
export function parseSuite(content: string): ParsedSuite {
  const issues: SuiteIssue[] = [];
  let doc: unknown;
  try {
    doc = YAML.parse(content);
  } catch (err) {
    return {
      suite: { flow: "", cases: [] },
      issues: [
        {
          message: `not valid YAML: ${(err as Error).message}`,
          severity: "error",
          lossy: true,
        },
      ],
    };
  }
  if (doc === null || doc === undefined) return { suite: { flow: "", cases: [] }, issues };
  if (!isRecord(doc)) {
    return {
      suite: { flow: "", cases: [] },
      issues: [
        {
          message: "a suite is a mapping of `flow` and `cases`",
          severity: "error",
          lossy: true,
        },
      ],
    };
  }

  unknownKeys(doc, SUITE_KEYS, "", undefined, issues);
  const suite: Suite = {
    flow: str(doc.flow) ?? "",
    cases: readCases(doc.cases, issues),
  };
  const inputs = readInputs(doc.inputs, issues);
  if (inputs) suite.inputs = inputs;
  const mocks = readMocks(doc.mocks, "mock", undefined, issues);
  if (mocks) suite.mocks = mocks;
  const env = readEnv(doc.env);
  if (env) suite.env = env;
  const timeout = str(doc.timeout);
  if (timeout) suite.timeout = timeout;

  // Everything collected above is something this parser could not read, and so could not
  // put in the model — which makes it exactly the set that re-serializing would delete.
  // The rules from validateSuite are the opposite: read fine, rejected by dolphin.
  return {
    suite,
    issues: [...issues.map((i) => ({ ...i, lossy: true })), ...validateSuite(suite)],
  };
}

function readCases(raw: unknown, issues: SuiteIssue[]): SuiteCase[] {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw)) {
    issues.push({ message: "`cases` is a list of cases", severity: "error" });
    return [];
  }
  return raw.map((entry, i) => readCase(entry, i, issues));
}

function readCase(raw: unknown, i: number, issues: SuiteIssue[]): SuiteCase {
  if (!isRecord(raw)) {
    issues.push({ message: `case ${i} is not a mapping`, severity: "error", caseIndex: i });
    return { name: "" };
  }
  unknownKeys(raw, CASE_KEYS, "", i, issues);

  const c: SuiteCase = { name: str(raw.name) ?? "" };
  const input = readCaseInput(raw.input, i, issues);
  if (input !== undefined) c.input = input;
  const mocks = readMocks(raw.mocks, "mock", i, issues);
  if (mocks) c.mocks = mocks;
  const env = readEnv(raw.env);
  if (env) c.env = env;
  const expect = readExpectation(raw.expect, i, issues);
  if (expect) c.expect = expect;
  const spies = readSpies(raw.spies, i, issues);
  if (spies) c.spies = spies;
  const timeout = str(raw.timeout);
  if (timeout) c.timeout = timeout;
  const skip = str(raw.skip);
  if (skip) c.skip = skip;
  return c;
}

/** A scalar is the name of a shared input; a mapping is an inline one. */
function readCaseInput(
  raw: unknown,
  i: number,
  issues: SuiteIssue[],
): CaseInput | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (typeof raw === "string") return raw;
  if (!isRecord(raw)) {
    issues.push({
      message: `case ${i}: \`input\` is the name of a shared input, or an inline {data, vars}`,
      severity: "error",
      caseIndex: i,
    });
    return undefined;
  }
  unknownKeys(raw, INPUT_KEYS, "input", i, issues);
  return readInput(raw);
}

function readInputs(
  raw: unknown,
  issues: SuiteIssue[],
): Record<string, SuiteInput> | undefined {
  if (!isRecord(raw)) return undefined;
  const out: Record<string, SuiteInput> = {};
  for (const [name, value] of Object.entries(raw)) {
    if (!isRecord(value)) {
      issues.push({
        message: `input ${JSON.stringify(name)} is not a mapping of {data, vars}`,
        severity: "error",
      });
      continue;
    }
    unknownKeys(value, INPUT_KEYS, `input ${JSON.stringify(name)}`, undefined, issues);
    out[name] = readInput(value);
  }
  return Object.keys(out).length ? out : undefined;
}

function readInput(raw: Record<string, unknown>): SuiteInput {
  const input: SuiteInput = {};
  if (raw.data !== undefined) input.data = raw.data;
  const vars = record(raw.vars);
  if (vars) input.vars = vars;
  return input;
}

function readExpectation(
  raw: unknown,
  i: number,
  issues: SuiteIssue[],
): Expectation | undefined {
  if (!isRecord(raw)) return undefined;
  unknownKeys(raw, EXPECT_KEYS, "expect", i, issues);
  const expect: Expectation = readMessageExpect(raw);
  if (raw.dropped === true) expect.dropped = true;
  const error = str(raw.error);
  if (error) expect.error = error;
  return expect;
}

function readMessageExpect(raw: Record<string, unknown>): MessageExpect {
  const m: MessageExpect = {};
  if (raw.body !== undefined) m.body = raw.body;
  const vars = record(raw.vars);
  if (vars) m.vars = vars;
  const that = strings(raw.that);
  if (that) m.that = that;
  return m;
}

function readSpies(
  raw: unknown,
  i: number,
  issues: SuiteIssue[],
): Record<string, SpyExpect> | undefined {
  if (!isRecord(raw)) return undefined;
  const out: Record<string, SpyExpect> = {};
  for (const [address, value] of Object.entries(raw)) {
    if (!isRecord(value)) {
      issues.push({
        message: `spy ${JSON.stringify(address)} is not a mapping of {count, records}`,
        severity: "error",
        caseIndex: i,
      });
      continue;
    }
    unknownKeys(value, SPY_KEYS, `spy ${JSON.stringify(address)}`, i, issues);
    const spy: SpyExpect = {};
    // `0` is an assertion — "this block never ran" — so it must survive as a value.
    if (typeof value.count === "number") spy.count = value.count;
    const records = readRecords(value.records, address, i, issues);
    if (records) spy.records = records;
    out[address] = spy;
  }
  return Object.keys(out).length ? out : undefined;
}

function readRecords(
  raw: unknown,
  address: string,
  i: number,
  issues: SuiteIssue[],
): RecordExpect[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  return raw.map((entry) => {
    if (!isRecord(entry)) {
      issues.push({
        message: `spy ${JSON.stringify(address)}: a record is a mapping`,
        severity: "error",
        caseIndex: i,
      });
      return {};
    }
    unknownKeys(entry, RECORD_KEYS, `spy ${JSON.stringify(address)}`, i, issues);
    const record: RecordExpect = {};
    if (isRecord(entry.input)) record.input = readMessageExpect(entry.input);
    if (isRecord(entry.output)) record.output = readMessageExpect(entry.output);
    if (entry.dropped === true) record.dropped = true;
    const error = str(entry.error);
    if (error) record.error = error;
    return record;
  });
}

/**
 * Mocks pass through as values — they are the runner's own `core.MockSpec`, which the
 * editor already round-trips through the run transport, so re-deriving the shape here
 * would be a second model to keep in step for no gain. Their keys are still checked:
 * dolphin decodes them as strictly as everything else, and a typo'd `deflt:` would
 * otherwise reach the user as a load failure rather than as the typo it is.
 */
function readMocks(
  raw: unknown,
  at: string,
  caseIndex: number | undefined,
  issues: SuiteIssue[],
): Record<string, MockSpec> | undefined {
  if (!isRecord(raw) || !Object.keys(raw).length) return undefined;
  for (const [address, spec] of Object.entries(raw)) {
    if (!isRecord(spec)) continue; // validate.ts reports the shape
    const where = `${at} ${JSON.stringify(address)}`;
    unknownKeys(spec, MOCK_KEYS, where, caseIndex, issues);
    for (const c of Array.isArray(spec.cases) ? spec.cases : []) {
      if (isRecord(c)) unknownKeys(c, MOCK_CASE_KEYS, where, caseIndex, issues);
    }
    if (isRecord(spec.default)) {
      unknownKeys(spec.default, MOCK_CASE_KEYS, where, caseIndex, issues);
    }
  }
  return raw as Record<string, MockSpec>;
}

function readEnv(raw: unknown): Record<string, string> | undefined {
  if (!isRecord(raw)) return undefined;
  const out: Record<string, string> = {};
  for (const [name, value] of Object.entries(raw)) {
    // YAML gives numbers and booleans their own types; env is strings all the way down.
    if (value !== null && value !== undefined) out[name] = String(value);
  }
  return Object.keys(out).length ? out : undefined;
}

/**
 * Report keys dolphin's decoder would reject. `at` locates the mapping ("expect", "spy
 * \"a.b\"") and is empty for the top level of a suite or a case.
 */
function unknownKeys(
  raw: Record<string, unknown>,
  allowed: string[],
  at: string,
  caseIndex: number | undefined,
  issues: SuiteIssue[],
): void {
  const where = at ? `${at}: ` : "";
  const scope = caseIndex === undefined ? "" : `case ${caseIndex}: `;
  for (const key of Object.keys(raw)) {
    if (allowed.includes(key)) continue;
    issues.push({
      message: `${scope}${where}unknown field ${JSON.stringify(key)} (dolphin will refuse the file; expected ${allowed.join(", ")})`,
      severity: "error",
      caseIndex,
    });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function record(value: unknown): Record<string, unknown> | undefined {
  return isRecord(value) && Object.keys(value).length ? value : undefined;
}

function strings(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const out = value.filter((v): v is string => typeof v === "string");
  return out.length ? out : undefined;
}
