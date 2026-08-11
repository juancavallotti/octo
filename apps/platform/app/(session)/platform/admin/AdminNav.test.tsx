import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const pathname = vi.fn(() => "/platform/admin");
vi.mock("next/navigation", () => ({ usePathname: () => pathname() }));

import AdminNav, { ADMIN_SECTIONS } from "./AdminNav";
import { LIVE } from "./AdminTiles";

describe("AdminNav", () => {
  it("links every admin section, and back to the platform", () => {
    render(<AdminNav />);

    for (const section of ADMIN_SECTIONS) {
      expect(screen.getByRole("link", { name: section.label }).getAttribute("href")).toBe(
        section.href,
      );
    }
    // Exact: "Platform agent" is a tab, and a loose match now finds both.
    expect(screen.getByRole("link", { name: "Platform" }).getAttribute("href")).toBe("/platform");
  });

  // The hub's own href prefixes every other page, so a plain startsWith would
  // light Overview everywhere. Longest match wins instead.
  it("marks only the deepest matching section as current", () => {
    pathname.mockReturnValue("/platform/admin/llm");
    render(<AdminNav />);

    expect(screen.getByRole("link", { name: "LLM provider" }).getAttribute("aria-current")).toBe(
      "page",
    );
    expect(screen.getByRole("link", { name: "Overview" }).getAttribute("aria-current")).toBeNull();
  });

  it("marks Overview as current on the hub itself", () => {
    pathname.mockReturnValue("/platform/admin");
    render(<AdminNav />);

    expect(screen.getByRole("link", { name: "Overview" }).getAttribute("aria-current")).toBe(
      "page",
    );
  });
});

// The hub's tiles and the switcher are two lists of the same pages, and they drifted
// the moment one was added in a different pull request from the other: the agent got
// a tile and no tab. Neither list can be derived from the other — a tile carries a
// sentence, a tab carries two words — so this is what holds them together.
describe("the admin section lists", () => {
  it("offers a tab for every live tile, and a tile for every tab", () => {
    const tabs = ADMIN_SECTIONS.filter((s) => s.key !== "overview").map((s) => s.href);
    const tiles = LIVE.map((t) => t.href);

    expect([...tabs].sort()).toEqual([...tiles].sort());
  });
});
