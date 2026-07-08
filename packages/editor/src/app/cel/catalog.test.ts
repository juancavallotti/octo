import { describe, expect, it } from "vitest";
import {
  CEL_VARIABLES,
  OCTO_FUNCTIONS,
  CEL_BUILTINS,
  allCompletions,
  lookup,
} from "./catalog";

describe("cel catalog", () => {
  it("lists variables first, then functions", () => {
    const all = allCompletions();
    expect(all.length).toBe(
      CEL_VARIABLES.length + OCTO_FUNCTIONS.length + CEL_BUILTINS.length,
    );
    expect(all.slice(0, CEL_VARIABLES.length).every((e) => e.kind === "variable")).toBe(
      true,
    );
  });

  it("resolves entries by name", () => {
    expect(lookup("body")?.kind).toBe("variable");
    expect(lookup("templateResource")?.kind).toBe("function");
    expect(lookup("filter")?.signature).toContain("filter");
    expect(lookup("nope")).toBeUndefined();
  });

  it("gives every entry a signature, summary, and example", () => {
    for (const e of allCompletions()) {
      expect(e.name).toBeTruthy();
      expect(e.signature).toBeTruthy();
      expect(e.summary).toBeTruthy();
      expect(e.example).toBeTruthy();
    }
  });

  it("has unique names", () => {
    const names = allCompletions().map((e) => e.name);
    expect(new Set(names).size).toBe(names.length);
  });
});
