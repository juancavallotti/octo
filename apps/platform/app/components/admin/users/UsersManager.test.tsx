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
  listUsers: (query: unknown) => listUsers(query),
  listRoles: () => listRoles(),
  grantRole: (id: string, role: string) => grantRole(id, role),
  revokeRole: (id: string, role: string) => revokeRole(id, role),
  deleteUser: (id: string) => deleteUser(id),
  createUser: (input: unknown) => createUser(input),
}));

import UsersManager from "./UsersManager";
import { RolesProvider } from "@/app/auth/RolesContext";
import { PLATFORM_ADMIN, PLATFORM_MONITOR } from "@/app/auth/roles";

interface Person {
  id: string;
  email: string;
  name: string;
  roles: string[];
  createdAt: string;
  lastLoginAt: string | null;
}

function user(over: Partial<Person> = {}): Person {
  return {
    id: over.id ?? "u1",
    email: over.email ?? "ada@example.com",
    name: over.name ?? "Ada Lovelace",
    roles: over.roles ?? [PLATFORM_ADMIN],
    createdAt: "2026-01-01T00:00:00Z",
    lastLoginAt: over.lastLoginAt === undefined ? "2026-01-02T00:00:00Z" : over.lastLoginAt,
  };
}

/**
 * Stand in for iam: filtering and paging happen there now, so a fake that
 * ignored the query would let a test pass against a component that never sent
 * one. It answers the way the service does, cursor and all.
 */
function serve(people: Person[], pageSize = 25) {
  listUsers.mockImplementation(
    ({ q = "", role = "", cursor = "" }: { q?: string; role?: string; cursor?: string }) => {
      const needle = q.toLowerCase();
      const matches = people.filter(
        (p) =>
          (!needle ||
            p.name.toLowerCase().includes(needle) ||
            p.email.toLowerCase().includes(needle)) &&
          (!role || p.roles.includes(role)),
      );
      const start = cursor ? matches.findIndex((p) => p.id === cursor) + 1 : 0;
      const items = matches.slice(start, start + pageSize);
      const last = items.at(-1);
      const more = last && matches.indexOf(last) < matches.length - 1;
      return Promise.resolve({ items, ...(more ? { nextCursor: last.id } : {}) });
    },
  );
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
  serve([user()]);
});

describe("UsersManager", () => {
  it("lists people with the roles they hold", async () => {
    renderManager();

    expect(await screen.findByText("Ada Lovelace")).toBeTruthy();
    expect(screen.getByText("ada@example.com")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Admin" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Monitor" }).getAttribute("aria-pressed")).toBe("false");
  });

  // "Did my invite work" is the question this screen will be asked, and a blank
  // cell is not an answer to it.
  it("says Never for somebody who has not arrived yet", async () => {
    serve([user({ lastLoginAt: null })]);
    renderManager();

    expect(await screen.findByText("Never")).toBeTruthy();
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
    serve([user(), user({ id: "u2", email: "bob@example.com", name: "Bob" })]);
    renderManager("u1");

    await screen.findByText("Bob");
    expect(screen.getByRole("button", { name: /Remove bob@example.com/ })).toHaveProperty(
      "disabled",
      false,
    );
  });

  // The filter is a question for iam, not a pass over what is already on screen:
  // the rest of the directory is not in the browser to filter.
  it("asks iam for the people matching the text filter", async () => {
    serve([user(), user({ id: "u2", email: "bob@example.com", name: "Bob" })]);
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Bob");
    await person.type(screen.getByLabelText("Filter by name or email"), "bob");

    await waitFor(() => expect(screen.queryByText("Ada Lovelace")).toBeNull());
    expect(screen.getByText("Bob")).toBeTruthy();
    expect(listUsers).toHaveBeenCalledWith(expect.objectContaining({ q: "bob" }));
  });

  it("asks iam for the people holding the chosen role", async () => {
    serve([
      user(),
      user({ id: "u2", email: "bob@example.com", name: "Bob", roles: [PLATFORM_MONITOR] }),
    ]);
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Bob");
    await person.selectOptions(screen.getByLabelText("Filter by role"), PLATFORM_MONITOR);

    await waitFor(() => expect(screen.queryByText("Ada Lovelace")).toBeNull());
    expect(screen.getByText("Bob")).toBeTruthy();
    expect(listUsers).toHaveBeenCalledWith(expect.objectContaining({ role: PLATFORM_MONITOR }));
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
    serve([]);
    renderManager();

    expect(await screen.findByText("Nobody yet.")).toBeTruthy();
  });
});

describe("paging", () => {
  const many = Array.from({ length: 5 }, (_, i) =>
    user({ id: `u${i}`, email: `p${i}@example.com`, name: `Person ${i}` }),
  );

  beforeEach(() => {
    listRoles.mockResolvedValue([]);
    serve(many, 2);
  });

  it("stays out of the way when everybody fits on one page", async () => {
    serve(many, 25);
    renderManager();

    await screen.findByText("Person 0");
    expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
  });

  it("walks forward and back through the directory", async () => {
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Person 0");
    expect(screen.queryByText("Person 2")).toBeNull();

    await person.click(screen.getByRole("button", { name: "Next page" }));
    expect(await screen.findByText("Person 2")).toBeTruthy();
    expect(screen.queryByText("Person 0")).toBeNull();

    await person.click(screen.getByRole("button", { name: "Previous page" }));
    expect(await screen.findByText("Person 0")).toBeTruthy();
  });

  // A filter is a different listing, and the cursors collected for the old one
  // name positions in a sequence that no longer exists.
  it("returns to the first page when the filter changes", async () => {
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Person 0");
    await person.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("Person 2");

    await person.type(screen.getByLabelText("Filter by name or email"), "Person");

    await waitFor(() => expect(screen.getByText("Person 0")).toBeTruthy());
  });
});
