import path from "node:path";
import { describe, expect, it } from "vitest";
import { helperExecutable } from "./helper";

/**
 * The layout of a packaged .app is the contract, and it is only ever exercised in a
 * signed bundle — so it is pinned here rather than discovered at run time.
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
