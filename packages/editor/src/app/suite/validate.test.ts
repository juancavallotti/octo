import { describe, expect, it } from "vitest";
import { validateSuite } from "./validate";
import type { Suite, SuiteCase } from "./types";

const suite = (over: Partial<Suite> = {}): Suite => ({
  flow: "orders",
  cases: [{ name: "a" }],
  ...over,
});

const withCase = (c: SuiteCase, over: Partial<Suite> = {}) =>
  validateSuite(suite({ cases: [c], ...over })).map((i) => i.message);

describe("validateSuite", () => {
  it("passes a suite dolphin would load", () => {
    expect(validateSuite(suite())).toEqual([]);
  });

  it("requires a flow and at least one case", () => {
    expect(validateSuite(suite({ flow: "  " })).map((i) => i.message)).toContain(
      "needs a `flow`: the flow every case in the file calls",
    );
    expect(validateSuite(suite({ cases: [] })).map((i) => i.message)).toContain(
      "has no cases yet",
    );
  });

  // The name is what a report, a JUnit XML and a person all identify a case by.
  it("requires every case to be named, and uniquely", () => {
    expect(withCase({ name: "" })).toContain("case 0 needs a name");
    expect(
      validateSuite(suite({ cases: [{ name: "a" }, { name: "a" }] })).map((i) => i.message),
    ).toContain('two cases are named "a"');
  });

  it("flags duplicate case names that differ only by surrounding whitespace", () => {
    const issues = validateSuite({
      flow: "orders",
      cases: [{ name: "same" }, { name: "  same  " }],
    });

    expect(issues.map((i) => i.message)).toContain('two cases are named "  same  "');
  });

  it("points an issue at the case it belongs to", () => {
    const issues = validateSuite(suite({ cases: [{ name: "ok" }, { name: "" }] }));
    expect(issues).toEqual([
      { message: "case 1 needs a name", severity: "error", caseIndex: 1 },
    ]);
  });

  it("rejects a case naming an input the file does not declare", () => {
    expect(withCase({ name: "a", input: "vip" })).toContain(
      'input "vip" is not declared in `inputs` (have: none)',
    );
    expect(
      withCase({ name: "a", input: "vip" }, { inputs: { std: {}, other: {} } }),
    ).toContain('input "vip" is not declared in `inputs` (have: other, std)');
    expect(withCase({ name: "a", input: "vip" }, { inputs: { vip: {} } })).toEqual([]);
  });

  // A case expecting a body AND a failure is not a stricter test, it is an impossible
  // one: under `error` the flow returned no message for a body to be compared against.
  it("pins the one-outcome rule on an expectation", () => {
    expect(withCase({ name: "a", expect: { dropped: true, error: "boom" } })).toContain(
      "expects both `dropped` and `error`: a flow that failed did not filter the message out",
    );
    expect(withCase({ name: "a", expect: { error: "boom", body: { x: 1 } } })).toContain(
      "expects `error` as well as the message the flow returned, but a flow that failed returned none",
    );
    expect(withCase({ name: "a", expect: { dropped: true, that: ["true"] } })).toContain(
      "expects `dropped` as well as the message the flow returned, but a flow that dropped the message returned none",
    );
    // Each on its own is fine.
    expect(withCase({ name: "a", expect: { dropped: true } })).toEqual([]);
    expect(withCase({ name: "a", expect: { error: "boom" } })).toEqual([]);
    expect(withCase({ name: "a", expect: { body: { x: 1 }, vars: { y: 2 } } })).toEqual([]);
  });

  it("checks a spy's count and records", () => {
    expect(withCase({ name: "a", spies: { "o.c": { count: -1 } } })).toContain(
      'spy "o.c": count is negative',
    );
    expect(
      withCase({ name: "a", spies: { "o.c": { count: 1, records: [{}, {}] } } }),
    ).toContain('spy "o.c": asserts on 2 records but expects a count of 1');
    expect(withCase({ name: "a", spies: { "  ": {} } })).toContain(
      "a spy has an empty block address",
    );
    // Asserting on fewer crossings than the count is fine.
    expect(withCase({ name: "a", spies: { "o.c": { count: 3, records: [{}] } } })).toEqual(
      [],
    );
  });

  // count: 0 is an assertion ("this block never ran"), not an absent one.
  it("accepts a spy count of zero", () => {
    expect(withCase({ name: "a", spies: { "o.c": { count: 0 } } })).toEqual([]);
  });

  // Unlike an expectation, a record's input is always fair game: the spy clones the
  // message before running the block, so even a block that failed shows what it carried.
  it("pins the one-outcome rule on a spied crossing, but not on its input", () => {
    expect(
      withCase({
        name: "a",
        spies: { "o.c": { records: [{ output: { body: 1 }, error: "boom" }] } },
      }),
    ).toContain(
      'spy "o.c": record 0 expects more than one outcome (output, error): a block either returns a message, fails, or drops it',
    );
    expect(
      withCase({
        name: "a",
        spies: { "o.c": { records: [{ input: { body: 1 }, error: "boom" }] } },
      }),
    ).toEqual([]);
  });

  it("checks the spelling of a duration", () => {
    expect(validateSuite(suite({ timeout: "30 seconds" })).map((i) => i.message)).toContain(
      'timeout "30 seconds" is not a duration (e.g. 30s, 1m30s)',
    );
    expect(withCase({ name: "a", timeout: "quick" })).toContain(
      'timeout "quick" is not a duration (e.g. 30s, 1m30s)',
    );
    expect(validateSuite(suite({ timeout: "1m30s" }))).toEqual([]);
  });

  // dolphin runs the engine's own core.NewMocks before it spawns anything, so a bad
  // spec is a load error that takes the whole file down with it.
  it("checks mocks against the engine's rules, at both levels", () => {
    expect(withCase({ name: "a", mocks: { "o.c": {} } })).toContain(
      'mock "o.c": needs at least one case, or a default',
    );
    expect(
      validateSuite(suite({ mocks: { "o.c": { cases: [{ body: 1 }] } } })).map((i) => i.message),
    ).toContain(
      'mock "o.c": case 0 needs a `when` expression (a case with no condition is the `default`)',
    );
    expect(
      withCase({ name: "a", mocks: { "o.c": { default: { when: "true", body: 1 } } } }),
    ).toContain(
      'mock "o.c": the default must not have a `when` expression: it is what runs when no case matched',
    );
    expect(withCase({ name: "a", mocks: { "  ": { default: { drop: true } } } })).toContain(
      "a mock has an empty block address",
    );
  });

  // A null on a case lifts the file's mock, so that one case runs the real block. Over an
  // address the file does not mock it does nothing at all — which is why it is refused
  // rather than shrugged at: a typo'd address would read as "the real block runs here"
  // while the mock it meant to lift stayed on, and the case would pass proving the
  // opposite of what it says.
  it("takes a null over an inherited mock, and refuses one over anything else", () => {
    const mocked = { mocks: { "o.c": { default: { body: 1 } } } };

    expect(withCase({ name: "a", mocks: { "o.c": null } }, mocked)).toEqual([]);
    expect(withCase({ name: "a", mocks: { "o.charge": null } }, mocked)).toContain(
      `mock "o.charge" is null, which lifts the file's mock for that block — but the file does not mock it (it mocks: o.c)`,
    );
    expect(withCase({ name: "a", mocks: { "o.c": null } })).toContain(
      `mock "o.c" is null, which lifts the file's mock for that block — but the file does not mock it (it mocks: nothing)`,
    );
  });

  // The file has nothing above it to lift, and dolphin decodes a null there as a spec that
  // does nothing — so it is the ordinary empty-spec rejection, not an un-mock.
  it("refuses a null in the file's own mocks", () => {
    expect(validateSuite(suite({ mocks: { "o.c": null } })).map((i) => i.message)).toContain(
      "mock \"o.c\": needs at least one case, or a default — only a case may be null, to lift the file's mock",
    );
  });

  it("pins the one-outcome rule on a mock case", () => {
    expect(withCase({ name: "a", mocks: { "o.c": { default: {} } } })).toContain(
      "mock \"o.c\": default: needs an outcome: one of `body`, `error` or `drop`",
    );
    expect(
      withCase({ name: "a", mocks: { "o.c": { default: { body: 1, drop: true } } } }),
    ).toContain(
      'mock "o.c": default: has more than one outcome (body, drop): a block either returns a message, fails, or drops it',
    );
    // vars is scratch alongside a body; a block that failed or dropped returned none.
    expect(
      withCase({ name: "a", mocks: { "o.c": { default: { error: "x", vars: { a: 1 } } } } }),
    ).toContain(
      "mock \"o.c\": default: `vars` only goes with a `body`: a block that failed or dropped returned no message",
    );
    expect(
      withCase({ name: "a", mocks: { "o.c": { default: { body: 1, vars: { a: 1 } } } } }),
    ).toEqual([]);
  });

  it("reports every problem, not just the first", () => {
    const issues = validateSuite({
      flow: "",
      cases: [{ name: "" }, { name: "b", input: "nope" }],
    });
    expect(issues.map((i) => i.message)).toEqual([
      "needs a `flow`: the flow every case in the file calls",
      "case 0 needs a name",
      'input "nope" is not declared in `inputs` (have: none)',
    ]);
  });
});
