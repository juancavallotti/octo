import { expect, it, vi } from "vitest";

vi.mock("@/app/model/orchestrator", () => ({ loadIntegrationTree: vi.fn() }));

import { toData } from "./managerData";

it("indexes membership both ways and keeps each folder's stored order", () => {
  const data = toData({
    folders: [],
    integrations: [],
    members: { "f-1": ["b", "a"], "f-2": [] },
    deployed: ["a"],
  });

  expect(data.order.get("f-1")).toEqual(["b", "a"]);
  expect(data.order.get("f-2")).toEqual([]);
  expect(data.membership.get("a")).toBe("f-1");
  expect(data.membership.has("c")).toBe(false);
  expect(data.deployed.has("a")).toBe(true);
});
