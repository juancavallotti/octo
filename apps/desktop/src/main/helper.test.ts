import path from "node:path";
import { describe, expect, it } from "vitest";
import { helperExecutable } from "./helper";

/**
 * The layout of a packaged .app is the contract here, and it is not one this code
 * can discover at run time on a developer's machine: the path is only ever exercised
 * in a signed bundle, where getting it wrong means the app starts, shows a splash,
 * and never comes up.
 */
describe("helperExecutable", () => {
  const resources = "/Applications/Octo.app/Contents/Resources";

  it("points at the helper bundle beside Resources", () => {
    expect(path.normalize(helperExecutable(resources, "Octo"))).toBe(
      "/Applications/Octo.app/Contents/Frameworks/Octo Helper.app/Contents/MacOS/Octo Helper",
    );
  });

  it("follows the product name, which is what electron-builder names helpers after", () => {
    expect(path.normalize(helperExecutable(resources, "Octo Nightly"))).toBe(
      "/Applications/Octo.app/Contents/Frameworks/Octo Nightly Helper.app/Contents/MacOS/Octo Nightly Helper",
    );
  });
});
