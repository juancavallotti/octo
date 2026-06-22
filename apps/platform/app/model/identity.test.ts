import { describe, it, expect } from "vitest";
import { slugify, uniqueSlug, duplicateNames, flowNames } from "./identity";
import { emptyDocument, newBlock } from "./document";

describe("slugify", () => {
  it("lower-cases and kebab-cases", () => {
    expect(slugify("My Database")).toBe("my-database");
    expect(slugify("already-slug")).toBe("already-slug");
    expect(slugify("___")).toBe("");
  });

  it("converts spaces to dashes and keeps a trailing dash for live typing", () => {
    expect(slugify("my db")).toBe("my-db");
    expect(slugify("my-")).toBe("my-");
    expect(slugify("  Spaces  &  symbols!! ")).toBe("spaces-symbols-");
  });
});

describe("uniqueSlug", () => {
  it("returns the slug when free", () => {
    expect(uniqueSlug("HTTP Client", new Set())).toBe("http-client");
  });

  it("suffixes until free", () => {
    const taken = new Set(["database", "database-2"]);
    expect(uniqueSlug("database", taken)).toBe("database-3");
  });

  it("falls back when the input slugs to empty", () => {
    expect(uniqueSlug("!!!", new Set())).toBe("item");
  });
});

describe("duplicateNames", () => {
  it("returns names occurring more than once, ignoring empty", () => {
    const dupes = duplicateNames(["a", "b", "a", "", "", "c", "c"]);
    expect(dupes).toEqual(new Set(["a", "c"]));
  });
});

describe("flowNames", () => {
  it("collects names from top-level and nested flows", () => {
    const doc = emptyDocument();
    doc.flows[0].name = "main";
    const branch = newBlock("if"); // seeds then/else sub-flows
    branch.slots!.then[0].name = "on-true";
    doc.flows[0].process = [branch];

    expect(flowNames(doc).sort()).toEqual(["main", "on-true"]);
  });
});
