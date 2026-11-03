import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listApiKeys = vi.fn();
const createApiKey = vi.fn();
const deleteApiKey = vi.fn();
vi.mock("@/app/model/apikeys", () => ({
  listApiKeys: () => listApiKeys(),
  createApiKey: (name: string, roles: string[], expiresAt: string) =>
    createApiKey(name, roles, expiresAt),
  deleteApiKey: (id: string) => deleteApiKey(id),
}));

import ApiKeysManager from "./ApiKeysManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import { RolesProvider } from "@/app/auth/RolesContext";
import {
  PLATFORM_ADMIN,
  PLATFORM_DEVELOPER,
  PLATFORM_MONITOR,
  PLATFORM_OPERATOR,
} from "@/app/auth/roles";

function renderAs(roles: string[]) {
  return render(
    <RolesProvider roles={roles} mayWrite>
      <ConfirmProvider>
        <ApiKeysManager />
      </ConfirmProvider>
    </RolesProvider>,
  );
}

describe("ApiKeysManager", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listApiKeys.mockResolvedValue([]);
  });

  it("offers an admin every key role but admin", async () => {
    renderAs([PLATFORM_ADMIN]);
    await waitFor(() => expect(listApiKeys).toHaveBeenCalled());
    for (const label of ["Monitor", "Developer", "Operator"]) {
      expect(screen.getByRole("checkbox", { name: label })).toBeInTheDocument();
    }
    expect(screen.queryByRole("checkbox", { name: "Admin" })).toBeNull();
  });

  it("offers a developer only what a developer reaches", async () => {
    renderAs([PLATFORM_DEVELOPER]);
    await waitFor(() => expect(listApiKeys).toHaveBeenCalled());
    expect(screen.getByRole("checkbox", { name: "Developer" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Monitor" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Operator" })).toBeNull();
  });

  it("creates nothing until a role is ticked, then sends the roles and the date", async () => {
    const user = userEvent.setup();
    createApiKey.mockResolvedValue({
      id: "k1",
      name: "ci",
      prefix: "octo_abcd",
      last4: "wxyz",
      roles: [PLATFORM_OPERATOR],
      createdAt: "2026-11-03T00:00:00Z",
      expiresAt: "2026-12-03T00:00:00Z",
      token: "octo_secret",
    });
    renderAs([PLATFORM_OPERATOR]);
    await waitFor(() => expect(listApiKeys).toHaveBeenCalled());

    await user.type(screen.getByPlaceholderText(/Key name/), "ci");
    const create = screen.getByRole("button", { name: /Create/ });
    expect(create).toBeDisabled();

    await user.click(screen.getByRole("checkbox", { name: "Operator" }));
    await user.click(screen.getByRole("checkbox", { name: "Monitor" }));
    await user.click(screen.getByRole("button", { name: "7 days" }));
    await user.click(create);

    await waitFor(() => expect(createApiKey).toHaveBeenCalled());
    const [name, roles, expiresAt] = createApiKey.mock.calls[0];
    expect(name).toBe("ci");
    expect(roles).toEqual([PLATFORM_OPERATOR, PLATFORM_MONITOR]);
    const days = (new Date(expiresAt).getTime() - Date.now()) / 86_400_000;
    expect(days).toBeGreaterThan(5.9);
    expect(days).toBeLessThanOrEqual(7);
  });
});
