import { expect, it } from "vitest";
import { buildPath, parsePathname } from "./query";

it("round-trips the selection through the path", () => {
  for (const sel of [{ selectedId: null }, { selectedId: "int-1" }]) {
    expect(parsePathname(`/platform/integrations${buildPath(sel)}`)).toEqual(sel);
  }
});

it("keeps the URL clean when nothing is selected", () => {
  expect(buildPath({ selectedId: null })).toBe("");
});
