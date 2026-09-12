import { describe, expect, it } from "vitest";
import { pathOf, shapeOfExpression } from "./cel";
import { field, objectOf } from "./shape";
import type { Scope, ValueShape } from "./types";

const keys = (s: ValueShape | undefined) =>
  s?.kind === "object" ? Object.keys(s.fields).sort() : null;
const typeOf = (s: ValueShape | undefined, key: string) =>
  s?.kind === "object" ? s.fields[key]?.shape.kind : null;

describe("shapeOfExpression: map literals", () => {
  it("reads the keys a set-payload states outright", () => {
    const shape = shapeOfExpression('{"orderId": "A-100", "deliveredDaysAgo": 14, "ok": true}');
    expect(keys(shape)).toEqual(["deliveredDaysAgo", "ok", "orderId"]);
    expect(typeOf(shape, "orderId")).toBe("string");
    expect(typeOf(shape, "deliveredDaysAgo")).toBe("number");
    expect(typeOf(shape, "ok")).toBe("bool");
  });

  it("closes the object, because a literal really does say what is in it", () => {
    const shape = shapeOfExpression('{"a": 1}');
    expect(shape?.kind === "object" && shape.open).toBe(false);
  });

  it("keeps the keys around a value it cannot type", () => {
    // The point of skipping rather than bailing: one computed field must not cost
    // the field names either side of it.
    const shape = shapeOfExpression('{"a": 1, "b": string(body.x) + "!", "c": true}');
    expect(keys(shape)).toEqual(["a", "b", "c"]);
    expect(typeOf(shape, "b")).toBe("dyn");
  });

  it("reads nested maps", () => {
    const shape = shapeOfExpression('{"user": {"id": "u", "age": 3}}');
    expect(keys(shape)).toEqual(["user"]);
    const user = shape?.kind === "object" ? shape.fields.user.shape : undefined;
    expect(keys(user)).toEqual(["age", "id"]);
  });

  it("reads a list of literals", () => {
    const shape = shapeOfExpression('{"tags": ["a", "b"]}');
    const tags = shape?.kind === "object" ? shape.fields.tags.shape : undefined;
    expect(tags).toEqual({ kind: "list", of: { kind: "string" } });
  });

  it("handles single quotes and commas inside strings", () => {
    const shape = shapeOfExpression(`{'a': 'x, y', "b": 2}`);
    expect(keys(shape)).toEqual(["a", "b"]);
  });

  it("says nothing about a map whose keys are computed", () => {
    // The keys would be data, and naming them would be inventing them.
    expect(shapeOfExpression('{body.key: 1}')).toBeUndefined();
  });

  it("says nothing about an expression still being typed", () => {
    expect(shapeOfExpression('{"a": 1')).toBeUndefined();
    expect(shapeOfExpression('{"a": ')).toBeUndefined();
  });

  it("says nothing about an expression that is not a literal at all", () => {
    expect(shapeOfExpression('has(body.x) ? body.x : "none"')).toBeUndefined();
    expect(shapeOfExpression('toJson(body)')).toBeUndefined();
    expect(shapeOfExpression("")).toBeUndefined();
  });
});

describe("shapeOfExpression: paths", () => {
  const scope: Scope = {
    roots: {
      vars: field(
        objectOf({
          incident: field(objectOf({ watchName: field({ kind: "string" }, "sample") }), "sample"),
        }),
        "declared",
      ),
    },
  };

  it("resolves a path against what is already in scope", () => {
    expect(shapeOfExpression("vars.incident.watchName", scope)).toEqual({ kind: "string" });
    expect(keys(shapeOfExpression("vars.incident", scope))).toEqual(["watchName"]);
  });

  it("has no answer for a path that is not there", () => {
    expect(shapeOfExpression("vars.nope.deeper", scope)).toBeUndefined();
    expect(shapeOfExpression("body.x", scope)).toBeUndefined();
  });

  it("has no answer without a scope to resolve against", () => {
    expect(shapeOfExpression("vars.incident.watchName")).toBeUndefined();
  });
});

describe("pathOf", () => {
  it("recognises a bare dotted path", () => {
    expect(pathOf(" vars.a.b ")).toEqual(["vars", "a", "b"]);
  });

  it("refuses anything that is more than one", () => {
    expect(pathOf("vars.a + 1")).toBeNull();
    expect(pathOf("f(vars.a)")).toBeNull();
    expect(pathOf('vars["a"]')).toBeNull();
  });
});
