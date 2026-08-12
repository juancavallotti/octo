import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const sendTestEmail = vi.fn();
vi.mock("@/app/model/siteSettings", () => ({
  sendTestEmail: (input: unknown) => sendTestEmail(input),
}));

import TestEmailPanel from "./TestEmailPanel";

const DRAFT = {
  fromEmail: "notifications@acme.com",
  fromName: "Acme",
  replyTo: "",
};

function renderPanel(overrides: Partial<Parameters<typeof TestEmailPanel>[0]> = {}) {
  return render(
    <TestEmailPanel
      draft={() => DRAFT}
      busy={false}
      configured
      hasTypedKey={false}
      {...overrides}
    />,
  );
}

describe("TestEmailPanel", () => {
  beforeEach(() => {
    sendTestEmail.mockResolvedValue({ messageId: "msg-1" });
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("sends the current draft to the recipient", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByPlaceholderText("you@example.com"), "me@example.com");
    await user.click(screen.getByRole("button", { name: "Send test email" }));

    await waitFor(() => expect(sendTestEmail).toHaveBeenCalled());
    expect(sendTestEmail.mock.calls[0][0]).toEqual({ ...DRAFT, to: "me@example.com" });
    expect(await screen.findByText(/Test message sent to me@example.com/)).toBeTruthy();
  });

  it("trims the recipient", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByPlaceholderText("you@example.com"), "  me@example.com  ");
    await user.click(screen.getByRole("button", { name: "Send test email" }));

    await waitFor(() => expect(sendTestEmail).toHaveBeenCalled());
    expect(sendTestEmail.mock.calls[0][0].to).toBe("me@example.com");
  });

  // The provider's own wording is what tells the operator what to fix, so it has to
  // reach the screen unchanged.
  it("renders the provider's message verbatim on failure", async () => {
    const user = userEvent.setup();
    sendTestEmail.mockRejectedValue(
      new Error("invalid Resend API key: email provider rejected the api key"),
    );
    renderPanel();

    await user.type(screen.getByPlaceholderText("you@example.com"), "me@example.com");
    await user.click(screen.getByRole("button", { name: "Send test email" }));

    expect(await screen.findByText(/invalid Resend API key/)).toBeTruthy();
  });

  it("cannot send without a recipient", async () => {
    renderPanel();

    expect(
      (screen.getByRole("button", { name: "Send test email" }) as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  // With no key stored and none typed there is nothing to send with; the orchestrator
  // would 409, and this saves the round trip.
  it("cannot send with no key on either side, and says why", async () => {
    const user = userEvent.setup();
    renderPanel({ configured: false, hasTypedKey: false });

    await user.type(screen.getByPlaceholderText("you@example.com"), "me@example.com");

    expect(
      (screen.getByRole("button", { name: "Send test email" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/Enter an API key above, or save one/)).toBeTruthy();
  });

  it("can send on an unsaved key alone, which is the point of the button", async () => {
    const user = userEvent.setup();
    renderPanel({ configured: false, hasTypedKey: true });

    await user.type(screen.getByPlaceholderText("you@example.com"), "me@example.com");
    await user.click(screen.getByRole("button", { name: "Send test email" }));

    await waitFor(() => expect(sendTestEmail).toHaveBeenCalled());
  });
});
