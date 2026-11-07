import { describe, expect, it, vi } from "vitest";

vi.mock("@/app/model/environments", () => ({ listEnvironments: vi.fn() }));

import { environmentRoleChanges } from "./EnvironmentRoles";

describe("environmentRoleChanges", () => {
  it("grants and revokes what differs per environment", () => {
    expect(
      environmentRoleChanges({ staging: ["platform:developer"] }, { staging: ["platform:operator"] }),
    ).toEqual([
      { environment: "staging", role: "platform:operator", grant: true },
      { environment: "staging", role: "platform:developer", grant: false },
    ]);
  });

  it("reads an environment named after a prototype property as its own entry", () => {
    expect(environmentRoleChanges({}, { constructor: ["platform:developer"] })).toEqual([
      { environment: "constructor", role: "platform:developer", grant: true },
    ]);
  });
});
