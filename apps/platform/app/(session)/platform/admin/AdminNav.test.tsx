import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const pathname = vi.fn(() => "/platform/admin");
vi.mock("next/navigation", () => ({ usePathname: () => pathname() }));

import AdminNav, { ADMIN_SECTIONS } from "./AdminNav";

describe("AdminNav", () => {
  it("links every admin section, and back to the platform", () => {
    render(<AdminNav />);

    for (const section of ADMIN_SECTIONS) {
      expect(screen.getByRole("link", { name: section.label }).getAttribute("href")).toBe(
        section.href,
      );
    }
    expect(screen.getByRole("link", { name: /Platform/ }).getAttribute("href")).toBe("/platform");
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
