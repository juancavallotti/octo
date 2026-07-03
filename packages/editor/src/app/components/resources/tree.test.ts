import { describe, expect, it } from "vitest";
import { buildTree, ensureFolders, flattenTree, leafName } from "./tree";
import type { ReconciledResource } from "./reconcile";

const file = (name: string): ReconciledResource => ({
  name,
  scope: "tracked",
  kind: "template",
});

describe("leafName", () => {
  it("returns the last path segment", () => {
    expect(leafName("templates/email/welcome.tmpl")).toBe("welcome.tmpl");
    expect(leafName("flat.json")).toBe("flat.json");
  });
});

describe("buildTree + flattenTree", () => {
  it("derives nested folders from names and orders folders before files", () => {
    const rows = flattenTree(
      buildTree([
        file("root.txt"),
        file("templates/a.tmpl"),
        file("templates/email/b.tmpl"),
      ]),
      new Set(),
    );
    expect(
      rows.map((r) =>
        r.kind === "folder" ? `[${r.path}]` : `${r.depth}:${r.label}`,
      ),
    ).toEqual([
      // Within a folder: subfolders first (recursed), then that folder's files.
      "[templates]",
      "[templates/email]",
      "2:b.tmpl",
      "1:a.tmpl",
      "0:root.txt",
    ]);
  });

  it("hides descendants of a collapsed folder", () => {
    const tree = buildTree([file("templates/a.tmpl")]);
    const rows = flattenTree(tree, new Set(["templates"]));
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("folder");
  });

  it("injects empty pending folders as rows", () => {
    const tree = ensureFolders(buildTree([]), ["templates/email"]);
    const rows = flattenTree(tree, new Set());
    expect(rows.map((r) => (r.kind === "folder" ? r.path : "?"))).toEqual([
      "templates",
      "templates/email",
    ]);
  });

  it("ensureFolders is idempotent with folders derived from files", () => {
    const tree = ensureFolders(buildTree([file("templates/a.tmpl")]), [
      "templates",
    ]);
    const folders = flattenTree(tree, new Set()).filter(
      (r) => r.kind === "folder",
    );
    expect(folders).toHaveLength(1);
  });

  it("keeps the file entry's full path for downstream ops", () => {
    const rows = flattenTree(buildTree([file("templates/a.tmpl")]), new Set());
    const fileRow = rows.find((r) => r.kind === "file");
    expect(fileRow?.kind === "file" && fileRow.entry.name).toBe(
      "templates/a.tmpl",
    );
    expect(fileRow?.kind === "file" && fileRow.label).toBe("a.tmpl");
  });
});
