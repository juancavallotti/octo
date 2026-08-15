import { describe, expect, it } from "vitest";

import {
  folderCountOf,
  runningCountOf,
  shownFor,
  unfiledCountOf,
} from "./managerViews";

import type { Data } from "./managerData";
import type { Integration } from "@/app/model/orchestrator";

const integration = (id: string) => ({ id, name: id, definition: "" }) as Integration;

const data: Data = {
  folders: [],
  integrations: ["a", "b", "c", "d"].map(integration),
  membership: new Map([
    ["a", "f1"],
    ["b", "f1"],
    ["c", "f2"],
  ]),
  // f1 lists b before a, and does not mention a card assigned since.
  order: new Map([["f1", ["b", "a"]]]),
  // Deployed cuts across filing: one filed, one not.
  deployed: new Set(["a", "d"]),
};

const ids = (list: Integration[]) => list.map((i) => i.id);

describe("shownFor", () => {
  it("shows everything under All", () => {
    expect(ids(shownFor("all", data))).toEqual(["a", "b", "c", "d"]);
  });

  it("shows only what is in no folder under Unfiled", () => {
    expect(ids(shownFor("unfiled", data))).toEqual(["d"]);
  });

  // Running is derived from deployments, not from filing, so it crosses folders.
  it("shows what is deployed under Running, filed or not", () => {
    expect(ids(shownFor("running", data))).toEqual(["a", "d"]);
  });

  // The deployed set is read from a separate call, so it can name an integration
  // the list no longer has. Showing it would render a card with nothing behind it.
  it("ignores a deployed id with no integration left", () => {
    const stale: Data = { ...data, deployed: new Set(["a", "gone"]) };
    expect(ids(shownFor("running", stale))).toEqual(["a"]);
    expect(runningCountOf(stale)).toBe(1);
  });

  it("honours a folder's stored order", () => {
    expect(ids(shownFor({ folder: "f1" }, data))).toEqual(["b", "a"]);
  });

  // The rule that only shows itself through a rendered list: a card just dragged
  // into a folder is not in that folder's stored order yet. Sorting it to the
  // front would land it somewhere nobody dropped it.
  it("puts an integration missing from the order at the end", () => {
    const justAssigned: Data = {
      ...data,
      membership: new Map([...data.membership, ["d", "f1"]]),
    };
    expect(ids(shownFor({ folder: "f1" }, justAssigned))).toEqual(["b", "a", "d"]);
  });

  it("shows a folder with no stored order in list order", () => {
    expect(ids(shownFor({ folder: "f2" }, data))).toEqual(["c"]);
  });
});

describe("counts", () => {
  it("counts what is unfiled, running, and in each folder", () => {
    expect(unfiledCountOf(data)).toBe(1);
    expect(runningCountOf(data)).toBe(2);
    expect(folderCountOf(data, "f1")).toBe(2);
    expect(folderCountOf(data, "f2")).toBe(1);
    expect(folderCountOf(data, "nope")).toBe(0);
  });
});
