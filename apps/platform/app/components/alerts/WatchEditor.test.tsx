import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
  usePathname: () => "/platform/metrics/alerts/new",
  useSearchParams: () => new URLSearchParams(),
}));

const createWatch = vi.fn();
const saveWatch = vi.fn();
const deleteWatch = vi.fn();
const previewWatch = vi.fn();
vi.mock("@/app/model/alerts", () => ({
  createWatch: (...a: unknown[]) => createWatch(...a),
  saveWatch: (...a: unknown[]) => saveWatch(...a),
  deleteWatch: (...a: unknown[]) => deleteWatch(...a),
  previewWatch: (...a: unknown[]) => previewWatch(...a),
}));

const confirm = vi.fn();
vi.mock("@/app/components/ConfirmDialog", () => ({
  useConfirm: () => confirm,
}));

import { WatchEditor } from "./WatchEditor";
import { newWatch } from "./catalogue";

function renderEditor(watchId: string | null = null) {
  return render(<WatchEditor initial={newWatch()} watchId={watchId} />);
}

describe("WatchEditor", () => {
  beforeEach(() => {
    push.mockReset();
    createWatch.mockReset().mockResolvedValue({ id: "w_1" });
    saveWatch.mockReset().mockResolvedValue({ id: "w_1" });
    deleteWatch.mockReset().mockResolvedValue(undefined);
    previewWatch.mockReset();
    confirm.mockReset().mockResolvedValue(true);
  });

  // The composable part: a watch is a set, and the set has to be editable.
  it("adds and removes conditions", async () => {
    const user = userEvent.setup();
    renderEditor();

    expect(screen.getByText("Condition 1")).toBeInTheDocument();
    // With one condition there is nothing to remove: a watch with none asks
    // nothing, and the service refuses it.
    expect(
      screen.queryByRole("button", { name: "Remove condition 1" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /add a condition/i }));
    expect(screen.getByText("Condition 2")).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Remove condition 2" }),
    );
    expect(screen.queryByText("Condition 2")).not.toBeInTheDocument();
  });

  it("sends the whole set, joined by the combinator", async () => {
    const user = userEvent.setup();
    renderEditor();

    await user.type(screen.getByLabelText("Name"), "checkout errors");
    await user.selectOptions(screen.getByLabelText("Combinator"), "any");
    await user.click(screen.getByRole("button", { name: /add a condition/i }));
    await user.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(createWatch).toHaveBeenCalled());
    const sent = createWatch.mock.calls[0][0] as Record<string, unknown>;
    expect(sent.name).toBe("checkout errors");
    expect(sent.combinator).toBe("any");
    expect((sent.conditions as unknown[]).length).toBe(2);
  });

  // Switching what a condition is judged by must not carry the previous kind's
  // parameters across: a spike's baseline means nothing to a threshold, and the
  // service would refuse a field nobody can see on the form.
  it("resets a condition's parameters when its kind changes", async () => {
    const user = userEvent.setup();
    renderEditor();

    await user.type(screen.getByLabelText("Name"), "x");
    await user.selectOptions(
      screen.getByLabelText("Condition 1 kind"),
      "spike",
    );
    expect(screen.getByLabelText("Condition 1 baseline")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("Condition 1 threshold"),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(createWatch).toHaveBeenCalled());

    const sent = createWatch.mock.calls[0][0] as {
      conditions: { params: Record<string, unknown> }[];
    };
    expect(sent.conditions[0].params.threshold).toBeUndefined();
    expect(sent.conditions[0].params.direction).toBe("up");
  });

  // Changing the source rescopes the metric, or the form would offer a trace
  // metric against the log tables.
  it("picks a metric that belongs to the chosen source", async () => {
    const user = userEvent.setup();
    renderEditor();

    await user.selectOptions(
      screen.getByLabelText("Condition 1 source"),
      "logs",
    );
    expect(screen.getByLabelText("Condition 1 levels")).toBeInTheDocument();

    // Pod stats have no catalogue: the names come from the runtime, so the
    // field becomes free text rather than an empty dropdown.
    await user.selectOptions(
      screen.getByLabelText("Condition 1 source"),
      "pod_stats",
    );
    expect(screen.getByLabelText("Condition 1 metric")).toHaveAttribute(
      "value",
      "",
    );
  });

  it("previews without saving or navigating", async () => {
    const user = userEvent.setup();
    previewWatch.mockResolvedValue({
      status: "ok",
      verdict: "false",
      matched: 0,
      total: 1,
      degraded: false,
      windowFrom: "2026-09-06T09:55:00Z",
      windowTo: "2026-09-06T10:00:00Z",
      outcomes: [
        {
          conditionId: "c_1",
          kind: "threshold",
          label: "error_rate gt over 5m",
          threshold: 0.05,
          observed: 0.01,
          samples: 5,
          windowFrom: "",
          windowTo: "",
          verdict: "false",
          reason: "threshold_unmet",
        },
      ],
    });
    renderEditor();

    await user.click(screen.getByRole("button", { name: /try it now/i }));

    await waitFor(() => expect(previewWatch).toHaveBeenCalled());
    expect(createWatch).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    // The declining condition explains itself in words, which is the point of
    // previewing at all.
    expect(
      await screen.findByText(/did not reach the threshold/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/records nothing and notifies nobody/i),
    ).toBeInTheDocument();
  });

  // The service's message names the field and the bound, so it is shown rather
  // than replaced with something generic.
  it("surfaces the service's own refusal", async () => {
    const user = userEvent.setup();
    createWatch.mockRejectedValue(
      new Error(
        "alerting: invalid watch: minSamples 9 exceeds the 3-bucket window",
      ),
    );
    renderEditor();

    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("minSamples 9");
  });

  // An unnamed watch cannot be saved, and the button says so before the round
  // trip rather than after it.
  it("will not create a watch with no name", () => {
    renderEditor();
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
  });

  it("confirms before deleting, and says what goes with it", async () => {
    const user = userEvent.setup();
    confirm.mockResolvedValue(false);
    renderEditor("w_1");

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(deleteWatch).not.toHaveBeenCalled();

    const asked = confirm.mock.calls[0][0] as { body: string; danger: boolean };
    expect(asked.body).toMatch(/history/i);
    expect(asked.danger).toBe(true);

    confirm.mockResolvedValue(true);
    await user.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteWatch).toHaveBeenCalledWith("w_1"));
  });

  // A new watch has no history to show, and an empty panel would read as "it has
  // never fired" rather than "it does not exist".
  it("saves an existing watch through save rather than create", async () => {
    const user = userEvent.setup();
    renderEditor("w_1");

    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveWatch).toHaveBeenCalled());
    expect(saveWatch.mock.calls[0][0]).toBe("w_1");
    expect(createWatch).not.toHaveBeenCalled();
  });
});
