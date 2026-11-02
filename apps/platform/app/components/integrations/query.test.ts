import { expect, it } from "vitest";
import { buildPath, parsePathname } from "./query";

it("round-trips the filter and the selection through the path", () => {
  for (const sel of [
    { filter: "all" as const, selectedId: null },
    { filter: "all" as const, selectedId: "int-1" },
    { filter: "running" as const, selectedId: null },
    { filter: "running" as const, selectedId: "int-1" },
  ]) {
    expect(parsePathname(`/platform/integrations${buildPath(sel)}`)).toEqual(sel);
  }
});

it("keeps the default view's URL clean", () => {
  expect(buildPath({ filter: "all", selectedId: null })).toBe("");
});
