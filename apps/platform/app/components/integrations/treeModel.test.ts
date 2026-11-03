import { describe, expect, it, vi } from "vitest";

vi.mock("@/app/model/orchestrator", () => ({}));

import type { Integration } from "@/app/model/orchestrator";
import type { Data } from "./managerData";
import { flatten } from "./model";
import { ancestorsOf, folderIntegrations, treeRows, type TreeRow } from "./treeModel";

const integration = (id: string) => ({ id, name: id, definition: "" }) as Integration;

// ops
// └── alerts      (holds b, a — stored in that order)
// empty
// c is filed in ops; d sits at the top level.
const data: Data = {
  folders: [
    {
      id: "ops",
      name: "ops",
      parentId: null,
      children: [{ id: "alerts", name: "alerts", parentId: "ops" }],
    },
    { id: "empty", name: "empty", parentId: null },
  ],
  integrations: ["a", "b", "c", "d"].map(integration),
  membership: new Map([
    ["a", "alerts"],
    ["b", "alerts"],
    ["c", "ops"],
  ]),
  order: new Map([["alerts", ["b", "a"]]]),
  deployed: new Set(["a", "d"]),
};

const label = (rows: TreeRow[]) =>
  rows.map((r) =>
    r.kind === "smart"
      ? `<${r.id}> ${r.count}`
      : r.kind === "folder"
        ? `${"  ".repeat(r.folder.depth)}[${r.folder.id}]`
        : `${"  ".repeat(r.depth)}${r.integration.id}`,
  );

// Both smart folders closed, so the cases below are about the real tree.
const smartClosed = ["smart:running", "smart:all"];

describe("treeRows", () => {
  it("nests subfolders before a folder's own integrations, and leaves the unfiled at the top", () => {
    expect(label(treeRows(data, new Set(smartClosed)))).toEqual([
      "<running> 2",
      "<all> 4",
      "[ops]",
      "  [alerts]",
      "    b",
      "    a",
      "  c",
      "[empty]",
      "d",
    ]);
  });

  it("hides everything beneath a collapsed folder", () => {
    expect(label(treeRows(data, new Set([...smartClosed, "ops"])))).toEqual([
      "<running> 2",
      "<all> 4",
      "[ops]",
      "[empty]",
      "d",
    ]);
  });

  // A smart folder's contents are computed, not filed: Running lists every
  // deployed integration flat, wherever it is filed, and the real tree still
  // shows each one where it lives.
  it("lists a smart folder's integrations flat, beside the real tree", () => {
    const rows = treeRows(data, new Set(["smart:all", "ops", "empty"]));
    expect(label(rows)).toEqual(["<running> 2", "  a", "  d", "<all> 4", "[ops]", "[empty]", "d"]);
    // The copies are their own rows, and say they are computed.
    const copies = rows.flatMap((r) => (r.kind === "integration" && r.computed ? [r.rowId] : []));
    expect(copies).toEqual(["smart:running:integration:a", "smart:running:integration:d"]);
  });

  it("counts a folder's own integrations and says whether it can expand", () => {
    const rows = treeRows(data, new Set(smartClosed));
    const folders = Object.fromEntries(
      rows.flatMap((r) => (r.kind === "folder" ? [[r.folder.id, r]] : [])),
    );
    expect(folders.ops).toMatchObject({ count: 1, expandable: true });
    expect(folders.alerts).toMatchObject({ count: 2, expandable: true });
    expect(folders.empty).toMatchObject({ count: 0, expandable: false });
  });

  // A match inside a closed folder would otherwise be found and not shown.
  it("searches names, keeping only the folders that lead to a match and opening them", () => {
    expect(label(treeRows(data, new Set([...smartClosed, "ops", "alerts"]), "A"))).toEqual([
      "<running> 1",
      "<all> 1",
      "[ops]",
      "  [alerts]",
      "    a",
    ]);
  });
});

describe("folderIntegrations", () => {
  // A card just dragged into a folder is not in that folder's stored order yet;
  // sorting it to the front would land it somewhere nobody dropped it.
  it("puts an integration missing from the stored order at the end", () => {
    const justAssigned: Data = {
      ...data,
      membership: new Map([...data.membership, ["d", "alerts"]]),
    };
    expect(folderIntegrations(justAssigned, "alerts").map((i) => i.id)).toEqual([
      "b",
      "a",
      "d",
    ]);
  });
});

describe("ancestorsOf", () => {
  it("walks from an integration's folder up to the top", () => {
    expect(ancestorsOf(data, flatten(data.folders), "a")).toEqual(["alerts", "ops"]);
    expect(ancestorsOf(data, flatten(data.folders), "d")).toEqual([]);
  });
});
