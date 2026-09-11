import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import UserMenuClient from "./UserMenuClient";
import { RolesProvider } from "@/app/auth/RolesContext";
import { PLATFORM_ADMIN, PLATFORM_MONITOR } from "@/app/auth/roles";

function renderMenu(roles: string[] = [PLATFORM_ADMIN]) {
  return render(
    <RolesProvider roles={roles}>
      <UserMenuClient
        name="Ada Lovelace"
        email="ada@example.com"
        image={null}
        signOutAction={vi.fn()}
      />
    </RolesProvider>,
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

  // The section's layout would send them straight back, so an entry that only
  // bounces is worse than no entry.
  it("hides the admin entry from somebody who cannot use it", async () => {
    const user = userEvent.setup();
    renderMenu([PLATFORM_MONITOR]);

    await user.click(screen.getByRole("button", { name: "Ada Lovelace" }));

    expect(screen.queryByRole("menuitem", { name: "Admin" })).toBeNull();
    // The rest of the menu is theirs as much as anyone's.
    expect(screen.getByRole("menuitem", { name: "API keys" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Sign out" })).toBeTruthy();
  });
});
