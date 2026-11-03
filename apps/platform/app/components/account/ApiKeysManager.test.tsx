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

  it("opens a card on each role saying what a key with it may do", async () => {
    listApiKeys.mockResolvedValue([key("k1", "ci", [PLATFORM_DEVELOPER], 30)]);
    renderAs([PLATFORM_ADMIN]);
    await screen.findByText("ci");
    const checkbox = screen.getByRole("checkbox", { name: "Operator" });
    expect(checkbox.closest("[aria-describedby]")).toHaveAccessibleDescription(
      expect.stringContaining("Deploy, roll out, scale and remove deployments"),
    );
    const pill = screen
      .getAllByText("Developer")
      .find((el) => el.closest("[aria-describedby]") && !el.closest("label") && !el.closest("[role=tooltip]"));
    expect(pill?.closest("[aria-describedby]")).toHaveAccessibleDescription(
      expect.stringContaining("Deploy, roll out or scale"),
    );
  });

  it("deletes every expired key, and only those, after confirming", async () => {
    const user = userEvent.setup();
    listApiKeys.mockResolvedValue([
      key("live", "live", [PLATFORM_MONITOR], 30),
      key("old1", "old1", [PLATFORM_MONITOR], -1),
      key("old2", "old2", [PLATFORM_MONITOR], -10),
    ]);
    deleteApiKey.mockResolvedValue(undefined);
    renderAs([PLATFORM_ADMIN]);

    await user.click(await screen.findByRole("button", { name: "Delete expired (2)" }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteApiKey).toHaveBeenCalledTimes(2));
    expect(deleteApiKey.mock.calls.map((c) => c[0]).sort()).toEqual(["old1", "old2"]);
  });

  it("refreshes after a partial bulk delete and names the keys that failed", async () => {
    const user = userEvent.setup();
    listApiKeys.mockResolvedValue([
      key("old1", "old1", [PLATFORM_MONITOR], -1),
      key("old2", "old2", [PLATFORM_MONITOR], -10),
    ]);
    deleteApiKey.mockImplementation((id: string) =>
      id === "old2" ? Promise.reject(new Error("boom")) : Promise.resolve(),
    );
    renderAs([PLATFORM_ADMIN]);

    await user.click(await screen.findByRole("button", { name: "Delete expired (2)" }));
    const calls = listApiKeys.mock.calls.length;
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    expect(await screen.findByText('Could not delete "old2".')).toBeInTheDocument();
    expect(listApiKeys.mock.calls.length).toBeGreaterThan(calls);
  });

  it("offers no bulk delete when nothing has expired", async () => {
    listApiKeys.mockResolvedValue([key("live", "live", [PLATFORM_MONITOR], 30)]);
    renderAs([PLATFORM_ADMIN]);
    await screen.findByText("live");
    expect(screen.queryByRole("button", { name: /Delete expired/ })).toBeNull();
  });
});

/** A listed key expiring `days` from now (negative for one already expired). */
function key(id: string, name: string, roles: string[], days: number) {
  return {
    id,
    name,
    prefix: "octo_abcd",
    last4: "wxyz",
    roles,
    createdAt: "2026-01-01T00:00:00Z",
    expiresAt: new Date(Date.now() + days * 86_400_000).toISOString(),
    lastUsedAt: null,
  };
}
