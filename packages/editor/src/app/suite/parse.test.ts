import { describe, expect, it } from "vitest";
import { parseSuite } from "./parse";

const messages = (content: string) => parseSuite(content).issues.map((i) => i.message);

describe("parseSuite", () => {
  it("reads a whole suite", () => {
    const { suite, issues } = parseSuite(`
flow: orders
inputs:
  vip:
    data:
      amount: 1000
    vars:
      tier: gold
mocks:
  orders.charge:
    default:
      body: {ok: true}
env:
  STRIPE_KEY: sk_test_fake
timeout: 1m
cases:
  - name: a vip order is charged
    input: vip
    mocks:
      orders.charge:
        default:
          error: card declined
    env:
      STRIPE_KEY: sk_test_other
    expect:
      body: {status: declined}
      vars: {tier: gold}
      that: ["body.status == 'declined'"]
    spies:
      orders.charge:
        count: 1
        records:
          - input: {body: {amount: 1000}}
    timeout: 5s
`);

    expect(issues).toEqual([]);
    expect(suite).toEqual({
      flow: "orders",
      inputs: { vip: { data: { amount: 1000 }, vars: { tier: "gold" } } },
      mocks: { "orders.charge": { default: { body: { ok: true } } } },
      env: { STRIPE_KEY: "sk_test_fake" },
      timeout: "1m",
      cases: [
        {
          name: "a vip order is charged",
          input: "vip",
          mocks: { "orders.charge": { default: { error: "card declined" } } },
          env: { STRIPE_KEY: "sk_test_other" },
          expect: {
            body: { status: "declined" },
            vars: { tier: "gold" },
            that: ["body.status == 'declined'"],
          },
          spies: {
            "orders.charge": { count: 1, records: [{ input: { body: { amount: 1000 } } }] },
          },
          timeout: "5s",
        },
      ],
    });
  });

  // A null is the un-mock, and it has to survive as a null: coerced to `{}` or dropped, it
  // would turn "run the real block here" back into the file's mock — silently, which is
  // the failure the null was added to end.
  it("keeps a case's null mock as a null", () => {
    const { suite, issues } = parseSuite(`
flow: orders
mocks:
  orders.charge:
    default:
      body: {ok: true}
cases:
  - name: the real block runs here
    mocks:
      orders.charge: null
`);

    expect(issues).toEqual([]);
    expect(suite.cases[0].mocks).toEqual({ "orders.charge": null });
  });

  it("takes an inline input as well as a named one", () => {
    const { suite, issues } = parseSuite(`
flow: orders
cases:
  - name: inline
    input:
      data: {who: world}
`);

    expect(issues).toEqual([]);
    expect(suite.cases[0].input).toEqual({ data: { who: "world" } });
  });

  // The tab renders a file the user is halfway through writing; refusing one would leave
  // them staring at an error with no way back to the form.
  it("never throws, whatever it is handed", () => {
    for (const bad of ["", "   ", "\t- [", "just a string", "42", "- a\n- b"]) {
      expect(() => parseSuite(bad), JSON.stringify(bad)).not.toThrow();
    }
  });

  it("reports unreadable YAML as an issue rather than throwing", () => {
    expect(messages("flow: orders\n  bad indent: [")[0]).toMatch(/not valid YAML/);
  });

  it("reads an empty document as an empty suite", () => {
    expect(parseSuite("")).toEqual({ suite: { flow: "", cases: [] }, issues: [] });
    expect(parseSuite("# just a comment\n").issues).toEqual([]);
  });

  // dolphin decodes with KnownFields(true): a misspelled `spys:` watches nothing,
  // asserts nothing, and goes green. Silently dropping the key here would author a file
  // dolphin then refuses to load at all.
  it("reports the unknown keys dolphin would refuse", () => {
    expect(messages("flow: orders\nflows: x\ncases: []\n")).toContainEqual(
      expect.stringContaining('unknown field "flows"'),
    );
    expect(messages("flow: orders\ncases:\n  - name: a\n    spys: {}\n")).toContainEqual(
      expect.stringContaining('unknown field "spys"'),
    );
    expect(
      messages("flow: orders\ncases:\n  - name: a\n    expect:\n      bodyy: 1\n"),
    ).toContainEqual(expect.stringContaining('unknown field "bodyy"'));
    expect(messages("flow: orders\ncases:\n  - name: a\n    input:\n      daat: 1\n")).toContainEqual(
      expect.stringContaining('unknown field "daat"'),
    );
    expect(
      messages("flow: orders\ninputs:\n  vip:\n    dta: 1\ncases:\n  - name: a\n"),
    ).toContainEqual(expect.stringContaining('unknown field "dta"'));
    expect(
      messages("flow: orders\ncases:\n  - name: a\n    spies:\n      x:\n        cont: 1\n"),
    ).toContainEqual(expect.stringContaining('unknown field "cont"'));
    expect(
      messages(
        "flow: orders\ncases:\n  - name: a\n    spies:\n      x:\n        records:\n          - outpt: {}\n",
      ),
    ).toContainEqual(expect.stringContaining('unknown field "outpt"'));
  });

  // A mock's keys are checked as strictly as everything else. Passing them through
  // unchecked would leave a typo'd `deflt:` to reach the user as a load failure.
  it("reports unknown keys inside a mock", () => {
    expect(
      messages("flow: o\nmocks:\n  o.c:\n    deflt:\n      drop: true\ncases: []\n"),
    ).toContainEqual(expect.stringContaining('unknown field "deflt"'));
    expect(
      messages(
        "flow: o\ncases:\n  - name: a\n    mocks:\n      o.c:\n        default:\n          bdy: 1\n",
      ),
    ).toContainEqual(expect.stringContaining('unknown field "bdy"'));
    expect(
      messages(
        "flow: o\nmocks:\n  o.c:\n    cases:\n      - whn: 'true'\n        body: 1\ncases: []\n",
      ),
    ).toContainEqual(expect.stringContaining('unknown field "whn"'));
  });

  // `count: 0` asserts the block never ran. Treating it as absent would silently drop
  // the assertion — the exact bug the Go type uses a *int to avoid.
  it("keeps a spy count of zero", () => {
    const { suite } = parseSuite(
      "flow: orders\ncases:\n  - name: a\n    spies:\n      orders.charge:\n        count: 0\n",
    );
    expect(suite.cases[0].spies?.["orders.charge"].count).toBe(0);
  });

  // A null body is a real assertion: the flow returned a message whose body was null.
  it("keeps a null body, and omits an absent one", () => {
    const withNull = parseSuite("flow: o\ncases:\n  - name: a\n    expect:\n      body: null\n");
    expect(withNull.suite.cases[0].expect).toEqual({ body: null });
    const without = parseSuite("flow: o\ncases:\n  - name: a\n    expect:\n      dropped: true\n");
    expect(without.suite.cases[0].expect).toEqual({ dropped: true });
  });

  it("stringifies env values YAML gave a non-string type", () => {
    const { suite } = parseSuite("flow: o\nenv:\n  PORT: 8080\n  DEBUG: true\ncases: []\n");
    expect(suite.env).toEqual({ PORT: "8080", DEBUG: "true" });
  });

  it("reports a `cases` that is not a list", () => {
    expect(messages("flow: orders\ncases: nope\n")).toContainEqual(
      "`cases` is a list of cases",
    );
  });

  it("reports a case input that is neither a name nor a mapping", () => {
    expect(messages("flow: o\ncases:\n  - name: a\n    input: [1, 2]\n")).toContainEqual(
      expect.stringContaining("the name of a shared input, or an inline"),
    );
  });

  it("carries the semantic issues validate.ts finds", () => {
    expect(messages("cases: []\n")).toContain(
      "needs a `flow`: the flow every case in the file calls",
    );
  });
});
