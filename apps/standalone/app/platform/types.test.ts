import { describe, expect, it } from "vitest";
import { tabFor } from "./types";

describe("tabFor", () => {
  it("shows versions on the folder's tab, and anything unknown on integrations", () => {
    expect(tabFor("versions")).toBe("folder");
    expect(tabFor("deployments")).toBe("deployments");
    expect(tabFor("nonsense")).toBe("integrations");
    expect(tabFor(undefined)).toBe("integrations");
  });
});
