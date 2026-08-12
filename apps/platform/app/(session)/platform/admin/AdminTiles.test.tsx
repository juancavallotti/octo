import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import AdminTiles from "./AdminTiles";

describe("AdminTiles", () => {
  it("links the live capabilities to their routes", () => {
    render(<AdminTiles />);

    expect(screen.getByRole("link", { name: /Email/ }).getAttribute("href")).toBe(
      "/platform/admin/email",
    );
    expect(screen.getByRole("link", { name: /LLM provider/ }).getAttribute("href")).toBe(
      "/platform/admin/llm",
    );
    expect(screen.getByRole("link", { name: /Platform agent/ }).getAttribute("href")).toBe(
      "/platform/admin/agent",
    );
  });

  // The unbuilt capabilities are shown so the section reads as intentional, but they
  // must not look clickable — there is nothing behind them yet.
  it("renders the unbuilt capabilities as inert, not as links", () => {
    render(<AdminTiles />);

    for (const name of [/Data retention/]) {
      expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.getByText(name.source.replace(/\\/g, ""))).toBeTruthy();
    }
    expect(screen.getAllByText("Coming soon")).toHaveLength(1);
  });

  it("marks the unbuilt tiles as disabled for assistive tech", () => {
    const { container } = render(<AdminTiles />);

    expect(container.querySelectorAll('[aria-disabled="true"]')).toHaveLength(1);
  });
});
