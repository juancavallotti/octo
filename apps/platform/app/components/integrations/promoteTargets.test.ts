import { describe, expect, it } from "vitest";
import { promoteTargets } from "./promoteTargets";

const all = [
  { name: "octo", displayName: "Default", namespace: "octo", default: true },
  { name: "staging", displayName: "Staging", namespace: "octo-staging", default: false },
  { name: "prod", displayName: "", namespace: "octo-prod", default: false },
];

describe("promoteTargets", () => {
  it("offers an administrator every other environment", () => {
    expect(promoteTargets(all, "staging", ["platform:admin"], {}).map((t) => t.name)).toEqual(["octo", "prod"]);
  });

  it("offers only where the person builds", () => {
    const got = promoteTargets(all, "", ["platform:developer"], {
      staging: ["platform:developer"],
      prod: ["platform:monitor"],
    });
    expect(got).toEqual([{ name: "staging", label: "Staging", namespace: "octo-staging" }]);
  });

  it("never offers the environment the version is already in", () => {
    expect(promoteTargets(all, "", ["platform:admin"], {}).map((t) => t.name)).not.toContain("octo");
  });
});
