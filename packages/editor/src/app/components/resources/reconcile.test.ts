import { describe, expect, it } from "vitest";
import { reconcileResources } from "./reconcile";
import type { StoredResource } from "../../providers/ResourceStoreProvider";

const r = (name: string, kind: "env" | "template" = "template"): StoredResource => ({
  id: name,
  name,
  kind,
  content: `content of ${name}`,
});

describe("reconcileResources", () => {
  it("marks declared+present files as tracked", () => {
    const out = reconcileResources(
      [r("templates/welcome.tmpl")],
      { env: [], templates: [{ resource: "templates/welcome.tmpl" }] },
    );
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({
      name: "templates/welcome.tmpl",
      scope: "tracked",
      kind: "template",
    });
  });

  it("marks present-but-undeclared files as out-of-scope", () => {
    const out = reconcileResources([r("stray.json")], { env: [], templates: [] });
    expect(out[0]).toMatchObject({ name: "stray.json", scope: "out-of-scope" });
    expect(out[0].stored).toBeDefined();
  });

  it("marks declared-but-absent names as declared-missing with the declared kind", () => {
    const out = reconcileResources([], {
      env: [".env.staging"],
      templates: [{ resource: "templates/x.tmpl" }],
    });
    expect(out).toEqual([
      { name: ".env.staging", scope: "declared-missing", kind: "env" },
      { name: "templates/x.tmpl", scope: "declared-missing", kind: "template" },
    ]);
  });

  it("excludes .env.dev entirely (owned by the Dev .env panel)", () => {
    const out = reconcileResources([r(".env.dev", "env")], { env: [], templates: [] });
    expect(out).toHaveLength(0);
  });

  it("sorts by name and handles an empty document", () => {
    const out = reconcileResources([r("b.txt"), r("a.txt")], undefined);
    expect(out.map((e) => e.name)).toEqual(["a.txt", "b.txt"]);
    expect(out.every((e) => e.scope === "out-of-scope")).toBe(true);
  });

  // The editor's own bookkeeping is not the user's to manage: listing it would offer
  // to edit or "include in project" a file the editor owns.
  it("excludes the editor's private .octo/ resources", () => {
    const out = reconcileResources(
      [r(".octo/editor-meta.json", "template"), r("welcome.tmpl", "template")],
      { env: [], templates: [] },
    );
    expect(out.map((e) => e.name)).toEqual(["welcome.tmpl"]);
  });

  // Even a document that somehow declares one must not surface it.
  it("excludes a declared .octo/ resource", () => {
    const out = reconcileResources([], {
      env: [],
      templates: [{ resource: ".octo/editor-meta.json" }],
    });
    expect(out).toHaveLength(0);
  });
});
