import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const createUser = vi.fn();
vi.mock("@/app/model/users", () => ({
  createUser: (input: unknown) => createUser(input),
}));

import AddUserModal from "./AddUserModal";

describe("adding a person", () => {
  // The point of the whole change: nobody types an OIDC subject any more, so
  // there is no field for one to be typed into.
  it("asks for an address and a name, and nothing else", () => {
    render(<AddUserModal onAdded={() => {}} onClose={() => {}} />);

    expect(screen.getByLabelText(/Email/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Name/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/subject/i)).toBeNull();
  });

  it("closes and reloads the list once the person is added", async () => {
    createUser.mockResolvedValue({});
    const onAdded = vi.fn();
    const onClose = vi.fn();
    const person = userEvent.setup();
    render(<AddUserModal onAdded={onAdded} onClose={onClose} />);

    await person.type(screen.getByLabelText(/Email/), "ada@example.com");
    await person.click(screen.getByRole("button", { name: "Add" }));

    expect(createUser).toHaveBeenCalledWith({ email: "ada@example.com", name: "" });
    expect(onAdded).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  // A refusal belongs in front of the person who can correct it, which means the
  // dialog they typed into rather than the list behind it.
  it("stays open and shows a refusal", async () => {
    createUser.mockRejectedValue(new Error("that address already has an account"));
    const onClose = vi.fn();
    const person = userEvent.setup();
    render(<AddUserModal onAdded={() => {}} onClose={onClose} />);

    await person.type(screen.getByLabelText(/Email/), "ada@example.com");
    await person.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByText(/already has an account/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
