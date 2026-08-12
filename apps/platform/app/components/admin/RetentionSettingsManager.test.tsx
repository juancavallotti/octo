import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getRetention = vi.fn();
const saveRetention = vi.fn();
const runRetention = vi.fn();
vi.mock("@/app/model/retention", () => ({
  getRetention: () => getRetention(),
  saveRetention: (input: unknown) => saveRetention(input),
  runRetention: () => runRetention(),
}));

import RetentionSettingsManager from "./RetentionSettingsManager";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";

const CONFIGURED = { logsDays: 30, tracesDays: 7, updatedAt: "2026-08-11T03:00:00Z" };
const KEEP_EVERYTHING = { logsDays: 0, tracesDays: 0, updatedAt: null };

const SWEPT = {
  logsDeleted: 120,
  tracesDeleted: 44,
  traceSummariesDeleted: 6,
  logsCutoff: "2026-07-12T03:00:00Z",
  tracesCutoff: "2026-08-04T03:00:00Z",
  durationMs: 1500,
};

function renderManager() {
  return render(
    <ConfirmProvider>
      <RetentionSettingsManager />
    </ConfirmProvider>,
  );
}

function logsInput(): HTMLInputElement {
  return screen.getByLabelText("Keep logs for (days)") as HTMLInputElement;
}

function tracesInput(): HTMLInputElement {
  return screen.getByLabelText("Keep traces for (days)") as HTMLInputElement;
}

/** Wait for the initial load to land, so nothing below races the seeded zeros. */
async function loaded(days = "30") {
  await waitFor(() => expect(logsInput().value).toBe(days));
}

describe("RetentionSettingsManager", () => {
  beforeEach(() => {
    getRetention.mockResolvedValue(CONFIGURED);
    saveRetention.mockResolvedValue(CONFIGURED);
    runRetention.mockResolvedValue(SWEPT);
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it("shows the stored windows", async () => {
    renderManager();
    await loaded();

    expect(tracesInput().value).toBe("7");
    expect(screen.getByText("Deleted after 30 days.")).toBeTruthy();
    expect(screen.getByText("Deleted after 7 days.")).toBeTruthy();
  });

  // A bare 0 in a box labelled "days" reads as a value nobody set. Saying what it
  // means is the difference between a default and an oversight.
  it("says what a zero window means", async () => {
    getRetention.mockResolvedValue(KEEP_EVERYTHING);
    renderManager();
    await loaded("0");

    expect(screen.getAllByText("Kept forever — nothing is deleted.")).toHaveLength(2);
  });

  it("sends both windows on a save", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.clear(tracesInput());
    await user.type(tracesInput(), "14");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveRetention).toHaveBeenCalled());
    expect(saveRetention.mock.calls[0][0]).toEqual({ logsDays: 30, tracesDays: 14 });
  });

  // The form seeds itself with zeros before the request resolves. Without the
  // load gate, a fast click would save "keep everything" over the stored policy.
  it("cannot save before the policy has loaded", () => {
    let resolve: (p: typeof CONFIGURED) => void = () => {};
    getRetention.mockReturnValue(new Promise((r) => (resolve = r)));
    renderManager();

    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    resolve(CONFIGURED);
  });

  it("refuses a window that is not a whole number of days", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.clear(logsInput());
    await user.type(logsInput(), "-5");

    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(screen.getByText(/whole number of days/)).toBeTruthy();
  });

  it("refuses a window beyond the maximum the service stores", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.clear(logsInput());
    await user.type(logsInput(), "3651");

    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it("deletes after confirmation and reports what went", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.click(screen.getByRole("button", { name: "Delete now" }));
    // Scoped to the dialog: the page's button carries the same label, and the
    // one that matters is the one behind the confirmation.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/older than 30 days.*older than 7 days/)).toBeTruthy();
    await user.click(within(dialog).getByRole("button", { name: "Delete now" }));

    await waitFor(() => expect(runRetention).toHaveBeenCalled());
    expect(
      screen.getByText("Deleted 120 log events, 44 trace records, 6 traces."),
    ).toBeTruthy();
  });

  it("does not delete when the confirmation is dismissed", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.click(screen.getByRole("button", { name: "Delete now" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: /Cancel/ }));

    expect(runRetention).not.toHaveBeenCalled();
  });

  // A sweep applies the stored policy. Running it against numbers the operator
  // can see they have edited would delete to a policy the confirmation did not
  // describe.
  it("will not run while the form has unsaved changes", async () => {
    const user = userEvent.setup();
    renderManager();
    await loaded();

    await user.clear(tracesInput());
    await user.type(tracesInput(), "1");

    expect(
      (screen.getByRole("button", { name: "Delete now" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/Save your changes first/)).toBeTruthy();
  });

  it("will not run when both streams are kept forever", async () => {
    getRetention.mockResolvedValue(KEEP_EVERYTHING);
    renderManager();
    await loaded("0");

    expect(
      (screen.getByRole("button", { name: "Delete now" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/Nothing to delete/)).toBeTruthy();
  });

  it("surfaces a failure inline", async () => {
    const user = userEvent.setup();
    saveRetention.mockRejectedValue(new Error("forbidden"));
    renderManager();
    await loaded();

    await user.clear(tracesInput());
    await user.type(tracesInput(), "14");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("forbidden")).toBeTruthy();
  });
});
