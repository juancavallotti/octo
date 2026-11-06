import { describe, expect, it } from "vitest";
import { landingAfterSwitch, visibleEnvironments } from "./environmentSwitch";

const all = [
  { name: "octo", displayName: "Default", default: true },
  { name: "staging", displayName: "Staging", default: false },
  { name: "qa", displayName: "", default: false },
];

describe("visibleEnvironments", () => {
  it("offers an administrator every environment", () => {
    expect(visibleEnvironments(all, ["platform:admin"], {}).map((o) => o.name)).toEqual([
      "",
      "staging",
      "qa",
    ]);
  });

  it("offers anybody else only where they hold a role", () => {
    const got = visibleEnvironments(all, [], { staging: ["platform:developer"] });
    expect(got).toEqual([{ name: "staging", label: "Staging" }]);
  });

  it("labels an environment by its name when it has no display name", () => {
    const got = visibleEnvironments(all, ["platform:admin"], {});
    expect(got[2]).toEqual({ name: "qa", label: "qa" });
  });
});

describe("landingAfterSwitch", () => {
  it.each([
    ["/platform/i/abc", "/platform/integrations"],
    ["/platform/integrations/abc/def", "/platform/integrations"],
    ["/platform/metrics/dep-1", "/platform/metrics"],
    ["/platform/metrics", "/platform/metrics"],
    ["/platform/admin/environments", "/platform/admin/environments"],
    ["/platform", "/platform"],
  ])("%s lands on %s", (from, to) => {
    expect(landingAfterSwitch(from)).toBe(to);
  });
});
