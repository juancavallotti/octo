import { describe, expect, it } from "vitest";
import { DEFAULT_CASE_TIMEOUT, envFor, inputFor, mocksFor, spiedBy, timeoutFor } from "./merge";
import type { Suite } from "./types";

const suite: Suite = {
  flow: "orders",
  inputs: { vip: { data: { amount: 1000 } } },
  mocks: {
    "orders.charge": { default: { body: { ok: true } } },
    "orders.notify": { default: { drop: true } },
  },
  env: { STRIPE_KEY: "sk_file", LOG_LEVEL: "error" },
  timeout: "1m",
  cases: [],
};

describe("inputFor", () => {
  it("resolves a named input, an inline one, and none at all", () => {
    expect(inputFor(suite, { name: "a", input: "vip" })).toEqual({ data: { amount: 1000 } });
    expect(inputFor(suite, { name: "a", input: { data: { x: 1 } } })).toEqual({
      data: { x: 1 },
    });
    // A case with no input calls the flow with an empty message, which is a legitimate
    // thing to test.
    expect(inputFor(suite, { name: "a" })).toEqual({});
  });

  it("resolves an undeclared name to empty rather than throwing", () => {
    // validate.ts reports this as the load error dolphin would raise; the form still
    // has to render while the user is mid-typo.
    expect(inputFor(suite, { name: "a", input: "nope" })).toEqual({});
  });
});

describe("mocksFor", () => {
  // dolphin's rule, and the one most likely to be "improved" into a deep merge. It must
  // not be: a merged run would diverge from `dolphin test` silently.
  it("replaces a file mock whole-spec, not field by field", () => {
    const merged = mocksFor(suite, {
      name: "a",
      mocks: { "orders.charge": { default: { error: "declined" } } },
    });

    expect(merged["orders.charge"]).toEqual({ default: { error: "declined" } });
    expect(merged["orders.charge"].default?.body).toBeUndefined();
    // Addresses the case did not mention keep the file's mock.
    expect(merged["orders.notify"]).toEqual({ default: { drop: true } });
  });

  it("keeps the file's mocks when a case declares none, and vice versa", () => {
    expect(mocksFor(suite, { name: "a" })).toEqual(suite.mocks);
    expect(
      mocksFor({ flow: "o", cases: [] }, { name: "a", mocks: { x: { default: {} } } }),
    ).toEqual({ x: { default: {} } });
    expect(mocksFor({ flow: "o", cases: [] }, { name: "a" })).toEqual({});
  });

  it("does not mutate the file's mocks", () => {
    mocksFor(suite, { name: "a", mocks: { "orders.charge": { default: { drop: true } } } });
    expect(suite.mocks!["orders.charge"]).toEqual({ default: { body: { ok: true } } });
  });
});

describe("envFor", () => {
  // Per variable, unlike mocks: a variable is a scalar, so a case that needs one value
  // different should not have to restate the other five to get it.
  it("lays a case's variables over the file's, one at a time", () => {
    expect(envFor(suite, { name: "a", env: { STRIPE_KEY: "sk_case" } })).toEqual({
      STRIPE_KEY: "sk_case",
      LOG_LEVEL: "error",
    });
  });
});

describe("timeoutFor", () => {
  it("prefers the case's, then the file's, then the default", () => {
    expect(timeoutFor(suite, { name: "a", timeout: "5s" })).toBe("5s");
    expect(timeoutFor(suite, { name: "a" })).toBe("1m");
    expect(timeoutFor({ flow: "o", cases: [] }, { name: "a" })).toBe(DEFAULT_CASE_TIMEOUT);
  });
});

describe("spiedBy", () => {
  // Sorted so two runs of the same suite spawn octo with identical arguments, which is
  // what makes the command a failure prints actually be the command that failed.
  it("returns the watched addresses, sorted", () => {
    expect(spiedBy({ name: "a", spies: { "o.z": {}, "o.a": {} } })).toEqual(["o.a", "o.z"]);
    expect(spiedBy({ name: "a" })).toEqual([]);
  });
});
