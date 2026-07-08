import { describe, expect, it } from "vitest";
import { autoPair } from "./autoPair";

describe("autoPair", () => {
  it("inserts a matching closer for an opener, caret between", () => {
    expect(autoPair("", 0, 0, "(")).toEqual({ value: "()", caret: 1 });
    expect(autoPair("body.map", 8, 8, "(")).toEqual({
      value: "body.map()",
      caret: 9,
    });
  });

  it("pairs quotes", () => {
    expect(autoPair("", 0, 0, '"')).toEqual({ value: '""', caret: 1 });
    expect(autoPair("", 0, 0, "'")).toEqual({ value: "''", caret: 1 });
  });

  it("steps over an existing closer instead of duplicating", () => {
    // caret before the ) in "()"
    expect(autoPair("()", 1, 1, ")")).toEqual({ value: "()", caret: 2 });
    expect(autoPair('""', 1, 1, '"')).toEqual({ value: '""', caret: 2 });
  });

  it("deletes both halves of an empty pair on Backspace", () => {
    expect(autoPair("()", 1, 1, "Backspace")).toEqual({ value: "", caret: 0 });
    expect(autoPair('a""', 2, 2, "Backspace")).toEqual({ value: "a", caret: 1 });
  });

  it("leaves normal typing and non-empty Backspace alone", () => {
    expect(autoPair("ab", 2, 2, "a")).toBeNull();
    expect(autoPair("ab", 2, 2, "Backspace")).toBeNull();
  });

  it("does not auto-pair when there is a selection", () => {
    expect(autoPair("abc", 0, 3, "(")).toBeNull();
  });
});
