import { expect, it, vi } from "vitest";

vi.mock("@/app/model/orchestrator", () => ({}));

import { runningCountOf } from "./managerViews";

import type { Data } from "./managerData";
import type { Integration } from "@/app/model/orchestrator";

const integration = (id: string) => ({ id, name: id, definition: "" }) as Integration;

// The deployed set comes from a separate read, so it can name an integration the
// list no longer has — which would inflate the Running count.
it("counts only deployed integrations the list still has", () => {
  const data: Data = {
    folders: [],
    integrations: ["a", "b"].map(integration),
    membership: new Map(),
    order: new Map(),
    deployed: new Set(["a", "gone"]),
  };
  expect(runningCountOf(data)).toBe(1);
});
