import { describe, expect, it } from "vitest";
import { listIcons, resolveIcon } from "./index";

describe("listIcons", () => {
  it("offers only names resolveIcon knows", () => {
    const names = listIcons();
    expect(names.length).toBeGreaterThan(0);

    // resolveIcon falls back to a generic box for an unknown name, so a listed
    // name that lands on the fallback is one the picker would offer and the UI
    // would then draw as something else.
    const fallback = resolveIcon("definitely-not-an-icon");
    const unresolvable = names.filter((n) => resolveIcon(n) === fallback);
    expect(unresolvable).toEqual([]);
  });

  it("is sorted, so the picker's order does not follow declaration order", () => {
    const names = listIcons();
    expect(names).toEqual([...names].sort());
  });

  it("includes the brand marks the product already ships", () => {
    expect(listIcons()).toEqual(expect.arrayContaining(["Slack", "Notion"]));
  });
});
