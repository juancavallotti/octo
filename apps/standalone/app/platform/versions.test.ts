import { describe, expect, it } from "vitest";
import { nextTag } from "./versions";

describe("nextTag", () => {
  it("bumps the newest version's patch number", () => {
    expect(nextTag(["v1.2.0", "v1.10.3", "v1.9.9"])).toBe("v1.10.4");
  });

  it("starts where octopaas starts when there are none", () => {
    expect(nextTag([])).toBe("v0.1.0");
  });

  it("leaves the field to the user when no tag reads as a version", () => {
    expect(nextTag(["spring-release"])).toBe("");
  });
});
