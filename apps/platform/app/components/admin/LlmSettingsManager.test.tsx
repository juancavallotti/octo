import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getLlmSettings = vi.fn();
const saveLlmSettings = vi.fn();
// The provider loads all three sources, so every one it reads has to be mocked
// even by a suite that only exercises this section.
vi.mock("@/app/model/siteSettings", () => ({
  getLlmSettings: () => getLlmSettings(),
  saveLlmSettings: (input: unknown) => saveLlmSettings(input),
  getWebSearchSettings: () => Promise.resolve(null),
  saveWebSearchSettings: () => Promise.resolve(null),
}));
vi.mock("@/app/model/agent", () => ({
  getAgentStatus: () => Promise.resolve(null),
  setAgentDeploymentSettings: () => Promise.resolve(null),
}));

import LlmSettingsManager from "./LlmSettingsManager";
import AgentSettingsForm from "./AgentSettingsForm";
import AgentSaveBar from "./AgentSaveBar";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const CONFIGURED = {
  provider: "ANTHROPIC",
  model: "claude-sonnet-4-6",
  configured: true,
  last4: "9f2a",
  updatedAt: "2026-01-02T00:00:00Z",
  encryptionAvailable: true,
};

// Rendered with the page's provider and its one Save, because that is where the
// draft and the writing now live — this section renders fields and nothing else.
function renderManager() {
  return render(
    <ConfirmProvider>
      <AgentSettingsForm>
        <LlmSettingsManager />
        <AgentSaveBar />
      </AgentSettingsForm>
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

    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe(
      "ANTHROPIC",
    );
    expect(screen.getByText(/9f2a/)).toBeTruthy();
  });

  // Same headline behaviour as the email form: switching model or provider must not
  // destroy the credentials sitting beside them.
  it("omits apiKey entirely when none was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    // Something has to change for there to be a save at all now — the page's one
    // Save writes what differs, and an unchanged form differs in nothing.
    await user.type(screen.getByDisplayValue("claude-sonnet-4-6"), "-2");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0]).not.toHaveProperty("apiKey");
  });

  it("sends the key when one was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    await user.type(
      screen.getByPlaceholderText("sk-ant-..."),
      "sk-ant-typed1234",
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0]).toMatchObject({
      apiKey: "sk-ant-typed1234",
    });
  });

  it("swaps the model to the new provider's default when it was untouched", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    await user.selectOptions(screen.getByRole("combobox"), "OPENAI");

    expect(modelInput().value).toBe("gpt-5.4");
  });

  it("swaps in OpenRouter's vendor-prefixed default", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    await user.selectOptions(screen.getByRole("combobox"), "OPENROUTER");

    // Ids here carry the vendor as a prefix; there is no bare model name.
    expect(modelInput().value).toBe("anthropic/claude-sonnet-4.5");
  });

  // A model the operator typed is theirs; switching provider must not discard it.
  it("keeps a custom model when the provider changes", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    const model = screen.getByDisplayValue(
      "claude-sonnet-4-6",
    ) as HTMLInputElement;
    await user.clear(model);
    await user.type(model, "my-finetuned-model");

    await user.selectOptions(screen.getByRole("combobox"), "GOOGLE");

    expect(
      (screen.getByDisplayValue("my-finetuned-model") as HTMLInputElement)
        .value,
    ).toBe("my-finetuned-model");
  });

  it("keeps Save disabled while the model is empty", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    const save = screen.getByRole("button", {
      name: "Save",
    }) as HTMLButtonElement;
    // Disabled to begin with, because nothing has changed: the page's one Save
    // offers itself only when there is something to write.
    expect(save.disabled).toBe(true);

    await user.type(screen.getByDisplayValue("claude-sonnet-4-6"), "-2");
    expect(save.disabled).toBe(false);

    await user.clear(screen.getByDisplayValue("claude-sonnet-4-6-2"));
    expect(save.disabled).toBe(true);
  });

  // This used to guard against the form saving its own seeded defaults over what
  // was stored, because it rendered a provider and model before the load
  // resolved. That hazard is now structurally gone: the draft IS the load, and
  // nothing is dirty until a person types. So the assertion is the stronger one —
  // Save never offers itself for a form nobody has edited, loaded or not.
  it("never offers to save a form nobody has edited", async () => {
    let resolveLoad: (v: typeof CONFIGURED) => void = () => {};
    getLlmSettings.mockReturnValue(
      new Promise<typeof CONFIGURED>((r) => {
        resolveLoad = r;
      }),
    );
    renderManager();

    const save = screen.getByRole("button", {
      name: "Save",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    resolveLoad(CONFIGURED);
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );
    expect(save.disabled).toBe(true);
  });

  it("will not remove the key while the model is empty", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.clear(modelInput());
    await user.click(screen.getByRole("button", { name: "Remove" }));

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(saveLlmSettings).not.toHaveBeenCalled();
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
    getLlmSettings.mockResolvedValue({
      ...CONFIGURED,
      encryptionAvailable: false,
    });
    renderManager();

    await waitFor(() =>
      expect(screen.getByText(/kv.encryptionKey/)).toBeTruthy(),
    );
    expect(
      (screen.getByPlaceholderText("sk-ant-...") as HTMLInputElement).disabled,
    ).toBe(true);
    // Provider and model stay editable: carrying the key forward needs no cipher.
    expect((screen.getByRole("combobox") as HTMLSelectElement).disabled).toBe(
      false,
    );
  });

  it("renders the orchestrator's message when a save fails", async () => {
    const user = userEvent.setup();
    saveLlmSettings.mockRejectedValue(
      new Error(
        "invalid provider (expected ANTHROPIC, OPENAI, GOOGLE or OPENROUTER)",
      ),
    );
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    await user.type(screen.getByDisplayValue("claude-sonnet-4-6"), "-2");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/invalid provider/)).toBeTruthy();
  });

  // A stored provider we no longer offer would otherwise leave the select showing
  // something other than what a save would send.
  it("falls back when the stored provider is not one we offer", async () => {
    getLlmSettings.mockResolvedValue({ ...CONFIGURED, provider: "COHERE" });
    renderManager();

    await waitFor(() =>
      expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe(
        "ANTHROPIC",
      ),
    );
  });

  it("trims the model before saving", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy(),
    );

    const model = modelInput();
    await user.clear(model);
    await user.type(model, "  spaced-model  ");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveLlmSettings).toHaveBeenCalled());
    expect(saveLlmSettings.mock.calls[0][0].model).toBe("spaced-model");
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
      expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe(
        "ANTHROPIC",
      ),
    );
    expect(screen.getByDisplayValue("claude-sonnet-4-6")).toBeTruthy();
  });
});
