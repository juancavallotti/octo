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
    r.kind === "folder"
      ? `${"  ".repeat(r.folder.depth)}[${r.folder.id}]`
      : `${"  ".repeat(r.depth)}${r.integration.id}`,
  );

describe("treeRows", () => {
  it("nests subfolders before a folder's own integrations, and leaves the unfiled at the top", () => {
    expect(label(treeRows(data, "all", new Set()))).toEqual([
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
    expect(label(treeRows(data, "all", new Set(["ops"])))).toEqual([
      "[ops]",
      "[empty]",
      "d",
    ]);
  });

  // Running cuts across filing, so it keeps a folder only for the deployed
  // integrations somewhere beneath it — an empty folder is noise there.
  it("keeps only deployed integrations and the folders leading to them under Running", () => {
    expect(label(treeRows(data, "running", new Set()))).toEqual([
      "[ops]",
      "  [alerts]",
      "    a",
      "d",
    ]);
  });

  it("counts a folder's own integrations and says whether it can expand", () => {
    const rows = treeRows(data, "all", new Set());
    const folders = Object.fromEntries(
      rows.flatMap((r) => (r.kind === "folder" ? [[r.folder.id, r]] : [])),
    );
    expect(folders.ops).toMatchObject({ count: 1, expandable: true });
    expect(folders.alerts).toMatchObject({ count: 2, expandable: true });
    expect(folders.empty).toMatchObject({ count: 0, expandable: false });
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
