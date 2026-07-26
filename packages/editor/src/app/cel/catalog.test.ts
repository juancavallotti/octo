import { describe, expect, it } from "vitest";
import {
  CEL_VARIABLES,
  OCTO_FUNCTIONS,
  CEL_BUILTINS,
  EXT_METHODS,
  EXT_NAMESPACES,
  EXT_NAMESPACE_ROOTS,
  LIST_METHODS,
  MAP_METHODS,
  STRING_METHODS,
  allCompletions,
  lookup,
  namespaceMembers,
} from "./catalog";

describe("cel catalog", () => {
  it("lists variables first, then functions", () => {
    const all = allCompletions();
    expect(all.length).toBe(
      CEL_VARIABLES.length +
        OCTO_FUNCTIONS.length +
        EXT_METHODS.length +
        CEL_BUILTINS.length +
        EXT_NAMESPACE_ROOTS.length,
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

  it("resolves a namespaced function by its dotted name", () => {
    expect(lookup("math.round")?.signature).toBe("math.round(double) -> double");
    expect(lookup("regex.extract")?.signature).toContain("optional(string)");
    expect(lookup("math.nope")).toBeUndefined();
    expect(lookup("nope.round")).toBeUndefined();
  });

  it("exposes each namespace's members and a root entry for it", () => {
    expect(namespaceMembers("base64")?.map((e) => e.name)).toEqual([
      "encode",
      "decode",
    ]);
    expect(namespaceMembers("nope")).toBeUndefined();
    // Inherited Object properties are not namespaces. Without an own-property
    // check these resolve to something truthy that is not a list of entries, and
    // the completion menu filters it as one.
    for (const inherited of ["constructor", "toString", "hasOwnProperty", "__proto__"]) {
      expect(namespaceMembers(inherited)).toBeUndefined();
    }
    expect(EXT_NAMESPACE_ROOTS.map((e) => e.name).sort()).toEqual(
      Object.keys(EXT_NAMESPACES).sort(),
    );
    expect(EXT_NAMESPACE_ROOTS.every((e) => e.kind === "namespace")).toBe(true);
  });

  it("gives every entry a signature, summary, and example", () => {
    const entries = [...allCompletions(), ...Object.values(EXT_NAMESPACES).flat()];
    for (const e of entries) {
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

  // The receiver-method lists are built by name lookup, so a rename in the
  // catalogue silently shrinks them. Pin the extension entries that must be there.
  it("offers the extension methods on the right receivers", () => {
    const names = (list: typeof LIST_METHODS) => list.map((e) => e.name);
    expect(names(STRING_METHODS)).toEqual(
      expect.arrayContaining(["upperAscii", "trim", "split", "format", "replace"]),
    );
    expect(names(LIST_METHODS)).toEqual(
      expect.arrayContaining(["distinct", "sortBy", "flatten", "join", "first"]),
    );
    expect(names(MAP_METHODS)).toEqual(
      expect.arrayContaining(["transformMap", "transformMapEntry", "existsOne"]),
    );
    // A string has no list reshaping and a list has no case folding.
    expect(names(STRING_METHODS)).not.toContain("sortBy");
    expect(names(LIST_METHODS)).not.toContain("upperAscii");
  });
});
