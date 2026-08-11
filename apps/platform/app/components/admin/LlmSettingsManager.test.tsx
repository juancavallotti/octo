import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getLlmSettings = vi.fn();
const saveLlmSettings = vi.fn();
vi.mock("@/app/model/siteSettings", () => ({
  getLlmSettings: () => getLlmSettings(),
  saveLlmSettings: (input: unknown) => saveLlmSettings(input),
}));

import LlmSettingsManager from "./LlmSettingsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const CONFIGURED = {
  provider: "ANTHROPIC",
  model: "claude-sonnet-4-6",
  configured: true,
  last4: "9f2a",
  updatedAt: "2026-01-02T00:00:00Z",
  encryptionAvailable: true,
};

function renderManager() {
  return render(
    <ConfirmProvider>
      <LlmSettingsManager />
    </ConfirmProvider>,
  );
}

/** The model input, addressed by its label rather than by its current value. */
function modelInput(): HTMLInputElement {
  return screen.getByLabelText(/^Model/) as HTMLInputElement;
}

describe("LlmSettingsManager", () => {
  beforeEach(() => {
    getLlmSettings.mockResolvedValue(CONFIGURED);
    saveLlmSettings.mockResolvedValue(CONFIGURED);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("shows the stored provider, model and which key is held", async () => {
    renderManager();

    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("ANTHROPIC");
    expect(screen.getByText(/9f2a/)).toBeTruthy();
  });

  // Same headline behaviour as the email form: switching model or provider must not
  // destroy the credentials sitting beside them.
  it("omits apiKey entirely when none was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0]).not.toHaveProperty("apiKey");
  });

  it("sends the key when one was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    await user.type(screen.getByPlaceholderText("sk-ant-..."), "sk-ant-typed1234");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0]).toMatchObject({ apiKey: "sk-ant-typed1234" });
  });

  it("swaps the model to the new provider's default when it was untouched", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    await user.selectOptions(screen.getByRole("combobox"), "OPENAI");

    expect(modelInput().value).toBe("gpt-5.4");
  });

  // A model the operator typed is theirs; switching provider must not discard it.
  it("keeps a custom model when the provider changes", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    const model = screen.getByDisplayValue("claude-sonnet-4-6") as HTMLInputElement;
    await user.clear(model);
    await user.type(model, "my-finetuned-model");

    await user.selectOptions(screen.getByRole("combobox"), "GOOGLE");

    expect((screen.getByDisplayValue("my-finetuned-model") as HTMLInputElement).value).toBe(
      "my-finetuned-model",
    );
  });

  it("keeps Save disabled while the model is empty", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    const save = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    expect(save.disabled).toBe(false);

    await user.clear(screen.getByDisplayValue("claude-sonnet-4-6"));
    expect(save.disabled).toBe(true);
  });

  it("removes the stored key only after confirming", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Remove" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0]).toMatchObject({ apiKey: "" });
  });

  it("disables the key field and explains why when encryption is unavailable", async () => {
    getLlmSettings.mockResolvedValue({ ...CONFIGURED, encryptionAvailable: false });
    renderManager();

    await waitFor(() => expect(screen.getByText(/kv.encryptionKey/)).toBeTruthy());
    expect((screen.getByPlaceholderText("sk-ant-...") as HTMLInputElement).disabled).toBe(true);
    // Provider and model stay editable: carrying the key forward needs no cipher.
    expect((screen.getByRole("combobox") as HTMLSelectElement).disabled).toBe(false);
  });

  it("renders the orchestrator's message when a save fails", async () => {
    const user = userEvent.setup();
    saveLlmSettings.mockRejectedValue(
      new Error("invalid provider (expected ANTHROPIC, OPENAI or GOOGLE)"),
    );
    renderManager();
    await waitFor(() => expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/invalid provider/)).toBeTruthy();
  });

  it("falls back to the first provider and its default when nothing is stored", async () => {
    getLlmSettings.mockResolvedValue({
      provider: "",
      model: "",
      configured: false,
      last4: "",
      updatedAt: null,
      encryptionAvailable: true,
    });
    renderManager();

    await waitFor(() =>
      expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("ANTHROPIC"),
    );
    expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy();
  });
});
