import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listUsers = vi.fn();
const listRoles = vi.fn();
const grantRole = vi.fn();
const revokeRole = vi.fn();
const deleteUser = vi.fn();
const createUser = vi.fn();
const updateUser = vi.fn();
vi.mock("@/app/model/users", () => ({
  listUsers: (query: unknown) => listUsers(query),
  listRoles: () => listRoles(),
  grantRole: (id: string, role: string) => grantRole(id, role),
  revokeRole: (id: string, role: string) => revokeRole(id, role),
  deleteUser: (id: string) => deleteUser(id),
  createUser: (input: unknown) => createUser(input),
  updateUser: (id: string, input: unknown) => updateUser(id, input),
}));

import UsersManager from "./UsersManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import { RolesProvider } from "@/app/auth/RolesContext";
import { PLATFORM_ADMIN, PLATFORM_MONITOR } from "@/app/auth/roles";

interface Person {
  id: string;
  subject: string;
  email: string;
  name: string;
  roles: string[];
  createdAt: string;
  lastLoginAt: string | null;
}

function user(over: Partial<Person> = {}): Person {
  return {
    id: over.id ?? "u1",
    subject: over.subject === undefined ? "auth0|ada" : over.subject,
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
      <ConfirmProvider>
        <UsersManager currentUserId={currentUserId} />
      </ConfirmProvider>
    </RolesProvider>,
  );
}

/** The open dialog, which is where every role change happens now. */
function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  vi.clearAllMocks();
  listRoles.mockResolvedValue([
    { role: PLATFORM_ADMIN, description: "Administers the installation" },
    { role: PLATFORM_MONITOR, description: "Looks, and nothing else" },
  ]);
  serve([user()]);
});

