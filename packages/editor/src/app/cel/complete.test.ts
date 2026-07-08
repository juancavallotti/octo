import { describe, expect, it } from "vitest";
import {
  tokenAt,
  completionsAt,
  applyCompletion,
  memberContext,
  type MemberProvider,
} from "./complete";
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

  it("offers nothing while the caret is inside a string literal", () => {
    expect(completionsAt('"t', 2).items).toHaveLength(0);
    expect(completionsAt("body.startsWith('t", 18).items).toHaveLength(0);
  });

  it("resumes offering after a string literal closes", () => {
    // caret after `t` in: "x" + t
    const text = '"x" + t';
    expect(completionsAt(text, text.length).items.map((e) => e.name)).toContain(
      "templateResource",
    );
  });
});

describe("memberContext", () => {
  it("returns the base path and token for a member access", () => {
    expect(memberContext("body.user.na", 12)).toEqual({
      base: ["body", "user"],
      token: "na",
      range: [10, 12],
    });
  });

  it("returns null for a non-member token", () => {
    expect(memberContext("body", 4)).toBeNull();
  });

  it("bails on a non-identifier base", () => {
    expect(memberContext("foo().na", 8)).toBeNull();
  });
});

describe("completionsAt with a member provider", () => {
  const members: MemberProvider = (path) =>
    path.length === 1 && path[0] === "body"
      ? [
          { name: "message", kind: "variable", signature: "string", summary: "", example: "" },
          { name: "mode", kind: "variable", signature: "string", summary: "", example: "" },
          { name: "id", kind: "variable", signature: "double", summary: "", example: "" },
        ]
      : undefined;

  it("offers sample keys after body.", () => {
    const names = completionsAt("body.", 5, { members }).items.map((e) => e.name);
    expect(names).toEqual(["message", "mode", "id"]);
  });

  it("filters member keys by the typed prefix", () => {
    const names = completionsAt("body.m", 6, { members }).items.map((e) => e.name);
    expect(names).toEqual(["message", "mode"]);
  });

  it("offers nothing for an unknown base", () => {
    expect(completionsAt("vars.x", 6, { members }).items).toHaveLength(0);
  });

  it("still offers nothing after a plain identifier dot without a provider", () => {
    expect(completionsAt("body.m", 6).items).toHaveLength(0);
  });
});

describe("literal receiver methods", () => {
  it("offers list methods after a `]` literal, even without a provider", () => {
    const names = completionsAt('["a","b"].', 10).items.map((e) => e.name);
    expect(names).toEqual(expect.arrayContaining(["map", "filter", "all", "size"]));
  });

  it("offers string methods after a closing quote", () => {
    const names = completionsAt('"hello".', 8).items.map((e) => e.name);
    expect(names).toEqual(
      expect.arrayContaining(["startsWith", "endsWith", "matches"]),
    );
  });

  it("filters list methods by the typed prefix", () => {
    const names = completionsAt('[1,2].m', 7).items.map((e) => e.name);
    expect(names).toEqual(["map"]);
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
