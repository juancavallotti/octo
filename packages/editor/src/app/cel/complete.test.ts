import { describe, expect, it } from "vitest";
import { tokenAt, completionsAt, applyCompletion } from "./complete";
import { lookup } from "./catalog";

describe("tokenAt", () => {
  it("reads the identifier left of the caret", () => {
    const q = tokenAt("bod", 3);
    expect(q.token).toBe("bod");
    expect(q.range).toEqual([0, 3]);
    expect(q.member).toBe(false);
  });

  it("flags a member access after a dot", () => {
    const q = tokenAt("body.na", 7);
    expect(q.token).toBe("na");
    expect(q.member).toBe(true);
  });

  it("is empty between tokens", () => {
    expect(tokenAt("body ", 5).token).toBe("");
  });
});

describe("completionsAt", () => {
  it("filters by name prefix", () => {
    const { items } = completionsAt("bo", 2);
    expect(items.map((e) => e.name)).toContain("body");
    expect(items.every((e) => e.name.toLowerCase().startsWith("bo"))).toBe(true);
  });

  it("suggests functions by prefix", () => {
    const names = completionsAt("start", 5).items.map((e) => e.name);
    expect(names).toContain("startsWith");
  });

  it("offers nothing on an empty token while typing", () => {
    expect(completionsAt("body ", 5).items).toHaveLength(0);
  });

  it("lists everything when explicitly triggered on an empty token", () => {
    expect(completionsAt("", 0, { explicit: true }).items.length).toBeGreaterThan(0);
  });

  it("offers nothing after a member dot (basic pass)", () => {
    expect(completionsAt("body.na", 7).items).toHaveLength(0);
  });
});

describe("applyCompletion", () => {
  it("replaces a variable token and places the caret after it", () => {
    const q = tokenAt("bo", 2);
    const r = applyCompletion("bo", q, lookup("body")!);
    expect(r.text).toBe("body");
    expect(r.caret).toBe(4);
  });

  it("inserts an opening paren for functions, caret inside", () => {
    const q = tokenAt("start", 5);
    const r = applyCompletion("start", q, lookup("startsWith")!);
    expect(r.text).toBe("startsWith(");
    expect(r.caret).toBe("startsWith(".length);
  });

  it("only replaces the token, preserving surrounding text", () => {
    const text = "size(bo) > 0";
    const q = tokenAt(text, 7); // caret after "bo"
    const r = applyCompletion(text, q, lookup("body")!);
    expect(r.text).toBe("size(body) > 0");
  });
});
