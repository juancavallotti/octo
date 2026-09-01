import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getWebSearchSettings = vi.fn();
const saveWebSearchSettings = vi.fn();
vi.mock("@/app/model/siteSettings", () => ({
  getWebSearchSettings: () => getWebSearchSettings(),
  saveWebSearchSettings: (input: unknown) => saveWebSearchSettings(input),
}));

import WebSearchSettingsManager from "./WebSearchSettingsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const CONFIGURED = {
  provider: "PARALLEL",
  configured: true,
  last4: "9f2a",
  updatedAt: "2026-01-02T00:00:00Z",
  encryptionAvailable: true,
};

const UNCONFIGURED = {
  ...CONFIGURED,
  configured: false,
  last4: "",
  updatedAt: null,
};

function renderManager() {
  return render(
    <ConfirmProvider>
      <WebSearchSettingsManager />
    </ConfirmProvider>,
  );
}

function keyField(): HTMLInputElement {
  return screen.getByPlaceholderText(
    "Your Parallel API key",
  ) as HTMLInputElement;
}

describe("WebSearchSettingsManager", () => {
  beforeEach(() => {
    getWebSearchSettings.mockResolvedValue(CONFIGURED);
    saveWebSearchSettings.mockResolvedValue(CONFIGURED);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("says which key is stored, and never shows it", async () => {
    renderManager();

    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());
    expect(keyField().value).toBe("");
  });

  it("says plainly when there is no key", async () => {
    getWebSearchSettings.mockResolvedValue(UNCONFIGURED);
    renderManager();

    await waitFor(() =>
      expect(screen.getByText(/No API key stored/)).toBeTruthy(),
    );
  });

  it("sends the key when one was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.type(keyField(), "parallel-typed-key");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveWebSearchSettings).toHaveBeenCalled());
    expect(saveWebSearchSettings.mock.calls[0][0]).toMatchObject({
      apiKey: "parallel-typed-key",
    });
  });

  // The key is the only field, so a save with an empty draft writes nothing. Offering
  // it would read as though it stored what was typed.
  it("keeps Save disabled until a key is typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    const save = screen.getByRole("button", {
      name: "Save",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    await user.type(keyField(), "parallel-typed-key");
    expect(save.disabled).toBe(false);
  });

  it("cannot save before the settings have loaded", async () => {
    const user = userEvent.setup();
    let resolveLoad: (v: typeof CONFIGURED) => void = () => {};
    getWebSearchSettings.mockReturnValue(
      new Promise<typeof CONFIGURED>((r) => {
        resolveLoad = r;
      }),
    );
    renderManager();

    const save = screen.getByRole("button", {
      name: "Save",
    }) as HTMLButtonElement;
    await user.type(keyField(), "parallel-typed-key");
    expect(save.disabled).toBe(true);

    resolveLoad(CONFIGURED);
    await waitFor(() => expect(save.disabled).toBe(false));
  });

  it("removes the stored key only after confirming", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Remove" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(saveWebSearchSettings).toHaveBeenCalled());
    expect(saveWebSearchSettings.mock.calls[0][0]).toMatchObject({
      apiKey: "",
    });
  });

  it("disables the key field and explains why when encryption is unavailable", async () => {
    getWebSearchSettings.mockResolvedValue({
      ...CONFIGURED,
      encryptionAvailable: false,
    });
    renderManager();

    await waitFor(() =>
      expect(screen.getByText(/kv.encryptionKey/)).toBeTruthy(),
    );
    expect(keyField().disabled).toBe(true);
  });

  it("renders the orchestrator's message when a save fails", async () => {
    const user = userEvent.setup();
    saveWebSearchSettings.mockRejectedValue(new Error("invalid api key"));
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.type(keyField(), "parallel-typed-key");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/invalid api key/)).toBeTruthy();
  });

  // A save that succeeded is not yet a change the agent sees: the key reaches him
  // when his deployment's bindings are written.
  it("says the change reaches him on his next roll-out", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.type(keyField(), "parallel-typed-key");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/Roll him out below/)).toBeTruthy();
  });
});
