import { describe, expect, it } from "vitest";
import {
  declareInProject,
  declareResource,
  renameInProject,
  undeclareInProject,
  undeclareResource,
  renameDeclaration,
} from "./declarations";
import { parseProject } from "../../model/project";
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

describe("project-wide declarations", () => {
  // octo.yaml carries the identity and an env file; orders.yaml declares a template.
  const files = parseProject([
    {
      path: "octo.yaml",
      content: "service:\n  name: shop\nresources:\n  env: [.env.staging]\n",
    },
    {
      path: "orders.yaml",
      content:
        "resources:\n  env: [.env.staging]\n  templates:\n    - resource: t.tmpl\nflows:\n  - name: o\n    process: []\n",
    },
  ]);

  it("declares a new resource in the file that carries the identity", () => {
    const [change] = declareInProject(files, "b.tmpl", "template");
    expect(change.path).toBe("octo.yaml");
    expect(change.resources.templates).toContainEqual({ resource: "b.tmpl" });
  });

  it("declares nothing when some file already does", () => {
    expect(declareInProject(files, "t.tmpl", "template")).toEqual([]);
  });

  it("undeclares from every file that declares it", () => {
    const changes = undeclareInProject(files, ".env.staging");
    expect(changes.map((c) => c.path)).toEqual(["octo.yaml", "orders.yaml"]);
    expect(changes.every((c) => !c.resources.env.includes(".env.staging"))).toBe(true);
  });

  it("renames only where the old name is declared", () => {
    const changes = renameInProject(files, "t.tmpl", "mail/t.tmpl", "template");
    expect(changes).toEqual([
      { path: "orders.yaml", resources: { env: [".env.staging"], templates: [{ resource: "mail/t.tmpl" }] } },
    ]);
  });
});
