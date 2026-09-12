import { describe, expect, it } from "vitest";
import { field, objectOf } from "./shape";
import { describe as describeShape, membersFor, rootsFor } from "./members";
import type { Scope } from "./types";

const scope: Scope = {
  roots: {
    body: field(
      objectOf({
        orderId: field({ kind: "string" }, "sample", "seen in a test input"),
        lines: field({ kind: "list", of: objectOf({ sku: field({ kind: "string" }, "sample") }) }, "sample"),
      }),
      "sample",
    ),
    vars: field(objectOf({ userId: field({ kind: "string" }, "inferred", "set by set-variable") }), "declared"),
    now: field({ kind: "timestamp" }, "declared", "the current time"),
  },
};

const names = (entries: { name: string }[] | undefined) => (entries ?? []).map((e) => e.name);

describe("rootsFor", () => {
  it("offers exactly the roots in scope", () => {
    expect(names(rootsFor(scope))).toEqual(["body", "now", "vars"]);
  });
});

describe("membersFor", () => {
  const members = membersFor(scope);

  it("completes a body's keys", () => {
    expect(names(members(["body"]))).toEqual(expect.arrayContaining(["orderId", "lines"]));
  });

  it("offers list methods on a nested list, not only on a root one", () => {
    // `names` turns undefined into [], so asserting [] here passed while membersFor
    // was answering "no idea" — the module treats those as different answers.
    expect(names(members(["body", "lines"]))).toContain("map");
  });

  it("says nothing about a root it has never heard of", () => {
    // undefined, not []: an empty list asserts there is nothing there, and the
    // caller treats the two differently.
    expect(members(["nope"])).toBeUndefined();
  });

  it("offers list methods on a value that is a list", () => {
    const listScope: Scope = { roots: { items: field({ kind: "list", of: { kind: "dyn" } }, "sample") } };
    expect(names(membersFor(listScope)(["items"]))).toContain("map");
  });

  it("reports where a belief came from, and never a value", () => {
    const entry = members(["vars"])?.find((e) => e.name === "userId");
    expect(entry?.summary).toBe("set by set-variable");
    // The reason line is provenance. A captured or sampled VALUE must never reach
    // the menu — it is the user's data, and the menu is not where it belongs.
    expect(entry?.summary).not.toMatch(/=/);
  });
});

describe("describe", () => {
  it("names shapes the way CEL does", () => {
    expect(describeShape({ kind: "string" })).toBe("string");
    expect(describeShape(objectOf({}))).toBe("map(string, dyn)");
    expect(describeShape({ kind: "list", of: { kind: "string" } })).toBe("list(string)");
  });
});
