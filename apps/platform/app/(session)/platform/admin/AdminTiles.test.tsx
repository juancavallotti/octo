import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import AdminTiles from "./AdminTiles";

describe("AdminTiles", () => {
  it("links the live capabilities to their routes", () => {
    render(<AdminTiles />);

    expect(screen.getByRole("link", { name: /Email/ }).getAttribute("href")).toBe(
      "/platform/admin/email",
    );
    expect(screen.getByRole("link", { name: /Platform agent/ }).getAttribute("href")).toBe(
      "/platform/admin/agent",
    );
    expect(screen.getByRole("link", { name: /Data retention/ }).getAttribute("href")).toBe(
      "/platform/admin/retention",
    );
  });

  // Data retention was the last tile the section described without having, and
  // its arrival empties the unbuilt list. These two assert the placeholder
  // treatment is gone rather than merely unused: a tile left inert would look
  // identical to a working one that had quietly lost its href.
  it("shows nothing as coming soon", () => {
    render(<AdminTiles />);

    expect(screen.queryByText("Coming soon")).toBeNull();
  });

  it("marks no tile as disabled for assistive tech", () => {
    const { container } = render(<AdminTiles />);

    expect(container.querySelectorAll('[aria-disabled="true"]')).toHaveLength(0);
  });
});
