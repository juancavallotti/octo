import { describe, expect, it } from "vitest";
import { fromStoredCase, toStoredCase } from "./mocks";

describe("mock case conversion", () => {
  it("preserves vars as an object when stored JSON parses to one", () => {
    expect(fromStoredCase({ body: "{}", vars: '{"tier":"gold"}' })).toEqual({
      body: {},
      vars: { tier: "gold" },
    });
  });

  it("keeps raw vars text when stored JSON is not a plain object", () => {
    expect(fromStoredCase({ vars: "42" })).toEqual({ vars: "42" });
    expect(fromStoredCase({ vars: "[1,2]" })).toEqual({ vars: "[1,2]" });
    expect(fromStoredCase({ vars: "null" })).toEqual({ vars: "null" });
  });

  it("round-trips object vars back to stored JSON text", () => {
    expect(toStoredCase({ vars: { tier: "gold" } })).toEqual({
      vars: '{\n  "tier": "gold"\n}',
    });
  });
});
