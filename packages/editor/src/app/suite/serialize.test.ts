import { describe, expect, it } from "vitest";
import YAML from "yaml";
import { serializeSuite } from "./serialize";
import { parseSuite } from "./parse";
import type { Suite } from "./types";

describe("serializeSuite", () => {
  // The struct order in testfile.go, not anything nicer: a hand-written suite and a
  // form-written one should read the same, so a diff is about what changed rather than
  // who wrote it.
  it("emits dolphin's key order", () => {
    const yaml = serializeSuite({
      flow: "orders",
      timeout: "1m",
      env: { A: "1" },
      mocks: { "orders.charge": { default: { drop: true } } },
      inputs: { vip: { data: 1 } },
      cases: [
        {
          skip: "flaky",
          timeout: "5s",
          spies: { "orders.charge": { count: 1 } },
          expect: { body: 1 },
          env: { B: "2" },
          mocks: { "orders.notify": { default: { drop: true } } },
          input: "vip",
          name: "one",
        },
      ],
    });

    expect(topLevelKeys(yaml)).toEqual(["flow", "inputs", "mocks", "env", "timeout", "cases"]);
    expect(caseKeys(yaml)).toEqual([
      "name",
      "input",
      "mocks",
      "env",
      "expect",
      "spies",
      "timeout",
      "skip",
    ]);
  });

  it("omits everything empty", () => {
    expect(serializeSuite({ flow: "orders", cases: [{ name: "a" }] })).toBe(
      "flow: orders\ncases:\n  - name: a\n",
    );
    // Empty containers are not assertions; emitting them is noise a reader has to read.
    expect(
      serializeSuite({
        flow: "orders",
        inputs: {},
        mocks: {},
        env: {},
        cases: [{ name: "a", expect: {}, spies: {}, mocks: {}, env: {} }],
      }),
    ).toBe("flow: orders\ncases:\n  - name: a\n");
  });

  // An absent `expect:` still asserts that the flow completed and did not fail, so an
  // empty mapping would add noise without adding meaning — but a body of null, or a
  // count of zero, are real assertions and must survive.
  it("keeps the falsy values that are assertions", () => {
    const yaml = serializeSuite({
      flow: "o",
      cases: [
        { name: "null body", expect: { body: null } },
        { name: "never ran", spies: { "o.c": { count: 0 } } },
      ],
    });

    expect(yaml).toContain("body: null");
    expect(yaml).toContain("count: 0");
    expect(parseSuite(yaml).suite.cases[0].expect).toEqual({ body: null });
    expect(parseSuite(yaml).suite.cases[1].spies?.["o.c"].count).toBe(0);
  });

  it("writes an inline input as a mapping and a shared one as a name", () => {
    const yaml = serializeSuite({
      flow: "o",
      inputs: { vip: { data: { amount: 1 } } },
      cases: [
        { name: "shared", input: "vip" },
        { name: "inline", input: { data: { amount: 2 } } },
      ],
    });

    expect(yaml).toContain("input: vip");
    expect(parseSuite(yaml).suite.cases[1].input).toEqual({ data: { amount: 2 } });
  });

  it("round-trips a suite that uses every part of the format", () => {
    const suite: Suite = {
      flow: "orders",
      inputs: { vip: { data: { amount: 1000 }, vars: { tier: "gold" } } },
      mocks: { "orders.charge": { cases: [{ when: "body.amount > 0", body: { ok: true } }] } },
      env: { STRIPE_KEY: "sk_test_fake" },
      timeout: "1m",
      cases: [
        {
          name: "charged",
          input: "vip",
          mocks: { "orders.charge": { default: { error: "declined" } } },
          env: { STRIPE_KEY: "sk_other" },
          expect: { body: { status: "ok" }, vars: { tier: "gold" }, that: ["body.status != ''"] },
          spies: {
            "orders.charge": {
              count: 1,
              records: [{ input: { body: { amount: 1000 } }, output: { body: { ok: true } } }],
            },
          },
          timeout: "5s",
        },
        { name: "dropped", expect: { dropped: true } },
        { name: "failed", expect: { error: "declined" } },
        { name: "skipped", skip: "pending a fix" },
      ],
    };

    const reparsed = parseSuite(serializeSuite(suite));
    expect(reparsed.issues).toEqual([]);
    expect(reparsed.suite).toEqual(suite);
  });
});

/** The top-level mapping keys, in emitted order (YAML.parse preserves insertion order). */
function topLevelKeys(yaml: string): string[] {
  return Object.keys(YAML.parse(yaml) as Record<string, unknown>);
}

/** The first case's keys, in emitted order. */
function caseKeys(yaml: string): string[] {
  const cases = (YAML.parse(yaml) as { cases: Record<string, unknown>[] }).cases;
  return Object.keys(cases[0]);
}
