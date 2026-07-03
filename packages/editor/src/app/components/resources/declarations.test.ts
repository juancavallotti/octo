import { describe, expect, it } from "vitest";
import {
  declareResource,
  undeclareResource,
  renameDeclaration,
} from "./declarations";
import type { Resources } from "../../model/document";

const base: Resources = {
  env: [".env.staging"],
  templates: [{ resource: "templates/a.tmpl", as: "a" }],
};

describe("declareResource", () => {
  it("adds env and template names, and is idempotent", () => {
    expect(declareResource(base, ".env.prod", "env").env).toContain(".env.prod");
    expect(declareResource(base, "b.tmpl", "template").templates).toContainEqual(
      { resource: "b.tmpl" },
    );
    expect(declareResource(base, ".env.staging", "env")).toBe(base);
  });

  it("seeds from empty when resources is undefined", () => {
    expect(declareResource(undefined, "x.tmpl", "template")).toEqual({
      env: [],
      templates: [{ resource: "x.tmpl" }],
    });
  });
});

describe("undeclareResource", () => {
  it("removes a name from whichever list holds it, leaving others", () => {
    const out = undeclareResource(base, "templates/a.tmpl");
    expect(out.templates).toHaveLength(0);
    expect(out.env).toEqual([".env.staging"]);
  });
});

describe("renameDeclaration", () => {
  it("moves a template declaration and preserves its alias", () => {
    const out = renameDeclaration(base, "templates/a.tmpl", "templates/z.tmpl", "template");
    expect(out.templates).toContainEqual({ resource: "templates/z.tmpl", as: "a" });
    expect(out.templates.some((t) => t.resource === "templates/a.tmpl")).toBe(false);
  });

  it("moves an env declaration", () => {
    const out = renameDeclaration(base, ".env.staging", ".env.qa", "env");
    expect(out.env).toEqual([".env.qa"]);
  });
});
