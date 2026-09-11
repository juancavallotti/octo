import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listUsers = vi.fn();
const listRoles = vi.fn();
const grantRole = vi.fn();
const revokeRole = vi.fn();
const deleteUser = vi.fn();
const createUser = vi.fn();
vi.mock("@/app/model/users", () => ({
  listUsers: () => listUsers(),
  listRoles: () => listRoles(),
  grantRole: (id: string, role: string) => grantRole(id, role),
  revokeRole: (id: string, role: string) => revokeRole(id, role),
  deleteUser: (id: string) => deleteUser(id),
  createUser: (subject: string, input: unknown) => createUser(subject, input),
}));

import UsersManager from "./UsersManager";
import { RolesProvider } from "@/app/auth/RolesContext";
import { PLATFORM_ADMIN, PLATFORM_MONITOR } from "@/app/auth/roles";

function user(over: Partial<{ id: string; email: string; name: string; roles: string[] }> = {}) {
  return {
    id: over.id ?? "u1",
    email: over.email ?? "ada@example.com",
    name: over.name ?? "Ada Lovelace",
    roles: over.roles ?? [PLATFORM_ADMIN],
    createdAt: "2026-01-01T00:00:00Z",
    lastLoginAt: "2026-01-02T00:00:00Z",
  };
}

function renderManager(currentUserId = "somebody-else") {
  return render(
    <RolesProvider roles={[PLATFORM_ADMIN]} mayWrite>
      <UsersManager currentUserId={currentUserId} />
    </RolesProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  listRoles.mockResolvedValue([
    { role: PLATFORM_ADMIN, description: "Administers the installation" },
    { role: PLATFORM_MONITOR, description: "Looks, and nothing else" },
  ]);
  listUsers.mockResolvedValue([user()]);
});

describe("UsersManager", () => {
  it("lists people with the roles they hold", async () => {
    renderManager();

    expect(await screen.findByText("Ada Lovelace")).toBeTruthy();
    expect(screen.getByText("ada@example.com")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Admin" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Monitor" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("grants a role the person does not hold", async () => {
    grantRole.mockResolvedValue(user({ roles: [PLATFORM_ADMIN, PLATFORM_MONITOR] }));
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: "Monitor" }));

    expect(grantRole).toHaveBeenCalledWith("u1", PLATFORM_MONITOR);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Monitor" }).getAttribute("aria-pressed")).toBe("true"),
    );
  });

  it("revokes a role the person holds", async () => {
    revokeRole.mockResolvedValue(user({ roles: [] }));
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: "Admin" }));
    expect(revokeRole).toHaveBeenCalledWith("u1", PLATFORM_ADMIN);
  });

  // Editing your own roles is how somebody locks themselves out of the section
  // they are standing in. iam refuses to remove the last administrator anyway;
  // this stops the attempt being made by accident.
  it("will not let somebody change or remove themselves", async () => {
    renderManager("u1");

    await screen.findByText("Ada Lovelace");
    expect(screen.getByRole("button", { name: "Admin" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: /Remove ada@example.com/ })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("leaves somebody else's row editable", async () => {
    listUsers.mockResolvedValue([user(), user({ id: "u2", email: "bob@example.com", name: "Bob" })]);
    renderManager("u1");

    await screen.findByText("Bob");
    expect(screen.getByRole("button", { name: /Remove bob@example.com/ })).toHaveProperty(
      "disabled",
      false,
    );
  });

  it("filters by name or email", async () => {
    listUsers.mockResolvedValue([user(), user({ id: "u2", email: "bob@example.com", name: "Bob" })]);
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Bob");
    await person.type(screen.getByLabelText("Filter by name or email"), "bob");

    expect(screen.queryByText("Ada Lovelace")).toBeNull();
    expect(screen.getByText("Bob")).toBeTruthy();
  });

  it("filters by role", async () => {
    listUsers.mockResolvedValue([
      user(),
      user({ id: "u2", email: "bob@example.com", name: "Bob", roles: [PLATFORM_MONITOR] }),
    ]);
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Bob");
    await person.selectOptions(screen.getByLabelText("Filter by role"), PLATFORM_MONITOR);

    expect(screen.getByText("Bob")).toBeTruthy();
    expect(screen.queryByText("Ada Lovelace")).toBeNull();
  });

  it("surfaces a refusal from iam rather than swallowing it", async () => {
    revokeRole.mockRejectedValue(new Error("this is the last platform:admin"));
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: "Admin" }));

    expect(await screen.findByText(/last platform:admin/)).toBeTruthy();
  });

  // Each call answers with the whole user, so two in flight together can land out
  // of order and the older reply would overwrite the newer state.
  it("takes one role change at a time", async () => {
    let settle: (u: unknown) => void = () => {};
    grantRole.mockReturnValue(new Promise((r) => (settle = r)));
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: "Monitor" }));

    expect(screen.getByRole("button", { name: "Admin" })).toHaveProperty("disabled", true);
    settle(user({ roles: [PLATFORM_ADMIN, PLATFORM_MONITOR] }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Admin" })).toHaveProperty("disabled", false),
    );
  });

  // A first load that fails must stop saying "Loading…", or the error sits
  // underneath a spinner that will never resolve.
  it("stops loading when the first request fails", async () => {
    listUsers.mockRejectedValue(new Error("iam unreachable"));
    renderManager();

    expect(await screen.findByText("iam unreachable")).toBeTruthy();
    expect(screen.queryByText("Loading…")).toBeNull();
  });

  it("says so when the list is empty", async () => {
    listUsers.mockResolvedValue([]);
    renderManager();

    expect(await screen.findByText("Nobody yet.")).toBeTruthy();
  });
});
