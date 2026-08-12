import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import UserMenuClient from "./UserMenuClient";

function renderMenu() {
  return render(
    <UserMenuClient
      name="Ada Lovelace"
      email="ada@example.com"
      image={null}
      signOutAction={vi.fn()}
    />,
  );
}

describe("UserMenuClient", () => {
  it("opens to the account destinations", async () => {
    const user = userEvent.setup();
    renderMenu();

    await user.click(screen.getByRole("button", { name: "Ada Lovelace" }));

    expect(screen.getByRole("menuitem", { name: "Admin" }).getAttribute("href")).toBe(
      "/platform/admin",
    );
    expect(screen.getByRole("menuitem", { name: "API keys" }).getAttribute("href")).toBe(
      "/platform/account",
    );
    expect(screen.getByRole("menuitem", { name: "Sign out" })).toBeTruthy();
  });

  it("closes when a destination is chosen", async () => {
    const user = userEvent.setup();
    renderMenu();

    await user.click(screen.getByRole("button", { name: "Ada Lovelace" }));
    await user.click(screen.getByRole("menuitem", { name: "Admin" }));

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("falls back to initials when the IdP returns no picture", () => {
    renderMenu();

    expect(screen.getByText("AL")).toBeTruthy();
  });
});
