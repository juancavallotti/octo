import { describe, expect, it } from "vitest";
import { defaultNamespace, nameProblem } from "./names";

describe("nameProblem", () => {
  it.each(["staging", "qa", "eu-west-1", "a"])("accepts %s", (name) => {
    expect(nameProblem(name)).toBeNull();
  });

  it.each([
    ["octo", /default/],
    ["Staging", /Lowercase/],
    ["-qa", /Lowercase/],
    ["qa-", /Lowercase/],
    ["a".repeat(31), /30/],
  ])("refuses %s", (name, why) => {
    expect(nameProblem(name)).toMatch(why);
  });

  it("says nothing about an empty field", () => {
    expect(nameProblem("")).toBeNull();
  });
});

describe("defaultNamespace", () => {
  it("prefixes the name", () => {
    expect(defaultNamespace("staging")).toBe("octo-staging");
  });
});
