import { describe, expect, it } from "vitest";

import { resolveDrag } from "./dragDrop";

import type { FlatFolder } from "./model";

// a
// └── b
//     └── c
// d
const flat: FlatFolder[] = [
  { id: "a", name: "a", parentId: null, depth: 0 },
  { id: "b", name: "b", parentId: "a", depth: 1 },
  { id: "c", name: "c", parentId: "b", depth: 2 },
  { id: "d", name: "d", parentId: null, depth: 0 },
];

const ctx = (membership: Record<string, string | null> = {}) => ({
  flat,
  membership: new Map(Object.entries(membership)),
});

const folder = (id: string) => ({ kind: "folder" as const, id, name: id });
const integration = (id: string) => ({ kind: "integration" as const, id, name: id });

describe("resolveDrag: integrations", () => {
  it("reorders onto a peer", () => {
    expect(resolveDrag(integration("i1"), integration("i2"), ctx())).toEqual({
      kind: "reorder-integration",
      id: "i1",
      beforeId: "i2",
    });
  });

  it("files into a folder it is not already in", () => {
    expect(resolveDrag(integration("i1"), { kind: "folder", id: "a" }, ctx())).toEqual({
      kind: "file-integration",
      folderId: "a",
      integrationId: "i1",
    });
  });

  it("unfiles onto Unfiled", () => {
    expect(
      resolveDrag(integration("i1"), { kind: "unfiled" }, ctx({ i1: "a" })),
    ).toEqual({ kind: "unfile-integration", folderId: "a", integrationId: "i1" });
  });

  // Every one of these is a write the tree would otherwise make for nothing.
  it("does nothing when the drop changes nothing", () => {
    const none = { kind: "none" };
    expect(resolveDrag(integration("i1"), integration("i1"), ctx())).toEqual(none);
    // Already in that folder.
    expect(
      resolveDrag(integration("i1"), { kind: "folder", id: "a" }, ctx({ i1: "a" })),
    ).toEqual(none);
    // Not in a folder to begin with.
    expect(resolveDrag(integration("i1"), { kind: "unfiled" }, ctx())).toEqual(none);
    // "All" is a view, not a place an integration can be.
    expect(resolveDrag(integration("i1"), { kind: "root" }, ctx({ i1: "a" }))).toEqual(
      none,
    );
  });
});

describe("resolveDrag: folders", () => {
  it("reorders among siblings", () => {
    expect(resolveDrag(folder("a"), { kind: "folder", id: "d" }, ctx())).toEqual({
      kind: "reorder-folder",
      parentId: null,
      id: "a",
      beforeId: "d",
    });
  });

  it("reparents under a folder in another group", () => {
    expect(resolveDrag(folder("d"), { kind: "folder", id: "b" }, ctx())).toEqual({
      kind: "reparent-folder",
      id: "d",
      name: "d",
      parentId: "b",
    });
  });

  it("lifts to the root off the All bucket", () => {
    expect(resolveDrag(folder("c"), { kind: "root" }, ctx())).toEqual({
      kind: "reparent-folder",
      id: "c",
      name: "c",
      parentId: null,
    });
  });

  // The case worth having a test for: dropping a folder into its own subtree
  // would detach that subtree from the tree, and nobody reproduces it by hand
  // twice.
  it("refuses to nest a folder inside itself or a descendant", () => {
    const none = { kind: "none" };
    expect(resolveDrag(folder("a"), { kind: "folder", id: "a" }, ctx())).toEqual(none);
    expect(resolveDrag(folder("a"), { kind: "folder", id: "b" }, ctx())).toEqual(none);
    // Two levels down is still inside it.
    expect(resolveDrag(folder("a"), { kind: "folder", id: "c" }, ctx())).toEqual(none);
  });

  it("does nothing for a drop that changes nothing", () => {
    const none = { kind: "none" };
    // Already at the root.
    expect(resolveDrag(folder("a"), { kind: "root" }, ctx())).toEqual(none);
    // A folder has no business on Unfiled.
    expect(resolveDrag(folder("a"), { kind: "unfiled" }, ctx())).toEqual(none);
    // Unknown ids on either side.
    expect(resolveDrag(folder("zz"), { kind: "folder", id: "a" }, ctx())).toEqual(none);
    expect(resolveDrag(folder("a"), { kind: "folder", id: "zz" }, ctx())).toEqual(none);
  });

  it("does nothing without both ends of the drag", () => {
    expect(resolveDrag(undefined, { kind: "root" }, ctx())).toEqual({ kind: "none" });
    expect(resolveDrag(folder("a"), undefined, ctx())).toEqual({ kind: "none" });
  });
});
