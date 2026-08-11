import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Mock the model so the component's load/save calls are observable.
const getEmailSettings = vi.fn();
const saveEmailSettings = vi.fn();
const sendTestEmail = vi.fn();
vi.mock("@/app/model/siteSettings", () => ({
  getEmailSettings: () => getEmailSettings(),
  saveEmailSettings: (input: unknown) => saveEmailSettings(input),
  sendTestEmail: (input: unknown) => sendTestEmail(input),
}));

import EmailSettingsManager from "./EmailSettingsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const CONFIGURED = {
  fromEmail: "notifications@acme.com",
  fromName: "Acme",
  replyTo: "support@acme.com",
  configured: true,
  last4: "9f2a",
  updatedAt: "2026-01-02T00:00:00Z",
  encryptionAvailable: true,
};

/** Render inside a ConfirmProvider so useConfirm() has its context. */
function renderManager() {
  return render(
    <ConfirmProvider>
      <EmailSettingsManager />
    </ConfirmProvider>,
  );
}

describe("EmailSettingsManager", () => {
  beforeEach(() => {
    getEmailSettings.mockResolvedValue(CONFIGURED);
    saveEmailSettings.mockResolvedValue(CONFIGURED);
    sendTestEmail.mockResolvedValue({ messageId: "msg-1" });
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("shows the stored settings and which key is held", async () => {
    renderManager();

    await waitFor(() =>
      expect(screen.getByDisplayValue("notifications@acme.com")).toBeTruthy(),
    );
    expect(screen.getByDisplayValue("Acme")).toBeTruthy();
    expect(screen.getByText(/9f2a/)).toBeTruthy();
  });

  it("says so when no key is stored", async () => {
    getEmailSettings.mockResolvedValue({ ...CONFIGURED, configured: false, last4: "" });
    renderManager();

    await waitFor(() => expect(screen.getByText("No API key stored.")).toBeTruthy());
  });

  // The headline behaviour. Every field is submitted on every save, so a save that
  // did not retype the key must not mention it at all — otherwise editing a display
  // name would destroy the stored key.
  it("omits apiKey entirely when none was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("notifications@acme.com")).toBeTruthy(),
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalled());
    const payload = saveEmailSettings.mock.calls[0][0];
    expect(payload).not.toHaveProperty("apiKey");
    expect(payload.fromEmail).toBe("notifications@acme.com");
  });

  it("sends the key when one was typed", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("notifications@acme.com")).toBeTruthy(),
    );

    await user.type(screen.getByPlaceholderText("re_..."), "re_typedkey123");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalled());
    expect(saveEmailSettings.mock.calls[0][0]).toMatchObject({
      apiKey: "re_typedkey123",
    });
  });

  it("keeps Save disabled until the from address looks like an address", async () => {
    const user = userEvent.setup();
    getEmailSettings.mockResolvedValue({ ...CONFIGURED, fromEmail: "" });
    renderManager();
    await waitFor(() => expect(screen.getByText("Email")).toBeTruthy());

    const save = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);

    await user.type(screen.getByPlaceholderText("notifications@example.com"), "not-an-email");
    expect(save.disabled).toBe(true);

    await user.clear(screen.getByPlaceholderText("notifications@example.com"));
    await user.type(screen.getByPlaceholderText("notifications@example.com"), "ops@acme.com");
    expect(save.disabled).toBe(false);
  });

  it("removes the stored key only after confirming", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Remove" }));

    // The dialog carries its own "Remove", so scope to it rather than to the page.
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalled());
    expect(saveEmailSettings.mock.calls[0][0]).toMatchObject({ apiKey: "" });
  });

  it("leaves the key alone when the removal is cancelled", async () => {
    const user = userEvent.setup();
    renderManager();
    await waitFor(() => expect(screen.getByText(/9f2a/)).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "Remove" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    expect(saveEmailSettings).not.toHaveBeenCalled();
  });

  it("disables the key field and explains why when encryption is unavailable", async () => {
    getEmailSettings.mockResolvedValue({ ...CONFIGURED, encryptionAvailable: false });
    renderManager();

    await waitFor(() => expect(screen.getByText(/kv.encryptionKey/)).toBeTruthy());
    const key = screen.getByPlaceholderText("re_...") as HTMLInputElement;
    expect(key.disabled).toBe(true);
    // The other fields stay editable: carrying the stored key forward needs no cipher.
    const from = screen.getByDisplayValue("notifications@acme.com") as HTMLInputElement;
    expect(from.disabled).toBe(false);
  });

  it("renders the orchestrator's message when a save fails", async () => {
    const user = userEvent.setup();
    saveEmailSettings.mockRejectedValue(new Error("invalid from address"));
    renderManager();
    await waitFor(() =>
      expect(screen.getByDisplayValue("notifications@acme.com")).toBeTruthy(),
    );

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("invalid from address")).toBeTruthy();
  });
});
