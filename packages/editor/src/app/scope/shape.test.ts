import { describe, expect, it } from "vitest";
import { field, merge, objectOf, shapeAtPath, shapeOfJson, UNKNOWN } from "./shape";
import type { ValueShape } from "./types";

const json = (v: unknown) => shapeOfJson(v, "sample");
const keys = (s: ValueShape) => (s.kind === "object" ? Object.keys(s.fields).sort() : null);

describe("shapeOfJson", () => {
  it("reads an object's keys", () => {
    expect(keys(json({ id: 1, name: "x" }))).toEqual(["id", "name"]);
  });

  it("leaves an object open — one sample never proves what is absent", () => {
    const shape = json({ id: 1 });
    expect(shape.kind === "object" && shape.open).toBe(true);
  });

  it("merges a list's elements rather than taking the first", () => {
    const shape = json([{ a: 1 }, { b: 2 }]);
    expect(shape.kind).toBe("list");
    expect(keys(shape.kind === "list" ? shape.of : UNKNOWN)).toEqual(["a", "b"]);
  });

  it("stops descending before a deeply nested sample becomes useless", () => {
    let deep: unknown = "leaf";
    for (let i = 0; i < 12; i++) deep = { next: deep };
    // It must terminate and say something honest rather than recurse forever.
    expect(shapeAtPath(json(deep), ["next"])).toBeDefined();
  });
});

describe("merge", () => {
  it("takes the other side when one knows nothing", () => {
    expect(merge(UNKNOWN, { kind: "string" })).toEqual({ kind: "string" });
  });

  it("widens two different scalars to dyn", () => {
    expect(merge({ kind: "string" }, { kind: "number" })).toEqual({ kind: "dyn" });
  });

  it("keeps the shape when one side was null", () => {
    // Widening a nullable object to dyn would cost every key it has, to say
    // something the model cannot express anyway.
    expect(keys(merge(json({ a: 1 }), { kind: "null" }))).toEqual(["a"]);
  });

  it("unions object keys", () => {
    expect(keys(merge(json({ a: 1 }), json({ b: 2 })))).toEqual(["a", "b"]);
  });

  it("marks a key present on only one side as uncertain", () => {
    const merged = merge(json({ a: 1, b: 2 }), json({ a: 1 }));
    expect(merged.kind === "object" && merged.fields.a.certain).toBe(true);
    expect(merged.kind === "object" && merged.fields.b.certain).toBe(false);
  });

  it("never narrows: merging is idempotent on itself", () => {
    const shape = json({ a: { b: [1, 2] } });
    expect(merge(shape, shape)).toEqual(shape);
  });
});

describe("shapeAtPath", () => {
  const shape = objectOf({ user: field(objectOf({ id: field({ kind: "string" }, "sample") }), "sample") });

  it("follows a path into nested objects", () => {
    expect(shapeAtPath(shape, ["user", "id"])).toEqual({ kind: "string" });
  });

  it("gives up rather than guessing when the path is not there", () => {
    expect(shapeAtPath(shape, ["user", "nope"])).toBeUndefined();
    expect(shapeAtPath(shape, ["user", "id", "deeper"])).toBeUndefined();
  });
});