describe("the directory", () => {
  it("lists people with the roles they hold", async () => {
    renderManager();

    expect(await screen.findByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    // Scoped to the table: the role filter offers every role as an option, and a
    // bare text query would find those instead.
    const row = within(screen.getByRole("table"));
    expect(row.getByText("Admin")).toBeInTheDocument();
    // A reading, not a control: the role a person does NOT hold has no place in
    // the row at all, because there is nothing to click.
    expect(row.queryByText("Monitor")).toBeNull();
  });

  // The answer to "why is this person not getting in" belongs on the screen that
  // gets asked it.
  it("shows the subject their provider presents", async () => {
    renderManager();
    expect(await screen.findByText("auth0|ada")).toBeInTheDocument();
  });

  it("says so when a provisioned person has not arrived", async () => {
    serve([user({ subject: "", lastLoginAt: null })]);
    renderManager();

    expect(await screen.findByText("Not signed in yet")).toBeInTheDocument();
    expect(screen.getByText("Never")).toBeInTheDocument();
  });

  it("says so when somebody holds nothing", async () => {
    serve([user({ roles: [] })]);
    renderManager();
    expect(await screen.findByText("No roles")).toBeInTheDocument();
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
    expect(listUsers).toHaveBeenCalledWith(expect.objectContaining({ role: PLATFORM_MONITOR }));
  });

  it("says so when the list is empty", async () => {
    serve([]);
    renderManager();
    expect(await screen.findByText("Nobody yet.")).toBeInTheDocument();
  });

  // A first load that fails must stop saying "Loading…", or the error sits
  // underneath a spinner that will never resolve.
  it("stops loading when the first request fails", async () => {
    listUsers.mockRejectedValue(new Error("iam unreachable"));
    renderManager();

    expect(await screen.findByText("iam unreachable")).toBeInTheDocument();
    expect(screen.queryByText("Loading…")).toBeNull();
  });
});

describe("adding a person", () => {
  // The point of the whole change: nobody types an OIDC subject any more, so
  // there is no field for one to be typed into.
  it("asks for an address, a name and roles, and nothing else", async () => {
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Ada Lovelace");
    await person.click(screen.getByRole("button", { name: "Add a person" }));

    expect(within(dialog()).getByLabelText(/Email/)).toBeInTheDocument();
    expect(within(dialog()).getByLabelText(/Name/)).toBeInTheDocument();
    expect(within(dialog()).getByRole("checkbox", { name: /Admin/ })).toBeInTheDocument();
    expect(within(dialog()).queryByLabelText(/subject/i)).toBeNull();
  });

  // Letting somebody in and saying what they may do is one decision, so it is
  // one call: nobody exists here holding something nobody chose.
  it("sends the chosen roles with the create", async () => {
    createUser.mockResolvedValue({});
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Ada Lovelace");
    await person.click(screen.getByRole("button", { name: "Add a person" }));
    await person.type(within(dialog()).getByLabelText(/Email/), "grace@example.com");
    await person.click(within(dialog()).getByRole("checkbox", { name: /Monitor/ }));
    await person.click(within(dialog()).getByRole("button", { name: "Add" }));

    expect(createUser).toHaveBeenCalledWith({
      email: "grace@example.com",
      name: "",
      roles: [PLATFORM_MONITOR],
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  // A refusal belongs in front of the person who can correct it, which means the
  // dialog they typed into rather than the list behind it.
  it("stays open and shows a refusal", async () => {
    createUser.mockRejectedValue(new Error("that address already has an account"));
    const person = userEvent.setup();
    renderManager();

    await screen.findByText("Ada Lovelace");
    await person.click(screen.getByRole("button", { name: "Add a person" }));
    await person.type(within(dialog()).getByLabelText(/Email/), "ada@example.com");
    await person.click(within(dialog()).getByRole("button", { name: "Add" }));

    expect(await screen.findByText(/already has an account/)).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});

describe("editing a person", () => {
  // Roles are not changeable from the table at all any more. That is the whole
  // reason the dialog exists: a chip in a list somebody is scrolling is how
  // platform:admin gets handed out by accident.
  it("offers no role control in the row", async () => {
    renderManager();
    await screen.findByText("Ada Lovelace");

    expect(screen.queryByRole("button", { name: "Monitor" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  it("opens with what the person already holds", async () => {
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Edit ada@example.com/ }));

    expect(within(dialog()).getByLabelText(/Email/)).toHaveValue("ada@example.com");
    expect(within(dialog()).getByRole("checkbox", { name: /Admin/ })).toBeChecked();
    expect(within(dialog()).getByRole("checkbox", { name: /Monitor/ })).not.toBeChecked();
  });

  it("sends only the roles that changed", async () => {
    updateUser.mockResolvedValue({});
    grantRole.mockResolvedValue({});
    revokeRole.mockResolvedValue({});
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Edit ada@example.com/ }));
    await person.click(within(dialog()).getByRole("checkbox", { name: /Monitor/ }));
    await person.click(within(dialog()).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(grantRole).toHaveBeenCalledWith("u1", PLATFORM_MONITOR));
    expect(revokeRole).not.toHaveBeenCalled();
    expect(updateUser).toHaveBeenCalledWith("u1", {
      email: "ada@example.com",
      name: "Ada Lovelace",
    });
  });

  it("revokes what was unticked", async () => {
    updateUser.mockResolvedValue({});
    revokeRole.mockResolvedValue({});
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Edit ada@example.com/ }));
    await person.click(within(dialog()).getByRole("checkbox", { name: /Admin/ }));
    await person.click(within(dialog()).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(revokeRole).toHaveBeenCalledWith("u1", PLATFORM_ADMIN));
    expect(grantRole).not.toHaveBeenCalled();
  });

  it("surfaces a refusal from iam rather than swallowing it", async () => {
    updateUser.mockResolvedValue({});
    revokeRole.mockRejectedValue(new Error("this is the last platform:admin"));
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Edit ada@example.com/ }));
    await person.click(within(dialog()).getByRole("checkbox", { name: /Admin/ }));
    await person.click(within(dialog()).getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/last platform:admin/)).toBeInTheDocument();
  });

  // Editing your own roles is how somebody locks themselves out of the section
  // they are standing in. iam refuses to remove the last administrator anyway;
  // this stops the attempt being made by accident.
  it("will not let somebody change their own roles or remove themselves", async () => {
    const person = userEvent.setup();
    renderManager("u1");

    await screen.findByText("Ada Lovelace");
    expect(screen.getByRole("button", { name: /Remove ada@example.com/ })).toBeDisabled();

    await person.click(screen.getByRole("button", { name: /Edit ada@example.com/ }));
    expect(within(dialog()).getByRole("checkbox", { name: /Admin/ })).toBeDisabled();
    expect(within(dialog()).getByText(/cannot change your own roles/)).toBeInTheDocument();
  });

  it("leaves somebody else's row removable", async () => {
    serve([user(), user({ id: "u2", email: "bob@example.com", name: "Bob" })]);
    renderManager("u1");

    await screen.findByText("Bob");
    expect(screen.getByRole("button", { name: /Remove bob@example.com/ })).toBeEnabled();
  });
});

describe("removing a person", () => {
  // Their API keys and grants go with them, so it asks first.
  it("asks before removing, and does not remove when declined", async () => {
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Remove ada@example.com/ }));
    await person.click(await screen.findByRole("button", { name: /cancel/i }));

    expect(deleteUser).not.toHaveBeenCalled();
  });

  it("removes when confirmed", async () => {
    deleteUser.mockResolvedValue(undefined);
    const person = userEvent.setup();
    renderManager();

    await person.click(await screen.findByRole("button", { name: /Remove ada@example.com/ }));
    await person.click(await screen.findByRole("button", { name: "Remove" }));

    await waitFor(() => expect(deleteUser).toHaveBeenCalledWith("u1"));
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
    expect(await screen.findByText("Person 2")).toBeInTheDocument();
    expect(screen.queryByText("Person 0")).toBeNull();

    await person.click(screen.getByRole("button", { name: "Previous page" }));
    expect(await screen.findByText("Person 0")).toBeInTheDocument();
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

    await waitFor(() => expect(screen.getByText("Person 0")).toBeInTheDocument());
  });
});
