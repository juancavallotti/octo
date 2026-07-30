import { describe, expect, it } from "vitest";
import { scaffoldSuite } from "./scaffold";
import { parseSuite } from "../../suite/parse";

describe("scaffoldSuite", () => {
  // A starter that dolphin refuses would greet every new user with an error, and the
  // scaffold is hand-written text rather than a serialized model, so nothing else
  // checks it.
  it("is a suite dolphin would load, with no problems", () => {
    const { suite, issues } = parseSuite(scaffoldSuite("orders"));

    expect(issues).toEqual([]);
    expect(suite.flow).toBe("orders");
    expect(suite.cases).toHaveLength(1);
  });

  // An empty `expect` is not an empty assertion: it says the flow completed and did not
  // fail. So a brand new suite passes, and the user sees green before writing anything.
  it("ships one case that asserts the flow completed", () => {
    const { suite } = parseSuite(scaffoldSuite("orders"));

    expect(suite.cases[0]).toEqual({ name: "it runs", expect: {} });
  });

  it("names the file dolphin will look for", () => {
    expect(scaffoldSuite("Order Intake")).toContain("dolphin test order-intake_test.yaml");
  });

  // Everything beyond the one case is commented out, so the starter teaches the format
  // without asserting anything the user did not ask for.
  it("leaves the optional sections as comments", () => {
    const text = scaffoldSuite("orders");

    for (const section of ["env:", "mocks:", "dropped: true"]) {
      const line = text.split("\n").find((l) => l.includes(section));
      expect(line?.trim().startsWith("#"), section).toBe(true);
    }
  });

  it("survives a flow name that is not an identifier", () => {
    const { suite, issues } = parseSuite(scaffoldSuite("Order Intake / v2"));

    expect(issues).toEqual([]);
    expect(suite.flow).toBe("Order Intake / v2");
  });
});
