import { describe, it, expect } from "vitest";
import { addCase, nameTaken, removeCase, uniqueCaseName, updateCase } from "./edit";
import type { Suite } from "./types";

const suite = (...names: string[]): Suite => ({
  flow: "orders",
  cases: names.map((name) => ({ name })),
});

describe("updateCase", () => {
  it("replaces one case and leaves the file alone", () => {
    const before = suite("a", "b");
    const after = updateCase(before, 1, { name: "b", skip: "later" });

    expect(after.cases).toEqual([{ name: "a" }, { name: "b", skip: "later" }]);
    expect(before.cases[1]).toEqual({ name: "b" }); // the input is untouched
  });

  it("ignores an index that is not there", () => {
    const before = suite("a");
    expect(updateCase(before, 5, { name: "x" })).toBe(before);
  });
});

describe("addCase", () => {
  // A case with no `expect` is not an empty test: dolphin still asserts the flow ran to
  // the end without failing, which is the assertion people most often forget to write.
  it("appends a case carrying nothing but its name", () => {
    expect(addCase(suite("a"), "b").cases).toEqual([{ name: "a" }, { name: "b" }]);
  });
});

describe("removeCase", () => {
  it("drops the case at the index", () => {
    expect(removeCase(suite("a", "b", "c"), 1).cases.map((c) => c.name)).toEqual([
      "a",
      "c",
    ]);
  });
});

describe("nameTaken", () => {
  it("lets a case keep its own name", () => {
    expect(nameTaken(suite("a", "b"), "a", 0)).toBe(false);
  });

  it("reports another case with the same name", () => {
    expect(nameTaken(suite("a", "b"), "a", 1)).toBe(true);
  });

  // dolphin compares the decoded strings, and YAML keeps the surrounding spaces.
  it("compares trimmed", () => {
    expect(nameTaken(suite("a"), "  a  ", 1)).toBe(true);
  });
});

describe("uniqueCaseName", () => {
  it("uses the base when nothing has it", () => {
    expect(uniqueCaseName(suite("a"))).toBe("new case");
  });

  it("counts up past every name already taken", () => {
    expect(uniqueCaseName(suite("new case", "new case 2"))).toBe("new case 3");
  });
});
