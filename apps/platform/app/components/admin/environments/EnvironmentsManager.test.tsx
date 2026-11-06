import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const listEnvironments = vi.fn();
const createEnvironment = vi.fn();
const updateEnvironment = vi.fn();
const deleteEnvironment = vi.fn();
vi.mock("@/app/model/environments", () => ({
  listEnvironments: () => listEnvironments(),
  createEnvironment: (input: unknown) => createEnvironment(input),
  updateEnvironment: (name: string, patch: unknown) => updateEnvironment(name, patch),
  deleteEnvironment: (name: string) => deleteEnvironment(name),
}));

import EnvironmentsManager from "./EnvironmentsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const DEFAULT = {
  name: "octo",
  namespace: "octo-system",
  displayName: "Default",
  description: "",
  default: true,
};
const STAGING = {
  name: "staging",
  namespace: "octo-staging",
  displayName: "Staging",
  description: "pre-prod",
  default: false,
  createdAt: "2026-11-01T00:00:00Z",
};

function renderManager() {
  return render(
    <ConfirmProvider>
      <EnvironmentsManager />
    </ConfirmProvider>,
  );
}

describe("EnvironmentsManager", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listEnvironments.mockResolvedValue([DEFAULT, STAGING]);
  });

  it("lists the default environment without edit or delete", async () => {
    renderManager();
    const row = await screen.findByRole("listitem", { name: "Default" });
    expect(within(row).getByText(/namespace octo-system/)).toBeTruthy();
    expect(within(row).queryByRole("button")).toBeNull();

    const staging = screen.getByRole("listitem", { name: "Staging" });
    expect(within(staging).getByRole("button", { name: "Delete Staging" })).toBeTruthy();
  });

  it("creates an environment and clears the form", async () => {
    createEnvironment.mockResolvedValue({ ...STAGING, name: "qa", namespace: "octo-qa" });
    renderManager();
    const form = await screen.findByRole("form", { name: "New environment" });

    await userEvent.type(within(form).getByPlaceholderText("staging"), "qa");
    expect(within(form).getByPlaceholderText("octo-qa")).toBeTruthy();
    await userEvent.click(within(form).getByRole("button", { name: /Create environment/ }));

    await waitFor(() =>
      expect(createEnvironment).toHaveBeenCalledWith({
        name: "qa",
        displayName: undefined,
        namespace: undefined,
        description: undefined,
      }),
    );
    await waitFor(() => expect(within(form).getByPlaceholderText("staging")).toHaveProperty("value", ""));
    expect(listEnvironments).toHaveBeenCalledTimes(2);
  });

  it("refuses a name that is taken or reserved before asking the orchestrator", async () => {
    renderManager();
    const form = await screen.findByRole("form", { name: "New environment" });
    const name = within(form).getByPlaceholderText("staging");
    const create = within(form).getByRole("button", { name: /Create environment/ });

    await userEvent.type(name, "staging");
    expect(screen.getByText("That environment already exists.")).toBeTruthy();
    expect(create).toHaveProperty("disabled", true);

    await userEvent.clear(name);
    await userEvent.type(name, "octo");
    expect(screen.getByText("\u201cocto\u201d is the default environment.")).toBeTruthy();
    expect(create).toHaveProperty("disabled", true);
  });

  it("deletes after confirming, and shows the orchestrator's refusal", async () => {
    deleteEnvironment.mockRejectedValue(
      new Error("the environment still has workloads; delete them first"),
    );
    renderManager();
    const row = await screen.findByRole("listitem", { name: "Staging" });

    await userEvent.click(within(row).getByRole("button", { name: "Delete Staging" }));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteEnvironment).toHaveBeenCalledWith("staging"));
    expect((await screen.findByRole("alert")).textContent).toMatch(/still has workloads/);
  });

  it("edits the display name and description", async () => {
    updateEnvironment.mockResolvedValue(STAGING);
    renderManager();
    const row = await screen.findByRole("listitem", { name: "Staging" });

    await userEvent.click(within(row).getByRole("button", { name: "Edit Staging" }));
    const display = within(row).getByLabelText("Display name");
    await userEvent.clear(display);
    await userEvent.type(display, "Stage");
    await userEvent.click(within(row).getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateEnvironment).toHaveBeenCalledWith("staging", {
        displayName: "Stage",
        description: "pre-prod",
      }),
    );
  });
});
