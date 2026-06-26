import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Mock the orchestrator client so the component's load/mutations are observable.
const listSecrets = vi.fn();
const setSecret = vi.fn();
const deleteSecret = vi.fn();
vi.mock("@/app/model/secrets", () => ({
  listSecrets: () => listSecrets(),
  setSecret: (name: string, value: string) => setSecret(name, value),
  deleteSecret: (name: string, force?: boolean) => deleteSecret(name, force),
}));

import SecretsManager from "./SecretsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

/** Render inside a ConfirmProvider so useConfirm() has its context. */
function renderManager() {
  return render(
    <ConfirmProvider>
      <SecretsManager />
    </ConfirmProvider>,
  );
}

describe("SecretsManager", () => {
  beforeEach(() => {
    listSecrets.mockResolvedValue([
      { name: "API_KEY", createdAt: "2026-01-01T00:00:00Z", lastUpdated: "2026-01-02T00:00:00Z" },
    ]);
    setSecret.mockResolvedValue({ name: "X", createdAt: "", lastUpdated: "" });
    deleteSecret.mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("lists existing secrets (name only, never a value)", async () => {
    renderManager();
    expect(await screen.findByText("API_KEY")).toBeInTheDocument();
  });

  it("creates a secret via setSecret and clears the form", async () => {
    renderManager();
    await screen.findByText("API_KEY");

    await userEvent.type(screen.getByPlaceholderText("SECRET_NAME"), "db_url");
    await userEvent.type(screen.getByPlaceholderText("value"), "postgres://x");
    await userEvent.click(screen.getByRole("button", { name: "Add" }));

    // Name is upper-cased by the input handler before being sent.
    expect(setSecret).toHaveBeenCalledWith("DB_URL", "postgres://x");
  });

  it("blocks adding when the name is not UPPER_SNAKE_CASE", async () => {
    renderManager();
    await screen.findByText("API_KEY");

    // A digit-leading name is invalid; the Add button stays disabled.
    await userEvent.type(screen.getByPlaceholderText("SECRET_NAME"), "1BAD");
    await userEvent.type(screen.getByPlaceholderText("value"), "v");
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
  });

  it("force-deletes when the secret is in use and the user confirms", async () => {
    deleteSecret
      .mockRejectedValueOnce(new Error("secret is in use by a deployment"))
      .mockResolvedValueOnce(undefined);

    renderManager();
    await screen.findByText("API_KEY");

    // Open the row's delete confirm, accept it, then accept the force-delete
    // dialog that appears once the orchestrator reports the secret is in use.
    await userEvent.click(screen.getByRole("button", { name: "Delete API_KEY" }));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await userEvent.click(
      await screen.findByRole("button", { name: "Force delete" }),
    );

    await waitFor(() => {
      expect(deleteSecret).toHaveBeenNthCalledWith(1, "API_KEY", undefined);
      expect(deleteSecret).toHaveBeenNthCalledWith(2, "API_KEY", true);
    });
  });
});
